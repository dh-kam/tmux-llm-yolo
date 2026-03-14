package runtime

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/dh-kam/tmux-llm-yolo/internal/capture"
	"github.com/dh-kam/tmux-llm-yolo/internal/llm"
	"github.com/dh-kam/tmux-llm-yolo/internal/policy"
	"github.com/dh-kam/tmux-llm-yolo/internal/prompt"
	"github.com/dh-kam/tmux-llm-yolo/internal/tmux"
	"github.com/dh-kam/tmux-llm-yolo/internal/tui"
)

const (
	stateMonitoring       = "monitoring"
	stateSuspectWaiting1  = "suspect_waiting_stage_1"
	stateSuspectWaiting2  = "suspect_waiting_stage_2"
	stateSuspectWaiting3  = "suspect_waiting_stage_3"
	stateConfidentWaiting = "confident_waiting"
	stateInterpreting     = "interpreting"
	stateActing           = "acting"
	stateCompleted        = "completed"
	stateStopped          = "stopped"
)

type Config struct {
	Target              string
	CaptureLines        int
	ContinueMessage     string
	SubmitKey           string
	SubmitKeyFallback   string
	SubmitFallbackDelay float64
	BaseInterval        time.Duration
	SuspectWait1        time.Duration
	SuspectWait2        time.Duration
	SuspectWait3        time.Duration
	Duration            time.Duration
	LLMName             string
	LLMModel            string
	FallbackLLMName     string
	FallbackLLMModel    string
	PolicyName          string
	Once                bool
	LogBuffer           *tui.LogBuffer
}

type Runner struct {
	cfg               Config
	client            tmux.API
	fetcher           capture.Fetcher
	executor          actionExecutor
	continuePlan      continueStrategy
	logger            func(string, ...interface{})
	queue             []task
	state             string
	ui                *tui.UI
	ctx               context.Context
	deadline          time.Time
	currentTask       task
	lastEvent         string
	sleepStarted      time.Time
	sleepUntil        time.Time
	sleepReason       string
	prevBase          capture.Snapshot
	continueSentCount int
	primaryProvider   llm.Provider
	fallbackProvider  llm.Provider
	primaryInitDone   bool
	fallbackInitDone  bool
	primaryInitErr    error
	fallbackInitErr   error
	lastLLMProvider   string
	ansiHistory       []ansiSnapshot
	screenHistory     []screenSnapshot
	continueOverride  string
	activePolicy      policy.Policy
}

const minimumStableWaitingDuration = 16 * time.Second
const ansiHistoryRetention = 3 * time.Minute
const longStableANSIDuration = 40 * time.Second
const defaultWatchDuration = 24 * time.Hour
const cursorProbeDelay = 120 * time.Millisecond
const lowActivityChangedLineThreshold = 0.08
const interactivePromptMinimumMatches = 2

var numberedPlanLinePattern = regexp.MustCompile(`(?m)^[[:space:]]*(\d+)[\).]\s+(.+)$`)

type ansiSnapshot struct {
	ANSI    string
	TakenAt time.Time
}

type screenSnapshot struct {
	ChangedLineRatio      float64
	PromptZoneFingerprint string
	InteractivePrompt     bool
	TakenAt               time.Time
}

type task interface {
	Name() string
	Description() string
	Run(*Runner) error
}

func New(cfg Config, client tmux.API, logger func(string, ...interface{})) *Runner {
	if cfg.BaseInterval <= 0 {
		cfg.BaseInterval = 4 * time.Second
	}
	if cfg.SuspectWait1 <= 0 {
		cfg.SuspectWait1 = 4 * time.Second
	}
	if cfg.SuspectWait2 <= 0 {
		cfg.SuspectWait2 = 4 * time.Second
	}
	if cfg.SuspectWait3 <= 0 {
		cfg.SuspectWait3 = 4 * time.Second
	}
	activePolicy := policy.Resolve(cfg.PolicyName)
	return &Runner{
		cfg:          cfg,
		client:       client,
		fetcher:      capture.Fetcher{Client: client},
		executor:     newActionExecutor(client, cfg, actionProviderHint(cfg)),
		continuePlan: newContinueStrategyWithPolicy(activePolicy, cfg.ContinueMessage),
		logger:       logger,
		state:        stateMonitoring,
		activePolicy: activePolicy,
	}
}

func (r *Runner) Run(ctx context.Context) error {
	r.ctx = ctx
	r.deadline = time.Now().Add(r.watchDuration())
	r.ui = tui.Start(ctx)
	defer r.ui.Stop()

	r.enqueue(checkDeadlineTask{})
	r.enqueue(baseCaptureTask{})

	for len(r.queue) > 0 {
		select {
		case <-ctx.Done():
			r.state = stateStopped
			r.lastEvent = "context canceled"
			r.updateUI()
			return ctx.Err()
		default:
		}

		r.currentTask, r.queue = r.queue[0], r.queue[1:]
		r.updateUI()
		if err := r.currentTask.Run(r); err != nil {
			if err == errStop {
				r.updateUI()
				return nil
			}
			r.lastEvent = err.Error()
			r.updateUI()
			return err
		}
		r.updateUI()
	}

	return nil
}

func (r *Runner) RunOnce(ctx context.Context) error {
	r.ctx = ctx
	r.deadline = time.Now().Add(r.watchDuration())
	r.ui = tui.Start(ctx)
	defer r.ui.Stop()

	r.setState(stateInterpreting, "once mode: 현재 화면을 즉시 해석")
	snap, err := r.fetcher.CaptureDual(ctx, r.cfg.Target, r.cfg.CaptureLines)
	if err != nil {
		return fmt.Errorf("once dual capture failed: %w", err)
	}
	r.prevBase = snap

	analysis := prompt.AnalyzeWithHintAndWidth(r.promptProviderHint(), snap.ANSI, snap.Plain, r.paneWidth())
	r.logger("once prompt analysis: provider=%s assistant_ui=%t processing=%t detected=%t line=%d class=%s reason=%s choice=%s", analysis.Provider, analysis.AssistantUI, analysis.Processing, analysis.PromptDetected, analysis.PromptLine, analysis.Classification, analysis.Reason, analysis.RecommendedChoice)
	if block := strings.TrimSpace(analysis.OutputBlock); block != "" {
		r.logger("once latest output block:\n%s", block)
	}
	if !analysis.AssistantUI {
		r.setState(stateCompleted, "once mode: assistant UI 시그니처가 없어 추가 입력 없이 종료")
		return nil
	}
	if r.hasCopilotSlashCommandState(analysis) {
		return r.clearPromptStateOnce("once mode: Copilot slash command 상태를 정리하고 종료")
	}
	if analysis.Processing {
		r.setState(stateCompleted, "once mode: 진행중 신호가 감지되어 추가 입력 없이 종료")
		return nil
	}
	if r.shouldReplacePendingPromptInput(analysis) {
		return r.injectContinueOnce("once mode: Copilot 입력창의 slash-start 미실행 텍스트를 비우고 새 continue 입력으로 교체")
	}
	if r.hasPendingPromptInput(analysis) {
		if r.shouldReplacePendingPromptInput(analysis) {
			return r.injectContinueOnce("once mode: Copilot 입력창의 미실행 텍스트를 비우고 새 continue 입력으로 교체")
		}
		return r.submitPendingInputOnce("once mode: 입력창에 미실행 텍스트가 남아 있어 submit-only 실행")
	}
	if plan, ok := deterministicActionPlan(analysis, "once mode: 결정적 입력 계획을 적용"); ok {
		return r.executeActionPlan(plan, true)
	}

	switch analysis.Classification {
	case prompt.ClassFreeTextRequest:
		fallthrough
	case prompt.ClassUnknownWaiting:
		decision, err := r.classifyWithLLM(ctx, analysis, snap.Plain)
		if err != nil {
			return r.injectContinueOnce("once mode: LLM 해석 실패로 기본 continue 입력")
		}
		r.maybeSetContinueOverride(decision.ContinueMessage, "llm")

		switch strings.ToUpper(strings.TrimSpace(decision.Action)) {
		case "SKIP":
			return r.injectContinueOnce(r.skipFallbackReason(decision.Reason, true))
		case "INJECT_SELECT":
			choice := prompt.ParseNumericChoice(decision.RecommendedChoice)
			if choice == "" {
				choice = "1"
			}
			return r.injectChoiceOnce(choice, decision.Reason)
		case "INJECT_INPUT":
			return r.injectInputOnce(decision.RecommendedChoice, decision.Reason)
		default:
			return r.injectContinueOnce(decision.Reason)
		}
	default:
		return r.injectContinueOnce("once mode: 미분류 대기 상태로 판단되어 기본 continue 입력")
	}
}

