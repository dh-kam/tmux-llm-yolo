package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/dh-kam/tmux-llm-yolo/internal/capture"
	"github.com/dh-kam/tmux-llm-yolo/internal/i18n"
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
	Locale              string
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
	DryRun              bool
	DryRunOutputFormat  string
	LogBuffer           *tui.LogBuffer
}

type Runner struct {
	cfg               Config
	locale            string
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
	cfg.Locale = i18n.NormalizeLocale(cfg.Locale)
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
		locale:       cfg.Locale,
		client:       client,
		fetcher:      capture.Fetcher{Client: client},
		executor:     newActionExecutor(client, cfg, actionProviderHint(cfg)),
		continuePlan: newContinueStrategyWithPolicy(activePolicy, cfg.ContinueMessage, cfg.Locale),
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

	r.enqueue(checkDeadlineTask{locale: r.locale}, baseCaptureTask{locale: r.locale})

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

func (r *Runner) t(key string, args ...interface{}) string {
	return i18n.T(r.locale, key, args...)
}

func (r *Runner) RunOnce(ctx context.Context) error {
	r.ctx = ctx
	r.deadline = time.Now().Add(r.watchDuration())
	r.ui = tui.Start(ctx)
	defer r.ui.Stop()

	r.setState(stateInterpreting, r.t("watch.state_once_interpret"))
	snap, err := r.fetcher.CaptureDual(ctx, r.cfg.Target, r.cfg.CaptureLines)
	if err != nil {
		return fmt.Errorf("once dual capture failed: %w", err)
	}
	r.prevBase = snap

	analysis := prompt.AnalyzeWithHintAndLocaleAndWidth(r.promptProviderHint(), snap.ANSI, snap.Plain, r.locale, r.paneWidth())
	r.logger(r.t("watch.log_once_prompt_analysis"), analysis.Provider, analysis.AssistantUI, analysis.Processing, analysis.PromptDetected, analysis.PromptLine, analysis.Classification, analysis.Reason, analysis.RecommendedChoice)
	if block := strings.TrimSpace(analysis.OutputBlock); block != "" {
		r.logger(r.t("watch.log_once_latest_output_block"), block)
	}
	if !analysis.AssistantUI {
		r.setState(stateCompleted, r.t("watch.state_once_no_assistant"))
		return nil
	}
	if r.hasCopilotSlashCommandState(analysis) {
		return r.clearPromptStateOnce(r.t("watch.state_once_copilot_clear"))
	}
	if analysis.Processing {
		r.setState(stateCompleted, r.t("watch.state_once_processing"))
		return nil
	}
	if r.shouldReplacePendingPromptInput(analysis) {
		return r.injectContinueOnce(r.t("watch.state_once_replace_copilot"))
	}
	if r.hasPendingPromptInput(analysis) {
		if r.shouldReplacePendingPromptInput(analysis) {
			return r.injectContinueOnce(r.t("watch.state_once_replace_copilot"))
		}
		return r.submitPendingInputOnce(r.t("watch.state_once_submit_pending"))
	}
	if plan, ok := deterministicActionPlan(analysis, r.t("watch.state_once_plan"), r.cfg.Locale); ok {
		return r.executeActionPlan(plan, true)
	}

	switch analysis.Classification {
	case prompt.ClassFreeTextRequest:
		fallthrough
	case prompt.ClassUnknownWaiting:
		decision, err := r.classifyWithLLM(ctx, analysis, snap.Plain)
		if err != nil {
			return r.injectContinueOnce(r.t("watch.state_once_llm_fail"))
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
			return r.injectContinueOnce(r.t("watch.interpret_unknown"))
		}
	default:
		return r.injectContinueOnce(r.t("watch.state_once_unknown"))
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

type checkDeadlineTask struct {
	locale string
}

func (t checkDeadlineTask) Name() string { return "CheckDeadlineTask" }
func (t checkDeadlineTask) Description() string {
	return i18n.T(t.locale, "watch.task_check_deadline_description")
}
func (t checkDeadlineTask) Run(r *Runner) error {
	if time.Now().After(r.deadline) {
		r.setState(stateStopped, r.t("watch.reason_watch_deadline_exceeded"))
		r.queue = nil
		return errStop
	}
	return nil
}

type baseCaptureTask struct {
	locale string
}

func (t baseCaptureTask) Name() string { return "CaptureTask" }
func (t baseCaptureTask) Description() string {
	return i18n.T(t.locale, "watch.task_base_capture_description")
}
func (t baseCaptureTask) Run(r *Runner) error {
	r.sleepUntil = time.Time{}
	r.sleepReason = ""
	r.setStateQuiet(stateMonitoring)
	snap, err := r.fetcher.CaptureDual(r.ctx, r.cfg.Target, r.cfg.CaptureLines)
	if err != nil {
		return fmt.Errorf("base dual capture failed: %w", err)
	}
	r.recordANSISnapshot(snap.ANSI, snap.TakenAt)
	analysis := prompt.AnalyzeWithHintAndLocale(r.promptProviderHint(), snap.ANSI, snap.Plain, r.locale)
	summary := r.recordScreenSnapshot(snap, analysis)
	if strings.TrimSpace(r.prevBase.ANSI) == "" {
		r.prevBase = snap
		r.enqueue(
			sleepTask{
				duration: r.baseInterval(),
				reason:   r.t("watch.reason_capture_capture_init", r.baseInterval().Round(time.Second)),
			},
			checkDeadlineTask{locale: r.locale},
			baseCaptureTask{locale: r.locale},
		)
		return nil
	}
	if r.shouldBypassWaitingStability(snap, analysis, summary) {
		r.setState(stateConfidentWaiting, r.t("watch.reason_bypass_prompt_wait"))
		r.prevBase = snap
		r.enqueue(checkDeadlineTask{locale: r.locale}, analyzeWaitingTask{
			locale:              r.locale,
			referenceANSI:       snap.ANSI,
			referencePromptZone: summary.PromptZoneFingerprint,
			allowVolatileANSI:   true,
		})
		return nil
	}
	if snap.ANSI != r.prevBase.ANSI {
		r.prevBase = snap
		r.enqueue(
			sleepTask{
				duration: r.baseInterval(),
				reason:   r.t("watch.reason_capture_changed", r.baseInterval().Round(time.Second)),
			},
			checkDeadlineTask{locale: r.locale},
			baseCaptureTask{locale: r.locale},
		)
		return nil
	}

	if r.hasLongStableANSI(snap.ANSI, snap.TakenAt) {
		r.setState(stateConfidentWaiting, r.t("watch.reason_stable_promote", longStableANSIDuration.Round(time.Second)))
		r.enqueue(
			checkDeadlineTask{locale: r.locale},
			analyzeWaitingTask{
				locale:        r.locale,
				referenceANSI: snap.ANSI,
				forceInput:    true,
			},
		)
		return nil
	}

	r.setState(stateSuspectWaiting1, r.t("watch.reason_suspect_initial", r.baseInterval().Round(time.Second)))
	r.enqueue(
		sleepTask{
			duration: r.suspectWait1(),
			reason:   r.t("watch.reason_suspect_wait", 1, r.suspectWait1().Round(time.Second)),
		},
		checkDeadlineTask{locale: r.locale},
		ansiRecheckTask{
			locale:        r.locale,
			stage:         1,
			referenceANSI: snap.ANSI,
		},
	)
	return nil
}

type ansiRecheckTask struct {
	stage         int
	locale        string
	referenceANSI string
}

func (t ansiRecheckTask) Name() string { return "CompareCaptureTask" }
func (t ansiRecheckTask) Description() string {
	return i18n.T(t.locale, "watch.task_ansi_recheck_description", t.stage)
}
func (t ansiRecheckTask) Run(r *Runner) error {
	snap, err := r.fetcher.CaptureDual(r.ctx, r.cfg.Target, r.cfg.CaptureLines)
	if err != nil {
		return fmt.Errorf("ansi recheck failed: %w", err)
	}
	r.recordANSISnapshot(snap.ANSI, snap.TakenAt)
	analysis := prompt.AnalyzeWithHintAndLocale(r.promptProviderHint(), snap.ANSI, snap.Plain, r.locale)
	summary := r.recordScreenSnapshot(snap, analysis)
	if r.shouldBypassWaitingStability(snap, analysis, summary) {
		r.setState(stateConfidentWaiting, r.t("watch.reason_bypass_prompt_wait"))
		r.prevBase = snap
		r.enqueue(checkDeadlineTask{locale: t.locale}, analyzeWaitingTask{
			locale:              t.locale,
			referenceANSI:       snap.ANSI,
			referencePromptZone: summary.PromptZoneFingerprint,
			allowVolatileANSI:   true,
		})
		return nil
	}
	if snap.ANSI != t.referenceANSI {
		r.setState(stateMonitoring, r.t("watch.reason_capture_changed_wait", r.baseInterval().Round(time.Second)))
		r.prevBase = snap
		r.enqueue(
			sleepTask{
				duration: r.baseInterval(),
				reason:   r.t("watch.sleep_capture_changed"),
			},
			checkDeadlineTask{locale: r.locale},
			baseCaptureTask{locale: r.locale},
		)
		return nil
	}

	if r.hasLongStableANSI(snap.ANSI, snap.TakenAt) {
		r.setState(stateConfidentWaiting, r.t("watch.reason_stable_promote", longStableANSIDuration.Round(time.Second)))
		r.enqueue(
			checkDeadlineTask{locale: r.locale},
			analyzeWaitingTask{
				locale:        t.locale,
				referenceANSI: snap.ANSI,
				forceInput:    true,
			},
		)
		return nil
	}

	elapsed := time.Since(r.prevBase.TakenAt)
	if elapsed < r.minStableWaitingDuration() {
		nextStage := t.stage + 1
		waitMore := r.suspectWaitForStage(nextStage)
		if remaining := r.minStableWaitingDuration() - elapsed; remaining > 0 && remaining < waitMore {
			waitMore = remaining
		}
		r.setState(r.suspectState(nextStage), r.t("watch.reason_min_stable_time", r.minStableWaitingDuration().Round(time.Second), nextStage))
		r.enqueue(
			sleepTask{
				duration: waitMore,
				reason:   r.t("watch.reason_suspect_wait", nextStage, waitMore.Round(time.Second)),
			},
			checkDeadlineTask{locale: r.locale},
			ansiRecheckTask{
				locale:        r.locale,
				stage:         nextStage,
				referenceANSI: snap.ANSI,
			},
		)
		return nil
	}

	r.setState(stateConfidentWaiting, r.t("watch.reason_confident_by_repetition"))
	r.enqueue(
		checkDeadlineTask{locale: r.locale},
		analyzeWaitingTask{
			locale:        t.locale,
			referenceANSI: snap.ANSI,
		},
	)
	return nil
}

type analyzeWaitingTask struct {
	locale              string
	referenceANSI       string
	referencePromptZone string
	allowVolatileANSI   bool
	forceInput          bool
}

func (t analyzeWaitingTask) Name() string { return "InterpretWaitingStateTask" }
func (t analyzeWaitingTask) Description() string {
	if t.forceInput {
		return i18n.T(t.locale, "watch.task_interpret_force_input_description")
	}
	if t.allowVolatileANSI {
		return i18n.T(t.locale, "watch.task_interpret_interactive_description")
	}
	return i18n.T(t.locale, "watch.task_interpret_default_description")
}
func (t analyzeWaitingTask) Run(r *Runner) error {
	r.setState(stateInterpreting, r.t("watch.interpret_start"))
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
		r.setState(stateMonitoring, r.t("watch.interpret_prompt_changed"))
		r.enqueue(
			sleepTask{
				duration: r.baseInterval(),
				reason:   r.t("watch.interpret_prompt_changed_wait", r.baseInterval().Round(time.Second)),
			},
			checkDeadlineTask{locale: r.locale},
			baseCaptureTask{locale: r.locale},
		)
		return nil
	}

analyze:
	analysis := prompt.AnalyzeWithHintAndLocaleAndWidth(r.promptProviderHint(), snap.ANSI, snap.Plain, r.locale, r.paneWidth())
	r.logger(r.t("watch.log_prompt_analysis"), analysis.Provider, analysis.AssistantUI, analysis.Processing, analysis.InteractivePrompt, analysis.PromptDetected, analysis.PromptLine, analysis.Classification, analysis.Reason, analysis.RecommendedChoice)
	if block := strings.TrimSpace(analysis.OutputBlock); block != "" {
		r.logger(r.t("watch.log_latest_output_block"), block)
	}
	if !analysis.AssistantUI {
		if t.forceInput {
			return r.injectContinue(r.t("watch.interpret_assistant_signature_weak"))
		}
		r.prevBase = snap
		r.setState(stateMonitoring, r.t("watch.interpret_no_assistant_monitor", r.baseInterval().Round(time.Second)))
		r.enqueue(
			sleepTask{
				duration: r.baseInterval(),
				reason:   r.t("watch.sleep_no_assistant_monitor", r.baseInterval().Round(time.Second)),
			},
			checkDeadlineTask{locale: r.locale},
			baseCaptureTask{locale: r.locale},
		)
		return nil
	}
	if r.hasCopilotSlashCommandState(analysis) {
		return r.clearPromptState(r.t("watch.interpret_copilot_clear"))
	}
	if analysis.Processing {
		if t.forceInput {
			return r.injectContinue(r.t("watch.interpret_force_input_override"))
		}
		r.prevBase = snap
		r.setState(stateMonitoring, r.t("watch.interpret_processing_wait", r.baseInterval().Round(time.Second)))
		r.enqueue(
			sleepTask{
				duration: r.baseInterval(),
				reason:   r.t("watch.sleep_processing_wait", r.baseInterval().Round(time.Second)),
			},
			checkDeadlineTask{locale: r.locale},
			baseCaptureTask{locale: r.locale},
		)
		return nil
	}
	if r.shouldReplacePendingPromptInput(analysis) {
		return r.injectContinue(r.t("watch.interpret_replace_pending"))
	}
	if r.hasPendingPromptInput(analysis) {
		return r.submitPendingInput(r.t("watch.interpret_submit_pending"))
	}
	if plan, ok := deterministicActionPlan(analysis, r.t("watch.interpret_plan"), r.cfg.Locale); ok {
		return r.executeActionPlan(plan, false)
	}

	switch analysis.Classification {
	case prompt.ClassFreeTextRequest:
		fallthrough
	case prompt.ClassUnknownWaiting:
		decision, err := r.classifyWithLLM(r.ctx, analysis, snap.Plain)
		if err != nil {
			return r.injectContinue(r.t("watch.interpret_llm_fail"))
		}
		if t.forceInput && strings.EqualFold(strings.TrimSpace(decision.Action), "SKIP") {
			decision.Action = "INJECT_CONTINUE"
			if strings.TrimSpace(decision.Reason) == "" {
				decision.Reason = r.t("watch.interpret_force_input_override")
			}
		}
		return r.applyLLMDecision(decision)
	default:
		return r.injectContinue(r.t("watch.interpret_unknown"))
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
	r.logger(r.t("watch.log_continue_prompt_selected"), nextCount, message)
	if err := r.getExecutor().SendContinue(r.ctx, continueRequest{Message: message}); err != nil {
		return err
	}
	r.continueSentCount = nextCount
	r.prevBase = capture.Snapshot{}
	r.enqueue(
		sleepTask{
			duration: r.baseInterval(),
			reason:   r.t("watch.sleep_after_send", r.baseInterval().Round(time.Second)),
		},
		checkDeadlineTask{locale: r.locale},
		baseCaptureTask{locale: r.locale},
	)
	return nil
}

func (r *Runner) injectContinueOnce(reason string) error {
	nextCount := r.continueSentCount + 1
	message := r.prepareTextInput(r.nextContinueMessage(nextCount))
	r.setState(stateActing, reason)
	r.logger(r.t("watch.log_continue_prompt_selected"), nextCount, message)
	if err := r.getExecutor().SendContinue(r.ctx, continueRequest{Message: message}); err != nil {
		return err
	}
	r.continueSentCount = nextCount
	r.setState(stateStopped, r.t("watch.once_after_input"))
	return nil
}

func (r *Runner) injectChoice(choice string, reason string) error {
	r.setState(stateActing, reason)
	if err := r.getExecutor().SendChoice(r.ctx, choiceRequest{Choice: choice}); err != nil {
		return err
	}
	r.prevBase = capture.Snapshot{}
	r.enqueue(
		sleepTask{
			duration: r.baseInterval(),
			reason:   r.t("watch.sleep_after_choice", r.baseInterval().Round(time.Second)),
		},
		checkDeadlineTask{locale: r.locale},
		baseCaptureTask{locale: r.locale},
	)
	return nil
}

func (r *Runner) injectChoiceOnce(choice string, reason string) error {
	r.setState(stateActing, reason)
	if err := r.getExecutor().SendChoice(r.ctx, choiceRequest{Choice: choice}); err != nil {
		return err
	}
	r.setState(stateStopped, r.t("watch.once_after_choice"))
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
		sleepTask{
			duration: r.baseInterval(),
			reason:   r.t("watch.sleep_after_cursor", r.baseInterval().Round(time.Second)),
		},
		checkDeadlineTask{locale: r.locale},
		baseCaptureTask{locale: r.locale},
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
	r.setState(stateStopped, r.t("watch.once_after_cursor"))
	return nil
}

func (r *Runner) cursorChoiceFallback(reason string, cause error) error {
	r.logger(r.t("watch.log_cursor_choice_failed"), cause)
	fallback := strings.TrimSpace(i18n.T(r.cfg.Locale, "watch.fallback_cursor"))
	if strings.Contains(fallback, "%s") {
		if strings.TrimSpace(reason) == "" {
			reason = r.t("watch.fallback_cursor_subject")
		}
		fallback = fmt.Sprintf(fallback, strings.TrimSpace(reason))
	} else if reason != "" {
		fallback = reason + " " + fallback
	}
	return r.injectContinue(fallback)
}

func (r *Runner) cursorChoiceFallbackOnce(reason string, cause error) error {
	r.logger(r.t("watch.log_cursor_choice_failed_once"), cause)
	fallback := strings.TrimSpace(i18n.T(r.cfg.Locale, "watch.fallback_cursor"))
	if strings.Contains(fallback, "%s") {
		if strings.TrimSpace(reason) == "" {
			reason = r.t("watch.fallback_cursor_subject")
		}
		fallback = fmt.Sprintf(fallback, strings.TrimSpace(reason))
	} else if reason != "" {
		fallback = reason + " " + fallback
	}
	return r.injectContinueOnce(fallback)
}

func (r *Runner) injectInputOnce(input string, reason string) error {
	input = r.prepareTextInput(input)
	if input == "" {
		fallbackReason := r.emptyInputFallbackReason(reason)
		r.logger(r.t("watch.log_empty_recommended_input_once"), reason)
		return r.injectContinueOnce(fallbackReason)
	}
	r.setState(stateActing, reason)
	if err := r.getExecutor().SendInput(r.ctx, inputRequest{Input: input}); err != nil {
		return err
	}
	r.setState(stateStopped, r.t("watch.once_after_input_submit"))
	return nil
}

func (r *Runner) submitPendingInput(reason string) error {
	r.setState(stateActing, reason)
	if err := r.getExecutor().SubmitPending(r.ctx, submitRequest{}); err != nil {
		return err
	}
	r.prevBase = capture.Snapshot{}
	r.enqueue(
		sleepTask{
			duration: r.baseInterval(),
			reason:   r.t("watch.sleep_after_submit", r.baseInterval().Round(time.Second)),
		},
		checkDeadlineTask{locale: r.locale},
		baseCaptureTask{locale: r.locale},
	)
	return nil
}

func (r *Runner) submitPendingInputOnce(reason string) error {
	r.setState(stateActing, reason)
	if err := r.getExecutor().SubmitPending(r.ctx, submitRequest{}); err != nil {
		return err
	}
	r.setState(stateStopped, r.t("watch.once_after_submit"))
	return nil
}

func (r *Runner) clearPromptState(reason string) error {
	r.setState(stateActing, reason)
	if err := r.getExecutor().ClearPrompt(r.ctx, clearPromptRequest{}); err != nil {
		return err
	}
	r.prevBase = capture.Snapshot{}
	r.enqueue(
		sleepTask{
			duration: r.baseInterval(),
			reason:   r.t("watch.sleep_after_clear", r.baseInterval().Round(time.Second)),
		},
		checkDeadlineTask{locale: r.locale},
		baseCaptureTask{locale: r.locale},
	)
	return nil
}

func (r *Runner) clearPromptStateOnce(reason string) error {
	r.setState(stateActing, reason)
	if err := r.getExecutor().ClearPrompt(r.ctx, clearPromptRequest{}); err != nil {
		return err
	}
	r.setState(stateStopped, r.t("watch.once_after_clear"))
	return nil
}

func (r *Runner) emptyInputFallbackReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return strings.TrimSpace(i18n.T(r.cfg.Locale, "watch.fallback_empty_input"))
	}
	return reason + " " + strings.TrimSpace(i18n.T(r.cfg.Locale, "watch.fallback_empty_input"))
}

func (r *Runner) skipFallbackReason(reason string, once bool) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		if once {
			return strings.TrimSpace(i18n.T(r.cfg.Locale, "watch.fallback_skip_once"))
		}
		return strings.TrimSpace(i18n.T(r.cfg.Locale, "watch.fallback_skip_watch"))
	}
	return reason + " " + strings.TrimSpace(i18n.T(r.cfg.Locale, "watch.fallback_skip"))
}

