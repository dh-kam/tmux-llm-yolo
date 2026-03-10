package runtime

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/dh-kam/tmux-llm-yolo/internal/capture"
	"github.com/dh-kam/tmux-llm-yolo/internal/llm"
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
	Once                bool
}

type Runner struct {
	cfg               Config
	client            tmux.API
	fetcher           capture.Fetcher
	continuePlan      continueStrategy
	logger            func(string, ...interface{})
	queue             []task
	state             string
	ui                *tui.UI
	ctx               context.Context
	deadline          time.Time
	currentTask       task
	lastEvent         string
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
}

const minimumStableWaitingDuration = 20 * time.Second

type task interface {
	Name() string
	Description() string
	Run(*Runner) error
}

func New(cfg Config, client tmux.API, logger func(string, ...interface{})) *Runner {
	if cfg.BaseInterval <= 0 {
		cfg.BaseInterval = 5 * time.Second
	}
	if cfg.SuspectWait1 <= 0 {
		cfg.SuspectWait1 = 5 * time.Second
	}
	if cfg.SuspectWait2 <= 0 {
		cfg.SuspectWait2 = 5 * time.Second
	}
	if cfg.SuspectWait3 <= 0 {
		cfg.SuspectWait3 = 5 * time.Second
	}
	return &Runner{
		cfg:          cfg,
		client:       client,
		fetcher:      capture.Fetcher{Client: client},
		continuePlan: newContinueStrategy(cfg.ContinueMessage),
		logger:       logger,
		state:        stateMonitoring,
	}
}