var errStop = fmt.Errorf("stop")

func (r *Runner) enqueue(tasks ...task) {
	r.queue = append(r.queue, tasks...)
	r.updateUI()
}

func (r *Runner) setState(state string, event string) {
	r.state = state
	if strings.TrimSpace(event) != "" {
		r.lastEvent = event
		r.logger("%s", event)
	}
	r.updateUI()
}

func (r *Runner) setStateQuiet(state string) {
	r.state = state
	r.updateUI()
}

func (r *Runner) nextTaskName() string {
	if len(r.queue) == 0 {
		return ""
	}
	return r.queue[0].Name() + " - " + r.queue[0].Description()
}

func (r *Runner) updateUI() {
	if r.ui == nil {
		return
	}
	currentTask := ""
	currentDesc := ""
	if r.currentTask != nil {
		currentTask = r.currentTask.Name()
		currentDesc = r.currentTask.Description()
	}
	r.ui.Update(tui.Snapshot{
		Target:      strings.TrimSpace(r.cfg.Target),
		State:       r.state,
		Mode:        r.modeName(),
		Capture:     fmt.Sprintf("%d lines", r.cfg.CaptureLines),
		WaitPlan:    r.waitPlanLine(),
		Continue:    r.continueLine(),
		Policy:      r.policyName(),
		CurrentTask: currentTask,
		CurrentDesc: currentDesc,
		NextTask:    r.nextTaskShortName(),
		NextDesc:    r.nextTaskDescription(),
		LLMPrimary:  r.providerState(r.cfg.LLMName, r.cfg.LLMModel, r.primaryInitDone, r.primaryInitErr),
		LLMFallback: r.fallbackProviderLine(),
		LLMActive:   r.activeLLMLine(),
		SleepReason: r.sleepReason,
		SleepStart:  r.sleepStarted,
		SleepUntil:  r.sleepUntil,
		Deadline:    r.deadline,
		LastEvent:   r.lastEvent,
		LastUpdated: time.Now(),
		LogLines:    r.cfg.LogBuffer.Lines(),
	})
}

func (r *Runner) scopeLine() string {
	mode := "watch"
	if r.cfg.Once {
		mode = "once"
	}
	parts := []string{
		"session=" + displayValue(strings.TrimSpace(r.cfg.Target)),
		"mode=" + mode,
		fmt.Sprintf("capture=%d", r.cfg.CaptureLines),
	}
	return strings.Join(parts, " | ")
}

func (r *Runner) modeName() string {
	if r.cfg.Once {
		return "once"
	}
	return "watch"
}

func (r *Runner) nextTaskShortName() string {
	if len(r.queue) == 0 {
		return ""
	}
	return r.queue[0].Name()
}

func (r *Runner) nextTaskDescription() string {
	if len(r.queue) == 0 {
		return ""
	}
	return r.queue[0].Description()
}

func (r *Runner) waitPlanLine() string {
	return fmt.Sprintf("%s>%s>%s>%s", r.baseInterval().Round(time.Second), r.suspectWait1().Round(time.Second), r.suspectWait2().Round(time.Second), r.suspectWait3().Round(time.Second))
}

func (r *Runner) continueLine() string {
	return fmt.Sprintf("%d sent / audit %d", r.continueSentCount, r.continuePlan.nextAuditIn(r.continueSentCount))
}

func (r *Runner) policyLine() string {
	parts := []string{
		"wait=" + r.waitPlanLine(),
		fmt.Sprintf("continue=%d sent,next-audit=%d", r.continueSentCount, r.continuePlan.nextAuditIn(r.continueSentCount)),
		"policy=" + r.policyName(),
	}
	llmPlan := "llm=primary"
	if strings.TrimSpace(r.cfg.FallbackLLMName) != "" {
		llmPlan = "llm=primary->fallback"
	}
	parts = append(parts, llmPlan)
	return strings.Join(parts, " | ")
}

func (r *Runner) policyName() string {
	if r.activePolicy != nil && strings.TrimSpace(r.activePolicy.Name()) != "" {
		return r.activePolicy.Name()
	}
	return policy.Default().Name()
}

func (r *Runner) promptProviderHint() string {
	return actionProviderHint(r.cfg)
}

func actionProviderHint(cfg Config) string {
	candidates := []string{
		strings.ToLower(strings.TrimSpace(cfg.LLMName)),
		strings.ToLower(strings.TrimSpace(cfg.Target)),
	}
	for _, candidate := range candidates {
		switch {
		case strings.Contains(candidate, "codex"):
			return "codex"
		case strings.Contains(candidate, "gemini"):
			return "gemini"
		case strings.Contains(candidate, "glm"), strings.Contains(candidate, "claude"):
			return "glm"
		case strings.Contains(candidate, "copilot"):
			return "copilot"
		}
	}
	return ""
}

func (r *Runner) paneWidth() int {
	if r.client == nil || r.ctx == nil {
		return 0
	}
	width, _, err := r.client.PaneSize(r.ctx, r.cfg.Target)
	if err != nil {
		return 0
	}
	return width
}

func (r *Runner) hasPendingPromptInput(analysis prompt.Analysis) bool {
	if !analysis.PromptActive {
		return false
	}
	if analysis.PromptPlaceholder {
		return false
	}
	promptText := strings.TrimSpace(analysis.PromptText)
	switch analysis.Provider {
	case "codex":
		return strings.HasPrefix(promptText, "›") && strings.TrimSpace(strings.TrimPrefix(promptText, "›")) != ""
	case "copilot":
		if !strings.HasPrefix(promptText, "❯") {
			return false
		}
		rest := strings.TrimSpace(strings.TrimPrefix(promptText, "❯"))
		if rest == "" {
			return false
		}
		lower := strings.ToLower(rest)
		return !strings.Contains(lower, "type @ to mention files")
	default:
		return false
	}
}