func continueMessageFromPlannedItems(analysis prompt.Analysis, locale string) string {
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
	return i18n.T(locale, "watch.plan_input_plan", firstNumber, firstText)
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
			r.logger(r.t("watch.log_empty_recommended_input"), decision.Action, decision.Reason)
			return r.injectContinue(r.emptyInputFallbackReason(decision.Reason))
		}
		r.setState(stateActing, decision.Reason)
		if err := r.getExecutor().SendInput(r.ctx, inputRequest{Input: input}); err != nil {
			return err
		}
		r.prevBase = capture.Snapshot{}
		r.enqueue(
			sleepTask{
				duration: r.baseInterval(),
				reason:   r.t("watch.sleep_after_input_value", r.baseInterval().Round(time.Second)),
			},
			checkDeadlineTask{locale: r.locale},
			baseCaptureTask{locale: r.locale},
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
		decision, err := classifyWithLLMFallbackForLocale(ctx, provider, candidate.llmName, candidate.llmModel, analysis, plainCapture, r.locale)
		if err == nil {
			r.lastLLMProvider = candidate.label + ":" + candidate.llmName
			r.updateUI()
			return decision, nil
		}
		r.logger(r.t("watch.log_llm_fallback_failed"), candidate.label, candidate.llmName, candidate.llmModel, err)
		errs = append(errs, fmt.Sprintf("%s=%v", candidate.label, err))
		r.lastLLMProvider = candidate.label + ":" + candidate.llmName + ":failed"
		r.updateUI()
	}
	if len(errs) == 0 {
		return llmDecision{}, errors.New(r.t("watch.error_no_llm_provider"))
	}
	return llmDecision{}, errors.New(r.t("watch.error_all_llm_providers_failed", strings.Join(errs, "; ")))
}