func (r *Runner) Run(ctx context.Context) error {
	r.ctx = ctx
	r.deadline = time.Now().Add(r.cfg.Duration)
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
	r.deadline = time.Now().Add(r.cfg.Duration)
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

	switch analysis.Classification {
	case prompt.ClassContinueAfterDone:
		return r.injectContinueOnce("once mode: 완료 후 다음 진행 요청으로 감지됨")
	case prompt.ClassNumberedMultipleChoice:
		choice := prompt.ParseNumericChoice(analysis.RecommendedChoice)
		if choice == "" {
			choice = "1"
		}
		return r.injectChoiceOnce(choice, "once mode: 번호형 선택지로 감지됨")
	case prompt.ClassCursorBasedChoice:
		return r.injectChoiceOnce("Enter", "once mode: 커서 기반 선택 UI로 감지되어 현재 선택 항목을 Enter로 확정")
	case prompt.ClassCompletedNoOp:
		r.setState(stateCompleted, "once mode: 추가 입력 없이 종료")
		return nil
	case prompt.ClassFreeTextRequest, prompt.ClassUnknownWaiting:
		decision, err := r.classifyWithLLM(ctx, analysis, snap.Plain)
		if err != nil {
			return r.injectContinueOnce("once mode: LLM 해석 실패로 기본 continue 입력")
		}

		switch strings.ToUpper(strings.TrimSpace(decision.Action)) {
		case "SKIP":
			r.setState(stateCompleted, "once mode: 추가 입력 없이 종료")
			return nil
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
		State:       r.state,
		Scope:       r.scopeLine(),
		Policy:      r.policyLine(),
		CurrentTask: currentTask,
		CurrentDesc: currentDesc,
		NextTask:    r.nextTaskName(),
		LLMStatus:   r.llmStatusLine(),
		SleepReason: r.sleepReason,
		SleepUntil:  r.sleepUntil,
		Deadline:    r.deadline,
		LastEvent:   r.lastEvent,
		LastUpdated: time.Now(),
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

func (r *Runner) policyLine() string {
	parts := []string{
		fmt.Sprintf("wait=%s->%s->%s->%s", r.baseInterval().Round(time.Second), r.suspectWait1().Round(time.Second), r.suspectWait2().Round(time.Second), r.suspectWait3().Round(time.Second)),
		fmt.Sprintf("continue=%d sent,next-audit=%d", r.continueSentCount, r.continuePlan.nextAuditIn(r.continueSentCount)),
	}
	llmPlan := "llm=primary"
	if strings.TrimSpace(r.cfg.FallbackLLMName) != "" {
		llmPlan = "llm=primary->fallback"
	}
	parts = append(parts, llmPlan)
	return strings.Join(parts, " | ")
}

func (r *Runner) promptProviderHint() string {
	candidates := []string{
		strings.ToLower(strings.TrimSpace(r.cfg.Target)),
		strings.ToLower(strings.TrimSpace(r.cfg.LLMName)),
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

func (r *Runner) submitKey() string {
	if r.promptProviderHint() == "copilot" && strings.EqualFold(strings.TrimSpace(r.cfg.SubmitKey), "C-m") {
		return "C-s"
	}
	return r.cfg.SubmitKey
}

func (r *Runner) submitKeyFallback() string {
	if r.promptProviderHint() == "copilot" {
		return ""
	}
	return r.cfg.SubmitKeyFallback
}

func (r *Runner) clearPromptBeforeTyping() bool {
	switch r.promptProviderHint() {
	case "codex", "copilot":
		return true
	default:
		return false
	}
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
		return 5 * time.Second
	}
	return r.cfg.BaseInterval
}

func (r *Runner) suspectWait1() time.Duration {
	if r.cfg.SuspectWait1 <= 0 {
		return 5 * time.Second
	}
	return r.cfg.SuspectWait1
}

func (r *Runner) suspectWait2() time.Duration {
	if r.cfg.SuspectWait2 <= 0 {
		return 5 * time.Second
	}
	return r.cfg.SuspectWait2
}

func (r *Runner) suspectWait3() time.Duration {
	if r.cfg.SuspectWait3 <= 0 {
		return 5 * time.Second
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
	if strings.TrimSpace(r.cfg.FallbackLLMName) != "" {
		parts = append(parts, fmt.Sprintf("fallback=%s", r.providerState(r.cfg.FallbackLLMName, r.cfg.FallbackLLMModel, r.fallbackInitDone, r.fallbackInitErr)))
	}
	if strings.TrimSpace(r.lastLLMProvider) != "" {
		parts = append(parts, "active="+r.lastLLMProvider)
	}
	return strings.Join(parts, " | ")
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
	r.setState(stateMonitoring, "base dual capture")
	snap, err := r.fetcher.CaptureDual(r.ctx, r.cfg.Target, r.cfg.CaptureLines)
	if err != nil {
		return fmt.Errorf("base dual capture failed: %w", err)
	}
	if strings.TrimSpace(r.prevBase.ANSI) == "" {
		r.prevBase = snap
		r.enqueue(
			sleepTask{duration: r.baseInterval(), reason: fmt.Sprintf("첫 기준 ANSI 캡처 저장 완료, 다음 %s 주기 대기", r.baseInterval().Round(time.Second))},
			checkDeadlineTask{},
			baseCaptureTask{},
		)
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
	ansi, err := r.fetcher.CaptureANSI(r.ctx, r.cfg.Target, r.cfg.CaptureLines)
	if err != nil {
		return fmt.Errorf("ansi recheck failed: %w", err)
	}
	if ansi != t.referenceANSI {
		r.setState(stateMonitoring, fmt.Sprintf("의심 단계 중 ANSI 화면이 바뀌어 %s 모니터링으로 복귀", r.baseInterval().Round(time.Second)))
		r.prevBase = capture.Snapshot{ANSI: ansi, TakenAt: time.Now()}
		r.enqueue(
			sleepTask{duration: r.baseInterval(), reason: fmt.Sprintf("ANSI 변경 감지, 다시 %s 기준 캡처 대기", r.baseInterval().Round(time.Second))},
			checkDeadlineTask{},
			baseCaptureTask{},
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
		r.setState(r.suspectState(nextStage), fmt.Sprintf("ANSI 화면은 동일하며 최소 안정 시간 %s를 채우기 위해 %d차 입력 대기 의심으로 진행", r.minStableWaitingDuration().Round(time.Second), nextStage))
		r.enqueue(
			sleepTask{duration: waitMore, reason: fmt.Sprintf("%d차 입력 대기 의심, %s 후 ANSI 재확인", nextStage, waitMore.Round(time.Second))},
			checkDeadlineTask{},
			ansiRecheckTask{stage: nextStage, referenceANSI: ansi},
		)
		return nil
	}

	r.setState(stateConfidentWaiting, "20초 동안 5회 ANSI 화면이 동일하여 입력 대기 상태를 확신")
	r.enqueue(checkDeadlineTask{}, analyzeWaitingTask{referenceANSI: ansi})
	return nil
}

type analyzeWaitingTask struct {
	referenceANSI string
}

func (t analyzeWaitingTask) Name() string { return "InterpretWaitingStateTask" }
func (t analyzeWaitingTask) Description() string {
	return "prompt 위치와 마지막 출력 블록을 해석해 다음 동작을 결정한다"
}
func (t analyzeWaitingTask) Run(r *Runner) error {
	r.setState(stateInterpreting, "waiting 화면 해석 시작")
	snap, err := r.fetcher.CaptureDual(r.ctx, r.cfg.Target, r.cfg.CaptureLines)
	if err != nil {
		return fmt.Errorf("analysis dual capture failed: %w", err)
	}
	if snap.ANSI != t.referenceANSI {
		r.prevBase = snap
		r.setState(stateMonitoring, "해석 직전 화면이 바뀌어 모니터링으로 복귀")
		r.enqueue(
			sleepTask{duration: r.baseInterval(), reason: fmt.Sprintf("해석 직전 ANSI 변경 감지, 다시 %s 모니터링", r.baseInterval().Round(time.Second))},
			checkDeadlineTask{},
			baseCaptureTask{},
		)
		return nil
	}

	analysis := prompt.AnalyzeWithHintAndWidth(r.promptProviderHint(), snap.ANSI, snap.Plain, r.paneWidth())
	r.logger("prompt analysis: provider=%s assistant_ui=%t processing=%t detected=%t line=%d class=%s reason=%s choice=%s", analysis.Provider, analysis.AssistantUI, analysis.Processing, analysis.PromptDetected, analysis.PromptLine, analysis.Classification, analysis.Reason, analysis.RecommendedChoice)
	if block := strings.TrimSpace(analysis.OutputBlock); block != "" {
		r.logger("latest output block:\n%s", block)
	}
	if !analysis.AssistantUI {
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

	switch analysis.Classification {
	case prompt.ClassContinueAfterDone:
		return r.injectContinue("완료 후 다음 진행 요청으로 감지됨")
	case prompt.ClassNumberedMultipleChoice:
		choice := prompt.ParseNumericChoice(analysis.RecommendedChoice)
		if choice == "" {
			choice = "1"
		}
		return r.injectChoice(choice, "번호형 선택지로 감지됨")
	case prompt.ClassCursorBasedChoice:
		return r.injectCursorConfirm("커서 기반 선택 UI로 감지되어 현재 선택 항목을 Enter로 확정")
	case prompt.ClassCompletedNoOp:
		r.setState(stateCompleted, "추가 작업 없음으로 판단되어 watcher 종료")
		r.queue = nil
		return errStop
	case prompt.ClassFreeTextRequest, prompt.ClassUnknownWaiting:
		decision, err := r.classifyWithLLM(r.ctx, analysis, snap.Plain)
		if err != nil {
			return r.injectContinue("LLM 해석 실패로 기본 continue 입력")
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
	r.sleepUntil = time.Now().Add(t.duration)
	r.updateUI()
	timer := time.NewTimer(t.duration)
	defer timer.Stop()
	select {
	case <-r.ctx.Done():
		return r.ctx.Err()
	case <-timer.C:
		r.sleepUntil = time.Time{}
		r.sleepReason = ""
		return nil
	}
}

func (r *Runner) injectContinue(reason string) error {
	message := r.prepareTextInput(r.nextContinueMessage())
	r.setState(stateActing, reason)
	r.logger("continue prompt selected: count=%d message=%q", r.continueSentCount, message)
	if err := sendContinueMessage(r.ctx, r.client, r.cfg.Target, message, r.submitKey(), r.submitKeyFallback(), r.cfg.SubmitFallbackDelay, r.clearPromptBeforeTyping()); err != nil {
		return err
	}
	r.prevBase = capture.Snapshot{}
	r.enqueue(
		sleepTask{duration: r.baseInterval(), reason: fmt.Sprintf("입력 전송 후 다음 %s 모니터링 대기", r.baseInterval().Round(time.Second))},
		checkDeadlineTask{},
		baseCaptureTask{},
	)
	return nil
}

func (r *Runner) injectContinueOnce(reason string) error {
	message := r.prepareTextInput(r.nextContinueMessage())
	r.setState(stateActing, reason)
	r.logger("continue prompt selected: count=%d message=%q", r.continueSentCount, message)
	if err := sendContinueMessage(r.ctx, r.client, r.cfg.Target, message, r.submitKey(), r.submitKeyFallback(), r.cfg.SubmitFallbackDelay, r.clearPromptBeforeTyping()); err != nil {
		return err
	}
	r.setState(stateStopped, "once mode: continue 입력 후 종료")
	return nil
}

func (r *Runner) injectChoice(choice string, reason string) error {
	r.setState(stateActing, reason)
	if err := sendChoiceMessage(r.ctx, r.client, r.cfg.Target, choice, r.submitKey(), r.submitKeyFallback(), r.cfg.SubmitFallbackDelay, r.clearPromptBeforeTyping()); err != nil {
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
	if err := sendChoiceMessage(r.ctx, r.client, r.cfg.Target, choice, r.submitKey(), r.submitKeyFallback(), r.cfg.SubmitFallbackDelay, r.clearPromptBeforeTyping()); err != nil {
		return err
	}
	r.setState(stateStopped, "once mode: 선택 입력 후 종료")
	return nil
}

func (r *Runner) injectCursorConfirm(reason string) error {
	r.setState(stateActing, reason)
	if err := sendChoiceMessage(r.ctx, r.client, r.cfg.Target, "Enter", r.submitKey(), r.submitKeyFallback(), r.cfg.SubmitFallbackDelay, r.clearPromptBeforeTyping()); err != nil {
		return err
	}
	r.prevBase = capture.Snapshot{}
	r.enqueue(
		sleepTask{duration: r.baseInterval(), reason: fmt.Sprintf("커서형 선택 확정 후 다음 %s 모니터링 대기", r.baseInterval().Round(time.Second))},
		checkDeadlineTask{},
		baseCaptureTask{},
	)
	return nil
}

func (r *Runner) injectInputOnce(input string, reason string) error {
	input = r.prepareTextInput(input)
	r.setState(stateActing, reason)
	if err := sendInputMessage(r.ctx, r.client, r.cfg.Target, input, r.submitKey(), r.submitKeyFallback(), r.cfg.SubmitFallbackDelay, r.clearPromptBeforeTyping()); err != nil {
		return err
	}
	r.setState(stateStopped, "once mode: 입력값 전송 후 종료")
	return nil
}

func (r *Runner) submitPendingInput(reason string) error {
	r.setState(stateActing, reason)
	if err := sendSubmitOnly(r.ctx, r.client, r.cfg.Target, r.submitKey(), r.submitKeyFallback(), r.cfg.SubmitFallbackDelay); err != nil {
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
	if err := sendSubmitOnly(r.ctx, r.client, r.cfg.Target, r.submitKey(), r.submitKeyFallback(), r.cfg.SubmitFallbackDelay); err != nil {
		return err
	}
	r.setState(stateStopped, "once mode: submit-only 실행 후 종료")
	return nil
}

func (r *Runner) clearPromptState(reason string) error {
	r.setState(stateActing, reason)
	if err := clearPromptState(r.ctx, r.client, r.cfg.Target); err != nil {
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
	if err := clearPromptState(r.ctx, r.client, r.cfg.Target); err != nil {
		return err
	}
	r.setState(stateStopped, "once mode: 입력창 정리 후 종료")
	return nil
}

type llmDecision struct {
	Action            string
	RecommendedChoice string
	Reason            string
}

func (r *Runner) applyLLMDecision(decision llmDecision) error {
	switch decision.Action {
	case "INJECT_SELECT":
		choice := prompt.ParseNumericChoice(decision.RecommendedChoice)
		if choice == "" {
			choice = "1"
		}
		return r.injectChoice(choice, decision.Reason)
	case "INJECT_INPUT":
		r.setState(stateActing, decision.Reason)
		if err := sendInputMessage(r.ctx, r.client, r.cfg.Target, r.prepareTextInput(decision.RecommendedChoice), r.submitKey(), r.submitKeyFallback(), r.cfg.SubmitFallbackDelay, r.clearPromptBeforeTyping()); err != nil {
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

func (r *Runner) nextContinueMessage() string {
	r.continueSentCount++
	return r.continuePlan.messageFor(r.continueSentCount)
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

Return exactly 3 lines:
ACTION: INJECT_CONTINUE | INJECT_SELECT | INJECT_INPUT | SKIP
RECOMMENDED_CHOICE: <number, text, or none>
REASON: <one short sentence>

Rules:
- Use INJECT_SELECT only for numbered menus.
- Use INJECT_INPUT for specific free-text input requests.
- Use INJECT_CONTINUE for completion handoff or safe generic continuation.
- Use SKIP only if the screen clearly says there is nothing left to do.
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
		case strings.HasPrefix(strings.ToUpper(line), "REASON:"):
			decision.Reason = strings.TrimSpace(line[len("REASON:"):])
		}
	}
	decision.Action = strings.ToUpper(strings.TrimSpace(decision.Action))
	if decision.RecommendedChoice == "none" {
		decision.RecommendedChoice = ""
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
	if inMode, err := client.IsPaneInMode(ctx, target); err == nil && inMode {
		if err := client.SendKeys(ctx, target, "-X", "cancel"); err != nil {
			return err
		}
	}
	if clearBeforeTyping {
		if err := client.SendKeys(ctx, target, "C-u"); err != nil {
			return err
		}
	}
	if err := client.SendKeys(ctx, target, "-l", message); err != nil {
		return err
	}
	if err := client.SendKeys(ctx, target, submitKey); err != nil {
		return err
	}
	if fallbackSubmitKey != "" {
		if fallbackDelay > 0 {
			time.Sleep(time.Duration(fallbackDelay * float64(time.Second)))
		}
		if err := client.SendKeys(ctx, target, fallbackSubmitKey); err != nil {
			return err
		}
	}
	return nil
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
	if inMode, err := client.IsPaneInMode(ctx, target); err == nil && inMode {
		if err := client.SendKeys(ctx, target, "-X", "cancel"); err != nil {
			return err
		}
	}
	if strings.EqualFold(choice, "Enter") {
		key := submitKey
		if strings.TrimSpace(key) == "" {
			key = "C-m"
		}
		if err := client.SendKeys(ctx, target, key); err != nil {
			return err
		}
		return nil
	}
	if _, err := strconv.Atoi(strings.TrimSpace(choice)); err != nil {
		return fmt.Errorf("invalid choice: %s", choice)
	}
	if clearBeforeTyping {
		if err := client.SendKeys(ctx, target, "C-u"); err != nil {
			return err
		}
	}
	if err := client.SendKeys(ctx, target, "-l", choice); err != nil {
		return err
	}
	if err := client.SendKeys(ctx, target, submitKey); err != nil {
		return err
	}
	if fallbackSubmitKey != "" {
		if fallbackDelay > 0 {
			time.Sleep(time.Duration(fallbackDelay * float64(time.Second)))
		}
		if err := client.SendKeys(ctx, target, fallbackSubmitKey); err != nil {
			return err
		}
	}
	return nil
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
	input = strings.TrimSpace(input)
	if input == "" {
		return fmt.Errorf("empty input")
	}
	if inMode, err := client.IsPaneInMode(ctx, target); err == nil && inMode {
		if err := client.SendKeys(ctx, target, "-X", "cancel"); err != nil {
			return err
		}
	}
	if clearBeforeTyping {
		if err := client.SendKeys(ctx, target, "C-u"); err != nil {
			return err
		}
	}
	if err := client.SendKeys(ctx, target, "-l", input); err != nil {
		return err
	}
	if err := client.SendKeys(ctx, target, submitKey); err != nil {
		return err
	}
	if fallbackSubmitKey != "" {
		if fallbackDelay > 0 {
			time.Sleep(time.Duration(fallbackDelay * float64(time.Second)))
		}
		if err := client.SendKeys(ctx, target, fallbackSubmitKey); err != nil {
			return err
		}
	}
	return nil
}

func sendSubmitOnly(
	ctx context.Context,
	client tmux.API,
	target string,
	submitKey string,
	fallbackSubmitKey string,
	fallbackDelay float64,
) error {
	if inMode, err := client.IsPaneInMode(ctx, target); err == nil && inMode {
		if err := client.SendKeys(ctx, target, "-X", "cancel"); err != nil {
			return err
		}
	}
	key := strings.TrimSpace(submitKey)
	if key == "" {
		key = "C-m"
	}
	if err := client.SendKeys(ctx, target, key); err != nil {
		return err
	}
	if strings.TrimSpace(fallbackSubmitKey) != "" {
		if fallbackDelay > 0 {
			time.Sleep(time.Duration(fallbackDelay * float64(time.Second)))
		}
		if err := client.SendKeys(ctx, target, fallbackSubmitKey); err != nil {
			return err
		}
	}
	return nil
}

func clearPromptState(
	ctx context.Context,
	client tmux.API,
	target string,
) error {
	if inMode, err := client.IsPaneInMode(ctx, target); err == nil && inMode {
		if err := client.SendKeys(ctx, target, "-X", "cancel"); err != nil {
			return err
		}
	}
	if err := client.SendKeys(ctx, target, "Escape"); err != nil {
		return err
	}
	time.Sleep(120 * time.Millisecond)
	if err := client.SendKeys(ctx, target, "C-u"); err != nil {
		return err
	}
	return nil
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