func (r *Runner) shouldReplacePendingPromptInput(analysis prompt.Analysis) bool {
	if !analysis.PromptActive {
		return false
	}
	if analysis.Provider != "copilot" {
		return false
	}
	promptText := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(analysis.PromptText), "❯"))
	return strings.HasPrefix(promptText, "/") || strings.Contains(promptText, ".ss") || promptText != ""
}

func (r *Runner) hasCopilotSlashCommandState(analysis prompt.Analysis) bool {
	if analysis.Provider != "copilot" {
		return false
	}
	promptText := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(analysis.PromptText), "❯")))
	if strings.HasPrefix(promptText, "/") {
		return true
	}
	block := strings.ToLower(analysis.OutputBlock)
	return strings.Contains(block, "▋ /") ||
		strings.Contains(block, "/add-dir") ||
		strings.Contains(block, "/allow-all") ||
		strings.Contains(block, "/agent")
}

func (r *Runner) prepareTextInput(input string) string {
	input = strings.TrimSpace(input)
	if r.promptProviderHint() == "copilot" {
		input = strings.TrimLeft(input, "/")
		input = strings.TrimSpace(input)
	}
	return input
}

func displayValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func (r *Runner) baseInterval() time.Duration {
	if r.cfg.BaseInterval <= 0 {
		return 4 * time.Second
	}
	return r.cfg.BaseInterval
}

func (r *Runner) watchDuration() time.Duration {
	if r.cfg.Duration <= 0 {
		return defaultWatchDuration
	}
	return r.cfg.Duration
}

func (r *Runner) suspectWait1() time.Duration {
	if r.cfg.SuspectWait1 <= 0 {
		return 4 * time.Second
	}
	return r.cfg.SuspectWait1
}

func (r *Runner) suspectWait2() time.Duration {
	if r.cfg.SuspectWait2 <= 0 {
		return 4 * time.Second
	}
	return r.cfg.SuspectWait2
}

func (r *Runner) suspectWait3() time.Duration {
	if r.cfg.SuspectWait3 <= 0 {
		return 4 * time.Second
	}
	return r.cfg.SuspectWait3
}

func (r *Runner) minStableWaitingDuration() time.Duration {
	minBySchedule := r.baseInterval() + r.suspectWait1() + r.suspectWait2() + r.suspectWait3()
	if minBySchedule >= minimumStableWaitingDuration {
		return minBySchedule
	}
	return minimumStableWaitingDuration
}

func (r *Runner) suspectWaitForStage(stage int) time.Duration {
	switch stage {
	case 1:
		return r.suspectWait1()
	case 2:
		return r.suspectWait2()
	default:
		return r.suspectWait3()
	}
}

func (r *Runner) suspectState(stage int) string {
	switch stage {
	case 1:
		return stateSuspectWaiting1
	case 2:
		return stateSuspectWaiting2
	default:
		return stateSuspectWaiting3
	}
}

func (r *Runner) llmStatusLine() string {
	parts := []string{
		fmt.Sprintf("primary=%s", r.providerState(r.cfg.LLMName, r.cfg.LLMModel, r.primaryInitDone, r.primaryInitErr)),
	}
	if fallback := r.fallbackProviderLine(); fallback != "" {
		parts = append(parts, "fallback="+fallback)
	}
	if active := r.activeLLMLine(); active != "" {
		parts = append(parts, "active="+active)
	}
	return strings.Join(parts, " | ")
}

func (r *Runner) fallbackProviderLine() string {
	if strings.TrimSpace(r.cfg.FallbackLLMName) == "" {
		return ""
	}
	return r.providerState(r.cfg.FallbackLLMName, r.cfg.FallbackLLMModel, r.fallbackInitDone, r.fallbackInitErr)
}

func (r *Runner) activeLLMLine() string {
	value := strings.TrimSpace(r.lastLLMProvider)
	if value == "" {
		return ""
	}
	parts := strings.Split(value, ":")
	if len(parts) == 0 {
		return value
	}
	if len(parts) >= 2 {
		return parts[0] + ":" + parts[1]
	}
	return parts[0]
}

func (r *Runner) providerState(name string, model string, initDone bool, initErr error) string {
	label := strings.TrimSpace(name)
	if model = strings.TrimSpace(model); model != "" {
		label += "/" + model
	}
	if label == "" {
		label = "-"
	}
	switch {
	case !initDone:
		return label + ":pending"
	case initErr != nil:
		return label + ":failed"
	default:
		return label + ":ready"
	}
}

func (r *Runner) recordANSISnapshot(ansi string, takenAt time.Time) {
	ansi = strings.TrimSpace(ansi)
	if ansi == "" {
		return
	}
	if takenAt.IsZero() {
		takenAt = time.Now()
	}
	r.ansiHistory = append(r.ansiHistory, ansiSnapshot{
		ANSI:    ansi,
		TakenAt: takenAt,
	})
	r.pruneANSIHistory(takenAt)
}

func (r *Runner) pruneANSIHistory(now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.Add(-ansiHistoryRetention)
	keepFrom := 0
	for keepFrom < len(r.ansiHistory) && r.ansiHistory[keepFrom].TakenAt.Before(cutoff) {
		keepFrom++
	}
	if keepFrom > 0 {
		r.ansiHistory = append([]ansiSnapshot(nil), r.ansiHistory[keepFrom:]...)
	}
}

func (r *Runner) recordScreenSnapshot(snap capture.Snapshot, analysis prompt.Analysis) screenSnapshot {
	summary := screenSnapshot{
		ChangedLineRatio:      changedLineRatio(r.prevBase.Plain, snap.Plain),
		PromptZoneFingerprint: prompt.PromptZoneFingerprint(snap.Plain),
		InteractivePrompt:     analysis.InteractivePrompt,
		TakenAt:               snap.TakenAt,
	}
	if summary.TakenAt.IsZero() {
		summary.TakenAt = time.Now()
	}
	r.screenHistory = append(r.screenHistory, summary)
	r.pruneScreenHistory(summary.TakenAt)
	return summary
}

func (r *Runner) pruneScreenHistory(now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.Add(-ansiHistoryRetention)
	keepFrom := 0
	for keepFrom < len(r.screenHistory) && r.screenHistory[keepFrom].TakenAt.Before(cutoff) {
		keepFrom++
	}
	if keepFrom > 0 {
		r.screenHistory = append([]screenSnapshot(nil), r.screenHistory[keepFrom:]...)
	}
}

func (r *Runner) shouldBypassWaitingStability(snap capture.Snapshot, analysis prompt.Analysis, summary screenSnapshot) bool {
	if !analysis.InteractivePrompt {
		return false
	}
	if r.hasStableInteractivePrompt(summary.PromptZoneFingerprint, summary.TakenAt) {
		return true
	}
	if summary.ChangedLineRatio > 0 && summary.ChangedLineRatio <= lowActivityChangedLineThreshold && r.hasRecentLowActivity(summary.TakenAt) {
		return true
	}
	switch analysis.Classification {
	case prompt.ClassNumberedMultipleChoice, prompt.ClassCursorBasedChoice:
		return summary.ChangedLineRatio <= lowActivityChangedLineThreshold || r.prevBase.ANSI == ""
	default:
		return false
	}
}

func (r *Runner) hasStableInteractivePrompt(fingerprint string, now time.Time) bool {
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		return false
	}
	r.pruneScreenHistory(now)
	matches := 0
	for i := len(r.screenHistory) - 1; i >= 0; i-- {
		snap := r.screenHistory[i]
		if !snap.InteractivePrompt {
			break
		}
		if snap.PromptZoneFingerprint != fingerprint {
			break
		}
		matches++
		if matches >= interactivePromptMinimumMatches {
			return true
		}
	}
	return false
}