func (r *Runner) getPrimaryProvider(ctx context.Context) (llm.Provider, error) {
	if r.primaryInitDone {
		return r.primaryProvider, r.primaryInitErr
	}
	r.primaryInitDone = true
	r.primaryProvider, r.primaryInitErr = initializeProvider(ctx, r.cfg.LLMName, r.cfg.LLMModel, r.locale, r.logger, "primary")
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
	r.fallbackProvider, r.fallbackInitErr = initializeProvider(ctx, r.cfg.FallbackLLMName, r.cfg.FallbackLLMModel, r.locale, r.logger, "fallback")
	return r.fallbackProvider, r.fallbackInitErr
}

func initializeProvider(ctx context.Context, name string, model string, locale string, logger func(string, ...interface{}), role string) (llm.Provider, error) {
	provider, err := llm.New(name, model)
	if err != nil {
		return nil, err
	}
	binary, err := provider.ValidateBinary()
	if err != nil {
		return nil, errors.New(i18n.T(locale, "watch.error_llm_binary_validate", err))
	}
	usage, err := provider.CheckUsage(ctx)
	if err != nil {
		return nil, err
	}
	if usage.HasKnownLimit && usage.Remaining <= 0 {
		return nil, errors.New(i18n.T(locale, "watch.error_llm_quota_exhausted", name, usage.Source))
	}
	if usage.HasKnownLimit {
		logger(i18n.T(locale, "watch.log_llm_lazy_init_usage"), role, name, model, binary, usage.Source, usage.Remaining)
	} else {
		logger(i18n.T(locale, "watch.log_llm_lazy_init"), role, name, model, binary, usage.Source)
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
		r.logger(r.t("watch.log_continue_override"), message)
		return
	}
	r.logger(r.t("watch.log_continue_override_from"), source, message)
}

func classifyWithLLMFallback(ctx context.Context, provider llm.Provider, llmName string, llmModel string, analysis prompt.Analysis, plainCapture string) (llmDecision, error) {
	return classifyWithLLMFallbackForLocale(ctx, provider, llmName, llmModel, analysis, plainCapture, i18n.DefaultAppLocale)
}

func classifyWithLLMFallbackForLocale(ctx context.Context, provider llm.Provider, llmName string, llmModel string, analysis prompt.Analysis, plainCapture string, locale string) (llmDecision, error) {
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
Write CONTINUE_MESSAGE and REASON in %s.

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
`, waitingClassifierResponseLanguage(locale), promptText, outputBlock, trimTailLines(plainCapture, 20))

	out, err := provider.RunPrompt(ctx, rawPrompt)
	if err != nil {
		return llmDecision{}, err
	}
	return parseLLMDecisionForLocale(out, locale), nil
}

func parseLLMDecision(raw string) llmDecision {
	return parseLLMDecisionForLocale(raw, i18n.DefaultAppLocale)
}

func parseLLMDecisionForLocale(raw string, locale string) llmDecision {
	decision := llmDecision{
		Action: "INJECT_CONTINUE",
		Reason: i18n.T(locale, "watch.reason_llm_fallback_default_continue"),
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

func waitingClassifierResponseLanguage(locale string) string {
	switch i18n.NormalizeLocale(locale) {
	case "ko":
		return "Korean"
	case "ja":
		return "Japanese"
	case "zh":
		return "Chinese"
	case "vi":
		return "Vietnamese"
	case "hi":
		return "Hindi"
	case "ru":
		return "Russian"
	case "es":
		return "Spanish"
	case "fr":
		return "French"
	default:
		return "English"
	}
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
	analysis := prompt.AnalyzeWithHintAndLocale(provider.Name(), ansi, plain, i18n.DefaultAppLocale)
	decision, err := classifyWithLLMFallback(ctx, provider, llmName, llmModel, analysis, plain)
	return analysis, decision, err
}