func (r *Runner) hasRecentLowActivity(now time.Time) bool {
	r.pruneScreenHistory(now)
	if len(r.screenHistory) == 0 {
		return false
	}
	var count int
	var total float64
	for i := len(r.screenHistory) - 1; i >= 0; i-- {
		snap := r.screenHistory[i]
		if now.Sub(snap.TakenAt) > ansiHistoryRetention {
			break
		}
		if snap.ChangedLineRatio <= 0 {
			continue
		}
		total += snap.ChangedLineRatio
		count++
		if count >= 6 {
			break
		}
	}
	if count < 2 {
		return false
	}
	return total/float64(count) <= lowActivityChangedLineThreshold
}

func changedLineRatio(prevPlain string, currentPlain string) float64 {
	prevLines := normalizeHistoryLines(prevPlain)
	currentLines := normalizeHistoryLines(currentPlain)
	limit := len(prevLines)
	if len(currentLines) > limit {
		limit = len(currentLines)
	}
	if limit == 0 {
		return 0
	}
	changed := 0
	for i := 0; i < limit; i++ {
		var prev string
		var current string
		if i < len(prevLines) {
			prev = prevLines[i]
		}
		if i < len(currentLines) {
			current = currentLines[i]
		}
		if prev != current {
			changed++
		}
	}
	return float64(changed) / float64(limit)
}

func normalizeHistoryLines(plain string) []string {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(plain, "\r\n", "\n")), "\n")
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		line = prompt.PromptZoneFingerprint(line)
		if line == "" {
			normalized = append(normalized, "")
			continue
		}
		normalized = append(normalized, line)
	}
	return normalized
}

func (r *Runner) hasLongStableANSI(ansi string, now time.Time) bool {
	ansi = strings.TrimSpace(ansi)
	if ansi == "" {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	r.pruneANSIHistory(now)
	var stableSince time.Time
	for i := len(r.ansiHistory) - 1; i >= 0; i-- {
		snap := r.ansiHistory[i]
		if snap.ANSI != ansi {
			break
		}
		stableSince = snap.TakenAt
	}
	if stableSince.IsZero() {
		return false
	}
	return now.Sub(stableSince) >= longStableANSIDuration
}

type checkDeadlineTask struct{}

func (checkDeadlineTask) Name() string { return "CheckDeadlineTask" }
func (checkDeadlineTask) Description() string {
	return "watch deadline exceeded 여부를 확인한다"
}
func (checkDeadlineTask) Run(r *Runner) error {
	if time.Now().After(r.deadline) {
		r.setState(stateStopped, "watch deadline exceeded")
		r.queue = nil
		return errStop
	}
	return nil
}

type baseCaptureTask struct{}

func (baseCaptureTask) Name() string        { return "CaptureTask" }
func (baseCaptureTask) Description() string { return "기준 주기 dual capture를 수행한다" }
func (baseCaptureTask) Run(r *Runner) error {
	r.sleepUntil = time.Time{}
	r.sleepReason = ""
	r.setStateQuiet(stateMonitoring)
	snap, err := r.fetcher.CaptureDual(r.ctx, r.cfg.Target, r.cfg.CaptureLines)
	if err != nil {
		return fmt.Errorf("base dual capture failed: %w", err)
	}
	r.recordANSISnapshot(snap.ANSI, snap.TakenAt)
	analysis := prompt.AnalyzeWithHint(r.promptProviderHint(), snap.ANSI, snap.Plain)
	summary := r.recordScreenSnapshot(snap, analysis)
	if strings.TrimSpace(r.prevBase.ANSI) == "" {
		r.prevBase = snap
		r.enqueue(
			sleepTask{duration: r.baseInterval(), reason: fmt.Sprintf("첫 기준 ANSI 캡처 저장 완료, 다음 %s 주기 대기", r.baseInterval().Round(time.Second))},
			checkDeadlineTask{},
			baseCaptureTask{},
		)
		return nil
	}
	if r.shouldBypassWaitingStability(snap, analysis, summary) {
		r.setState(stateConfidentWaiting, "하단 interactive prompt가 감지되어 ANSI 안정성 대기를 우회하고 해석으로 진입")
		r.prevBase = snap
		r.enqueue(checkDeadlineTask{}, analyzeWaitingTask{
			referenceANSI:       snap.ANSI,
			referencePromptZone: summary.PromptZoneFingerprint,
			allowVolatileANSI:   true,
		})
		return nil
	}
	if snap.ANSI != r.prevBase.ANSI {
		r.prevBase = snap
		r.enqueue(
			sleepTask{duration: r.baseInterval(), reason: fmt.Sprintf("ANSI 화면이 이전 기준 캡처와 달라짐, 다시 %s 모니터링", r.baseInterval().Round(time.Second))},
			checkDeadlineTask{},
			baseCaptureTask{},
		)
		return nil
	}

	if r.hasLongStableANSI(snap.ANSI, snap.TakenAt) {
		r.setState(stateConfidentWaiting, fmt.Sprintf("ANSI 화면이 최근 %s 이상 동일하게 유지되어 즉시 입력 대기 상태로 승격", longStableANSIDuration.Round(time.Second)))
		r.enqueue(checkDeadlineTask{}, analyzeWaitingTask{referenceANSI: snap.ANSI, forceInput: true})
		return nil
	}

	r.setState(stateSuspectWaiting1, fmt.Sprintf("ANSI 화면이 %s 기준 캡처와 동일하여 1차 입력 대기 의심", r.baseInterval().Round(time.Second)))
	r.enqueue(
		sleepTask{duration: r.suspectWait1(), reason: fmt.Sprintf("1차 입력 대기 의심, %s 후 ANSI 재확인", r.suspectWait1().Round(time.Second))},
		checkDeadlineTask{},
		ansiRecheckTask{stage: 1, referenceANSI: snap.ANSI},
	)
	return nil
}

type ansiRecheckTask struct {
	stage         int
	referenceANSI string
}

func (t ansiRecheckTask) Name() string { return "CompareCaptureTask" }
func (t ansiRecheckTask) Description() string {
	return fmt.Sprintf("%d차 입력 대기 의심 상태에서 ANSI 재비교를 수행한다", t.stage)
}
func (t ansiRecheckTask) Run(r *Runner) error {
	snap, err := r.fetcher.CaptureDual(r.ctx, r.cfg.Target, r.cfg.CaptureLines)
	if err != nil {
		return fmt.Errorf("ansi recheck failed: %w", err)
	}
	r.recordANSISnapshot(snap.ANSI, snap.TakenAt)
	analysis := prompt.AnalyzeWithHint(r.promptProviderHint(), snap.ANSI, snap.Plain)
	summary := r.recordScreenSnapshot(snap, analysis)
	if r.shouldBypassWaitingStability(snap, analysis, summary) {
		r.setState(stateConfidentWaiting, "의심 단계 중 interactive prompt가 감지되어 ANSI 안정성 대기를 우회하고 해석으로 진입")
		r.prevBase = snap
		r.enqueue(checkDeadlineTask{}, analyzeWaitingTask{
			referenceANSI:       snap.ANSI,
			referencePromptZone: summary.PromptZoneFingerprint,
			allowVolatileANSI:   true,
		})
		return nil
	}
	if snap.ANSI != t.referenceANSI {
		r.setState(stateMonitoring, fmt.Sprintf("의심 단계 중 ANSI 화면이 바뀌어 %s 모니터링으로 복귀", r.baseInterval().Round(time.Second)))
		r.prevBase = snap
		r.enqueue(
			sleepTask{duration: r.baseInterval(), reason: fmt.Sprintf("ANSI 변경 감지, 다시 %s 기준 캡처 대기", r.baseInterval().Round(time.Second))},
			checkDeadlineTask{},
			baseCaptureTask{},
		)
		return nil
	}

	if r.hasLongStableANSI(snap.ANSI, snap.TakenAt) {
		r.setState(stateConfidentWaiting, fmt.Sprintf("ANSI 화면이 최근 %s 이상 동일하게 유지되어 즉시 입력 대기 상태로 승격", longStableANSIDuration.Round(time.Second)))
		r.enqueue(checkDeadlineTask{}, analyzeWaitingTask{referenceANSI: snap.ANSI, forceInput: true})
		return nil
	}

	elapsed := time.Since(r.prevBase.TakenAt)
	if elapsed < r.minStableWaitingDuration() {
		nextStage := t.stage + 1
		waitMore := r.suspectWaitForStage(nextStage)
		if remaining := r.minStableWaitingDuration() - elapsed; remaining > 0 && remaining < waitMore {
			waitMore = remaining
		}
		r.setState(r.suspectState(nextStage), fmt.Sprintf("ANSI 화면은 동일하며 최소 안정 시간 %s를 채우기 위해 %d차 입력 대기 의심으로 진행", r.minStableWaitingDuration().Round(time.Second), nextStage))
		r.enqueue(
			sleepTask{duration: waitMore, reason: fmt.Sprintf("%d차 입력 대기 의심, %s 후 ANSI 재확인", nextStage, waitMore.Round(time.Second))},
			checkDeadlineTask{},
			ansiRecheckTask{stage: nextStage, referenceANSI: snap.ANSI},
		)
		return nil
	}

	r.setState(stateConfidentWaiting, "16초 동안 5회 ANSI 화면이 동일하여 입력 대기 상태를 확신")
	r.enqueue(checkDeadlineTask{}, analyzeWaitingTask{referenceANSI: snap.ANSI})
	return nil
}

type analyzeWaitingTask struct {
	referenceANSI       string
	referencePromptZone string
	allowVolatileANSI   bool
	forceInput          bool
}

func (t analyzeWaitingTask) Name() string { return "InterpretWaitingStateTask" }
func (t analyzeWaitingTask) Description() string {
	if t.forceInput {
		return "장기 정지 ANSI 화면을 강제 입력 대상으로 해석해 다음 동작을 결정한다"
	}
	if t.allowVolatileANSI {
		return "interactive prompt를 우선시해 변동 ANSI를 허용하고 다음 동작을 결정한다"
	}
	return "prompt 위치와 마지막 출력 블록을 해석해 다음 동작을 결정한다"
}
func (t analyzeWaitingTask) Run(r *Runner) error {
	r.setState(stateInterpreting, "waiting 화면 해석 시작")
	snap, err := r.fetcher.CaptureDual(r.ctx, r.cfg.Target, r.cfg.CaptureLines)
	if err != nil {
		return fmt.Errorf("analysis dual capture failed: %w", err)
	}
	if snap.ANSI != t.referenceANSI {
		if t.allowVolatileANSI {
			currentPromptZone := prompt.PromptZoneFingerprint(snap.Plain)
			if currentPromptZone == t.referencePromptZone && currentPromptZone != "" {
				goto analyze
			}
		}
		r.prevBase = snap
		r.setState(stateMonitoring, "해석 직전 화면이 바뀌어 모니터링으로 복귀")
		r.enqueue(
			sleepTask{duration: r.baseInterval(), reason: fmt.Sprintf("해석 직전 ANSI 변경 감지, 다시 %s 모니터링", r.baseInterval().Round(time.Second))},
			checkDeadlineTask{},
			baseCaptureTask{},
		)
		return nil
	}

analyze:
	analysis := prompt.AnalyzeWithHintAndWidth(r.promptProviderHint(), snap.ANSI, snap.Plain, r.paneWidth())
	r.logger("prompt analysis: provider=%s assistant_ui=%t processing=%t interactive=%t detected=%t line=%d class=%s reason=%s choice=%s", analysis.Provider, analysis.AssistantUI, analysis.Processing, analysis.InteractivePrompt, analysis.PromptDetected, analysis.PromptLine, analysis.Classification, analysis.Reason, analysis.RecommendedChoice)
	if block := strings.TrimSpace(analysis.OutputBlock); block != "" {
		r.logger("latest output block:\n%s", block)
	}
	if !analysis.AssistantUI {
		if t.forceInput {
			return r.injectContinue("장시간 동일 ANSI 화면에서 assistant UI 시그니처는 약하지만 강제 continue 입력")
		}
		r.prevBase = snap
		r.setState(stateMonitoring, fmt.Sprintf("assistant UI 시그니처가 없어 입력하지 않고 %s 모니터링으로 복귀", r.baseInterval().Round(time.Second)))
		r.enqueue(
			sleepTask{duration: r.baseInterval(), reason: fmt.Sprintf("assistant UI 미확인, 다시 %s 모니터링", r.baseInterval().Round(time.Second))},
			checkDeadlineTask{},
			baseCaptureTask{},
		)
		return nil
	}
	if r.hasCopilotSlashCommandState(analysis) {
		return r.clearPromptState("Copilot slash command 상태를 정리하고 모니터링으로 복귀")
	}
	if analysis.Processing {
		if t.forceInput {
			return r.injectContinue("장시간 동일 ANSI 화면이 processing으로 보이지만 정체 상태로 판단되어 강제 continue 입력")
		}
		r.prevBase = snap
		r.setState(stateMonitoring, fmt.Sprintf("진행중 신호가 남아 있어 입력하지 않고 %s 모니터링으로 복귀", r.baseInterval().Round(time.Second)))
		r.enqueue(
			sleepTask{duration: r.baseInterval(), reason: fmt.Sprintf("진행중 신호 감지, 다시 %s 모니터링", r.baseInterval().Round(time.Second))},
			checkDeadlineTask{},
			baseCaptureTask{},
		)
		return nil
	}
	if r.shouldReplacePendingPromptInput(analysis) {
		return r.injectContinue("Copilot 입력창의 slash-start 미실행 텍스트를 비우고 새 continue 입력으로 교체")
	}
	if r.hasPendingPromptInput(analysis) {
		return r.submitPendingInput("입력창에 미실행 텍스트가 남아 있어 submit-only 실행")
	}
	if plan, ok := deterministicActionPlan(analysis, "결정적 입력 계획을 적용"); ok {
		return r.executeActionPlan(plan, false)
	}

	switch analysis.Classification {
	case prompt.ClassFreeTextRequest:
		fallthrough
	case prompt.ClassUnknownWaiting:
		decision, err := r.classifyWithLLM(r.ctx, analysis, snap.Plain)
		if err != nil {
			return r.injectContinue("LLM 해석 실패로 기본 continue 입력")
		}
		if t.forceInput && strings.EqualFold(strings.TrimSpace(decision.Action), "SKIP") {
			decision.Action = "INJECT_CONTINUE"
			if strings.TrimSpace(decision.Reason) == "" {
				decision.Reason = "장시간 동일 ANSI 화면에서 강제 continue 입력"
			}
		}
		return r.applyLLMDecision(decision)
	default:
		return r.injectContinue("미분류 대기 상태로 판단되어 기본 continue 입력")
	}
}

type sleepTask struct {
	duration time.Duration
	reason   string
}

func (t sleepTask) Name() string        { return "SleepTask" }
func (t sleepTask) Description() string { return t.reason }
func (t sleepTask) Run(r *Runner) error {
	if t.duration <= 0 {
		return nil
	}
	r.sleepReason = t.reason
	r.sleepStarted = time.Now()
	r.sleepUntil = time.Now().Add(t.duration)
	r.updateUI()
	timer := time.NewTimer(t.duration)
	defer timer.Stop()
	select {
	case <-r.ctx.Done():
		return r.ctx.Err()
	case <-timer.C:
		r.sleepStarted = time.Time{}
		r.sleepUntil = time.Time{}
		r.sleepReason = ""
		return nil
	}
}

func (r *Runner) injectContinue(reason string) error {
	nextCount := r.continueSentCount + 1
	message := r.prepareTextInput(r.nextContinueMessage(nextCount))
	r.setState(stateActing, reason)
	r.logger("continue prompt selected: count=%d message=%q", nextCount, message)
	if err := r.getExecutor().SendContinue(r.ctx, continueRequest{Message: message}); err != nil {
		return err
	}
	r.continueSentCount = nextCount
	r.prevBase = capture.Snapshot{}
	r.enqueue(
		sleepTask{duration: r.baseInterval(), reason: fmt.Sprintf("입력 전송 후 다음 %s 모니터링 대기", r.baseInterval().Round(time.Second))},
		checkDeadlineTask{},
		baseCaptureTask{},
	)
	return nil
}

func (r *Runner) injectContinueOnce(reason string) error {
	nextCount := r.continueSentCount + 1
	message := r.prepareTextInput(r.nextContinueMessage(nextCount))
	r.setState(stateActing, reason)
	r.logger("continue prompt selected: count=%d message=%q", nextCount, message)
	if err := r.getExecutor().SendContinue(r.ctx, continueRequest{Message: message}); err != nil {
		return err
	}
	r.continueSentCount = nextCount
	r.setState(stateStopped, "once mode: continue 입력 후 종료")
	return nil
}

func (r *Runner) injectChoice(choice string, reason string) error {
	r.setState(stateActing, reason)
	if err := r.getExecutor().SendChoice(r.ctx, choiceRequest{Choice: choice}); err != nil {
		return err
	}
	r.prevBase = capture.Snapshot{}
	r.enqueue(
		sleepTask{duration: r.baseInterval(), reason: fmt.Sprintf("선택지 입력 후 다음 %s 모니터링 대기", r.baseInterval().Round(time.Second))},
		checkDeadlineTask{},
		baseCaptureTask{},
	)
	return nil
}

func (r *Runner) injectChoiceOnce(choice string, reason string) error {
	r.setState(stateActing, reason)
	if err := r.getExecutor().SendChoice(r.ctx, choiceRequest{Choice: choice}); err != nil {
		return err
	}
	r.setState(stateStopped, "once mode: 선택 입력 후 종료")
	return nil
}

func (r *Runner) injectCursorConfirm(reason string) error {
	return r.injectCursorChoice("1", reason)
}

func (r *Runner) injectCursorChoice(choice string, reason string) error {
	r.setState(stateActing, reason)
	if err := r.getExecutor().SendCursorChoice(r.ctx, cursorChoiceRequest{Choice: choice}); err != nil {
		return r.cursorChoiceFallback(reason, err)
	}
	r.prevBase = capture.Snapshot{}
	r.enqueue(
		sleepTask{duration: r.baseInterval(), reason: fmt.Sprintf("커서형 선택 확정 후 다음 %s 모니터링 대기", r.baseInterval().Round(time.Second))},
		checkDeadlineTask{},
		baseCaptureTask{},
	)
	return nil
}

func (r *Runner) injectCursorConfirmOnce(reason string) error {
	return r.injectCursorChoiceOnce("1", reason)
}

func (r *Runner) injectCursorChoiceOnce(choice string, reason string) error {
	r.setState(stateActing, reason)
	if err := r.getExecutor().SendCursorChoice(r.ctx, cursorChoiceRequest{Choice: choice}); err != nil {
		return r.cursorChoiceFallbackOnce(reason, err)
	}
	r.setState(stateStopped, "once mode: 커서형 선택 확정 후 종료")
	return nil
}

func (r *Runner) cursorChoiceFallback(reason string, cause error) error {
	r.logger("cursor choice failed; falling back to continue: %v", cause)
	return r.injectContinue(reason + " 방향키 선택이 확인되지 않아 continue 입력으로 폴백")
}

func (r *Runner) cursorChoiceFallbackOnce(reason string, cause error) error {
	r.logger("cursor choice failed in once mode; falling back to continue: %v", cause)
	return r.injectContinueOnce(reason + " 방향키 선택이 확인되지 않아 continue 입력으로 폴백")
}

func (r *Runner) injectInputOnce(input string, reason string) error {
	input = r.prepareTextInput(input)
	if input == "" {
		fallbackReason := r.emptyInputFallbackReason(reason)
		r.logger("empty recommended input in once mode; falling back to continue: reason=%q", reason)
		return r.injectContinueOnce(fallbackReason)
	}
	r.setState(stateActing, reason)
	if err := r.getExecutor().SendInput(r.ctx, inputRequest{Input: input}); err != nil {
		return err
	}
	r.setState(stateStopped, "once mode: 입력값 전송 후 종료")
	return nil
}

func (r *Runner) submitPendingInput(reason string) error {
	r.setState(stateActing, reason)
	if err := r.getExecutor().SubmitPending(r.ctx, submitRequest{}); err != nil {
		return err
	}
	r.prevBase = capture.Snapshot{}
	r.enqueue(
		sleepTask{duration: r.baseInterval(), reason: fmt.Sprintf("submit-only 실행 후 다음 %s 모니터링 대기", r.baseInterval().Round(time.Second))},
		checkDeadlineTask{},
		baseCaptureTask{},
	)
	return nil
}

func (r *Runner) submitPendingInputOnce(reason string) error {
	r.setState(stateActing, reason)
	if err := r.getExecutor().SubmitPending(r.ctx, submitRequest{}); err != nil {
		return err
	}
	r.setState(stateStopped, "once mode: submit-only 실행 후 종료")
	return nil
}

func (r *Runner) clearPromptState(reason string) error {
	r.setState(stateActing, reason)
	if err := r.getExecutor().ClearPrompt(r.ctx, clearPromptRequest{}); err != nil {
		return err
	}
	r.prevBase = capture.Snapshot{}
	r.enqueue(
		sleepTask{duration: r.baseInterval(), reason: fmt.Sprintf("입력창 정리 후 다음 %s 모니터링 대기", r.baseInterval().Round(time.Second))},
		checkDeadlineTask{},
		baseCaptureTask{},
	)
	return nil
}

func (r *Runner) clearPromptStateOnce(reason string) error {
	r.setState(stateActing, reason)
	if err := r.getExecutor().ClearPrompt(r.ctx, clearPromptRequest{}); err != nil {
		return err
	}
	r.setState(stateStopped, "once mode: 입력창 정리 후 종료")
	return nil
}

func (r *Runner) emptyInputFallbackReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "빈 free-text 입력 추천이 반환되어 기본 continue 입력으로 폴백"
	}
	return reason + " 빈 free-text 입력 추천이 반환되어 기본 continue 입력으로 폴백"
}

func (r *Runner) skipFallbackReason(reason string, once bool) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		if once {
			return "once mode: SKIP 결정이 반환되어 continue 입력으로 폴백"
		}
		return "LLM이 SKIP 결정을 반환해도 continue 입력으로 폴백"
	}
	return reason + " SKIP 대신 continue 입력으로 폴백"
}

func continueMessageFromPlannedItems(analysis prompt.Analysis) string {
	if analysis.Classification != prompt.ClassFreeTextRequest {
		return ""
	}
	if !analysis.PromptActive {
		return ""
	}
	block := strings.TrimSpace(analysis.OutputBlock)
	if block == "" {
		return ""
	}
	matches := numberedPlanLinePattern.FindAllStringSubmatch(block, -1)
	if len(matches) == 0 {
		return ""
	}
	firstNumber := strings.TrimSpace(matches[0][1])
	firstText := strings.TrimSpace(matches[0][2])
	if firstNumber == "" || firstText == "" {
		return ""
	}
	firstText = truncatePlaintextSentence(firstText, 96)
	return fmt.Sprintf("남은 항목 %s번(%s)부터 진행하고, 검증까지 마친 뒤 다음 항목으로 이어서 진행해보자.", firstNumber, firstText)
}

func truncatePlaintextSentence(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

type llmDecision struct {
	Action            string
	RecommendedChoice string
	Reason            string
	ContinueMessage   string
}

func (r *Runner) applyLLMDecision(decision llmDecision) error {
	r.maybeSetContinueOverride(decision.ContinueMessage, "llm")
	switch decision.Action {
	case "SKIP":
		return r.injectContinue(r.skipFallbackReason(decision.Reason, false))
	case "INJECT_SELECT":
		choice := prompt.ParseNumericChoice(decision.RecommendedChoice)
		if choice == "" {
			choice = "1"
		}
		return r.injectChoice(choice, decision.Reason)
	case "INJECT_INPUT":
		input := r.prepareTextInput(decision.RecommendedChoice)
		if input == "" {
			r.logger("empty recommended input from LLM; falling back to continue: action=%s reason=%q", decision.Action, decision.Reason)
			return r.injectContinue(r.emptyInputFallbackReason(decision.Reason))
		}
		r.setState(stateActing, decision.Reason)
		if err := r.getExecutor().SendInput(r.ctx, inputRequest{Input: input}); err != nil {
			return err
		}
		r.prevBase = capture.Snapshot{}
		r.enqueue(
			sleepTask{duration: r.baseInterval(), reason: fmt.Sprintf("입력값 전송 후 다음 %s 모니터링 대기", r.baseInterval().Round(time.Second))},
			checkDeadlineTask{},
			baseCaptureTask{},
		)
		return nil
	default:
		return r.injectContinue(decision.Reason)
	}
}

func (r *Runner) getExecutor() actionExecutor {
	if r.executor == nil {
		r.executor = newActionExecutor(r.client, r.cfg, r.promptProviderHint())
	}
	return r.executor
}

func (r *Runner) classifyWithLLM(ctx context.Context, analysis prompt.Analysis, plainCapture string) (llmDecision, error) {
	providers := []struct {
		label    string
		getter   func(context.Context) (llm.Provider, error)
		llmName  string
		llmModel string
	}{
		{
			label:    "primary",
			getter:   r.getPrimaryProvider,
			llmName:  r.cfg.LLMName,
			llmModel: r.cfg.LLMModel,
		},
	}
	if strings.TrimSpace(r.cfg.FallbackLLMName) != "" {
		providers = append(providers, struct {
			label    string
			getter   func(context.Context) (llm.Provider, error)
			llmName  string
			llmModel string
		}{
			label:    "fallback",
			getter:   r.getFallbackProvider,
			llmName:  r.cfg.FallbackLLMName,
			llmModel: r.cfg.FallbackLLMModel,
		})
	}

	var errs []string
	for _, candidate := range providers {
		provider, err := candidate.getter(ctx)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s=%v", candidate.label, err))
			r.updateUI()
			continue
		}
		decision, err := classifyWithLLMFallback(ctx, provider, candidate.llmName, candidate.llmModel, analysis, plainCapture)
		if err == nil {
			r.lastLLMProvider = candidate.label + ":" + candidate.llmName
			r.updateUI()
			return decision, nil
		}
		r.logger("LLM fallback classification failed via %s llm=%s model=%s: %v", candidate.label, candidate.llmName, candidate.llmModel, err)
		errs = append(errs, fmt.Sprintf("%s=%v", candidate.label, err))
		r.lastLLMProvider = candidate.label + ":" + candidate.llmName + ":failed"
		r.updateUI()
	}
	if len(errs) == 0 {
		return llmDecision{}, fmt.Errorf("no llm provider configured")
	}
	return llmDecision{}, fmt.Errorf("all llm providers failed: %s", strings.Join(errs, "; "))
}

func (r *Runner) getPrimaryProvider(ctx context.Context) (llm.Provider, error) {
	if r.primaryInitDone {
		return r.primaryProvider, r.primaryInitErr
	}
	r.primaryInitDone = true
	r.primaryProvider, r.primaryInitErr = initializeProvider(ctx, r.cfg.LLMName, r.cfg.LLMModel, r.logger, "primary")
	return r.primaryProvider, r.primaryInitErr
}

func (r *Runner) getFallbackProvider(ctx context.Context) (llm.Provider, error) {
	if strings.TrimSpace(r.cfg.FallbackLLMName) == "" {
		return nil, fmt.Errorf("fallback llm not configured")
	}
	if r.fallbackInitDone {
		return r.fallbackProvider, r.fallbackInitErr
	}
	r.fallbackInitDone = true
	r.fallbackProvider, r.fallbackInitErr = initializeProvider(ctx, r.cfg.FallbackLLMName, r.cfg.FallbackLLMModel, r.logger, "fallback")
	return r.fallbackProvider, r.fallbackInitErr
}

func initializeProvider(ctx context.Context, name string, model string, logger func(string, ...interface{}), role string) (llm.Provider, error) {
	provider, err := llm.New(name, model)
	if err != nil {
		return nil, err
	}
	binary, err := provider.ValidateBinary()
	if err != nil {
		return nil, fmt.Errorf("llm 바이너리 검증 실패: %w", err)
	}
	usage, err := provider.CheckUsage(ctx)
	if err != nil {
		return nil, err
	}
	if usage.HasKnownLimit && usage.Remaining <= 0 {
		return nil, fmt.Errorf("llm %s의 잔여 사용량이 0 이하입니다 (source=%s)", name, usage.Source)
	}
	if usage.HasKnownLimit {
		logger("LLM lazy init (%s): llm=%s model=%s binary=%s usage_source=%s remaining=%d", role, name, model, binary, usage.Source, usage.Remaining)
	} else {
		logger("LLM lazy init (%s): llm=%s model=%s binary=%s usage_source=%s", role, name, model, binary, usage.Source)
	}
	return provider, nil
}

func (r *Runner) nextContinueMessage(nextCount int) string {
	if override := strings.TrimSpace(r.continueOverride); override != "" {
		r.continueOverride = ""
		return override
	}
	if nextCount <= 0 {
		nextCount = r.continueSentCount + 1
	}
	return r.continuePlan.messageFor(nextCount)
}

func (r *Runner) maybeSetContinueOverride(message string, source string) {
	message = r.prepareTextInput(message)
	if message == "" {
		return
	}
	r.continueOverride = message
	if strings.TrimSpace(source) == "" {
		r.logger("continue override prepared: %q", message)
		return
	}
	r.logger("continue override prepared from %s: %q", source, message)
}

func classifyWithLLMFallback(ctx context.Context, provider llm.Provider, llmName string, llmModel string, analysis prompt.Analysis, plainCapture string) (llmDecision, error) {
	if provider == nil {
		return llmDecision{}, fmt.Errorf("provider not configured")
	}
	outputBlock := strings.TrimSpace(analysis.OutputBlock)
	if outputBlock == "" {
		outputBlock = strings.TrimSpace(plainCapture)
	}
	promptText := strings.TrimSpace(analysis.PromptText)
	rawPrompt := fmt.Sprintf(`You are a strict terminal waiting-state classifier.
The watcher is already confident the terminal is waiting for user input because ANSI screen content stayed unchanged through multiple timed checks.

Classify the required action from the final output block and prompt line.

Return exactly 4 lines:
ACTION: INJECT_CONTINUE | INJECT_SELECT | INJECT_INPUT | SKIP
RECOMMENDED_CHOICE: <number, text, or none>
CONTINUE_MESSAGE: <short follow-up continue instruction, or none>
REASON: <one short sentence>

Rules:
- Use INJECT_SELECT only for numbered menus.
- Use INJECT_INPUT for specific free-text input requests.
- Use INJECT_CONTINUE for completion handoff or safe generic continuation.
- Use SKIP only if the screen clearly says there is nothing left to do.
- If the visible text mentions parity, match rate, pass rate, or percentage progress, propose a concrete CONTINUE_MESSAGE that pushes parity toward 100%% through planned execution and verification.
- If the visible text sounds like the work is done or nearly done, prefer a CONTINUE_MESSAGE that pushes the agent to verify completion instead of stopping early:
  run the relevant build/test/unit/integration checks,
  confirm the original goal is fully satisfied,
  scan the code for TODO, FIXME, not implemented, stub, placeholder, or missing branches,
  review whether all functional requirements are actually implemented in code,
  and only then move on to non-functional improvement work.
- When the work appears functionally complete, the preferred next steps are:
  verify correctness with tests,
  inspect for unfinished code paths,
  profile CPU and memory usage,
  identify and improve bottlenecks,
  reduce memory footprint where practical,
  and propose code-level refactors that improve modularity, readability, maintainability, performance, and testability.
- Push the agent toward architecture and design cleanup when appropriate:
  Clean Architecture boundaries,
  Single Responsibility Principle,
  interface-driven design,
  lower coupling,
  clearer module boundaries,
  and easier testing/extensibility.
- A strong CONTINUE_MESSAGE should be specific, action-oriented, and should push the agent to keep improving the code even after a completion summary.
- CONTINUE_MESSAGE is optional, but when useful it should be a single actionable sentence in the operator's language.
- Do not mention anything outside the visible terminal text.

Prompt line:
%s

Last output block:
%s

Full plain capture tail:
%s
`, promptText, outputBlock, trimTailLines(plainCapture, 20))

	out, err := provider.RunPrompt(ctx, rawPrompt)
	if err != nil {
		return llmDecision{}, err
	}
	return parseLLMDecision(out), nil
}

func parseLLMDecision(raw string) llmDecision {
	decision := llmDecision{
		Action: "INJECT_CONTINUE",
		Reason: "LLM fallback default continue",
	}
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(strings.ToUpper(line), "ACTION:"):
			decision.Action = strings.TrimSpace(line[len("ACTION:"):])
		case strings.HasPrefix(strings.ToUpper(line), "RECOMMENDED_CHOICE:"):
			decision.RecommendedChoice = strings.TrimSpace(line[len("RECOMMENDED_CHOICE:"):])
		case strings.HasPrefix(strings.ToUpper(line), "CONTINUE_MESSAGE:"):
			decision.ContinueMessage = strings.TrimSpace(line[len("CONTINUE_MESSAGE:"):])
		case strings.HasPrefix(strings.ToUpper(line), "REASON:"):
			decision.Reason = strings.TrimSpace(line[len("REASON:"):])
		}
	}
	decision.Action = strings.ToUpper(strings.TrimSpace(decision.Action))
	if decision.RecommendedChoice == "none" {
		decision.RecommendedChoice = ""
	}
	if strings.EqualFold(decision.ContinueMessage, "none") {
		decision.ContinueMessage = ""
	}
	return decision
}

func trimTailLines(value string, maxLines int) string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	if maxLines <= 0 || len(lines) <= maxLines {
		return strings.TrimRight(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	}
	start := len(lines) - maxLines
	return strings.TrimRight(strings.Join(lines[start:], "\n"), "\n")
}

func sendContinueMessage(
	ctx context.Context,
	client tmux.API,
	target string,
	message string,
	submitKey string,
	fallbackSubmitKey string,
	fallbackDelay float64,
	clearBeforeTyping bool,
) error {
	return SendContinueMessage(ctx, client, target, message, submitKey, fallbackSubmitKey, fallbackDelay, clearBeforeTyping)
}

func sendChoiceMessage(
	ctx context.Context,
	client tmux.API,
	target string,
	choice string,
	submitKey string,
	fallbackSubmitKey string,
	fallbackDelay float64,
	clearBeforeTyping bool,
) error {
	return SendChoiceMessage(ctx, client, target, choice, submitKey, fallbackSubmitKey, fallbackDelay, clearBeforeTyping)
}

func sendInputMessage(
	ctx context.Context,
	client tmux.API,
	target string,
	input string,
	submitKey string,
	fallbackSubmitKey string,
	fallbackDelay float64,
	clearBeforeTyping bool,
) error {
	return SendInputMessage(ctx, client, target, input, submitKey, fallbackSubmitKey, fallbackDelay, clearBeforeTyping)
}

func sendSubmitOnly(
	ctx context.Context,
	client tmux.API,
	target string,
	submitKey string,
	fallbackSubmitKey string,
	fallbackDelay float64,
) error {
	return SendSubmitOnly(ctx, client, target, submitKey, fallbackSubmitKey, fallbackDelay)
}

func clearPromptState(
	ctx context.Context,
	client tmux.API,
	target string,
) error {
	return ClearPromptState(ctx, client, target)
}

func RunOfflineAnalysis(ctx context.Context, provider llm.Provider, llmName string, llmModel string, raw io.Reader) (prompt.Analysis, llmDecision, error) {
	data, err := io.ReadAll(raw)
	if err != nil {
		return prompt.Analysis{}, llmDecision{}, err
	}
	ansi := string(data)
	plain := capture.StripANSI(ansi)
	analysis := prompt.AnalyzeWithHint(provider.Name(), ansi, plain)
	decision, err := classifyWithLLMFallback(ctx, provider, llmName, llmModel, analysis, plain)
	return analysis, decision, err
}
