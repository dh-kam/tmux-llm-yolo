package sessioncheck

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dh-kam/yollo/internal/capture"
	"github.com/dh-kam/yollo/internal/i18n"
	"github.com/dh-kam/yollo/internal/llm"
	"github.com/dh-kam/yollo/internal/tmux"
)

type Decision struct {
	Action            string
	Status            string
	Working           bool
	MultiChoice       bool
	Completed         bool
	RecommendedChoice string
	Reason            string
}

func IsCompletedCapturePath(path string) bool {
	name := strings.ToLower(strings.TrimSpace(filepath.Base(path)))
	if name == "" || strings.HasPrefix(name, ".") {
		return false
	}
	if strings.HasSuffix(name, ".completed") {
		return true
	}
	marker := ".completed."
	idx := strings.Index(name, marker)
	if idx < 0 {
		return false
	}
	suffix := strings.TrimPrefix(name[idx+len(".completed"):], ".")
	if suffix == "" {
		return false
	}
	parts := strings.Split(suffix, ".")
	if len(parts) == 0 || len(parts) > 2 {
		return false
	}
	allowed := map[string]struct{}{
		"ansi":    {},
		"plain":   {},
		"capture": {},
		"txt":     {},
	}
	for _, part := range parts {
		if _, ok := allowed[part]; !ok {
			return false
		}
	}
	if len(parts) == 1 {
		return parts[0] == "capture" || parts[0] == "txt"
	}
	return (parts[1] == "capture" || parts[1] == "txt") && parts[0] != "txt"
}

func CompletedCaptureDecision(path string, locale string) Decision {
	return Decision{
		Action:    "INJECT_CONTINUE",
		Status:    "COMPLETED",
		Completed: true,
		Reason:    i18n.T(locale, "cmd.reason_completed_fixture", filepath.Base(strings.TrimSpace(path))),
	}
}

func BuildNeedToContinuePrompt(captureForLLM string, llmName string, llmModel string) string {
	return BuildNeedToContinuePromptForLocale(captureForLLM, llmName, llmModel, i18n.DefaultAppLocale)
}

func BuildNeedToContinuePromptForLocale(captureForLLM string, llmName string, llmModel string, locale string) string {
	spec := needContinuePromptSpecForLocale(locale)
	return fmt.Sprintf(`You are a strict classifier.
Determine the terminal state and classify user intent from the following terminal capture.
Context assumptions:
- This output is from an autonomous terminal session; we only know what's on screen.
- We want to decide whether to inject a new user action now, and if needed which action to inject.
- "%s" means a command is still active, progressing, or the screen is showing a non-terminal prompt state.
- %s
- For provider %s/%s, prefer WAITING/COMPLETED + INJECT_CONTINUE for short natural language confirmations of readiness.
- Write REASON in %s.

	Return exactly 6 lines in this format:
	ACTION: INJECT_CONTINUE, INJECT_SELECT, INJECT_INPUT, or SKIP
	STATUS: WORKING | ASKING | COMPLETED | WAITING | UNKNOWN
	WORKING: true or false
	MULTIPLE_CHOICE: true or false
	RECOMMENDED_CHOICE: <number or number + option label>
	REASON: <one short sentence>

	Rules:
	- ACTION should be INJECT_CONTINUE when it looks like the model is awaiting a free-text response or can safely proceed after completion.
	- ACTION may be INJECT_INPUT when a specific free-text value should be entered directly (including a short command-like token).
	- ACTION should be INJECT_SELECT when numbered objectives are shown and one choice number should be typed.
	- For INJECT_SELECT, RECOMMENDED_CHOICE should include option number and label when possible (e.g. "3) Exit").
	- STATUS=ASKING when options like "1) ..., 2) ..., 3) ..." are shown and the output asks for exactly one answer.
	- For ASKING/INJECT_SELECT, MULTIPLE_CHOICE=true and RECOMMENDED_CHOICE should be the highest-priority option number (usually 1 if no stronger signal).
	- Ignore footer lines like "Use /skills", "Context compacted", "? for shortcuts", and percentage context left.
	- WORKING signals include long-running commands, spinners, progress bars, running tests/builds, and text like "esc to interrupt", %s, "processing", "press ctrl+c".
	- If working signals exist, STATUS should be WORKING and ACTION SKIP, even if any choice-like text is also present.
	- Ignore footer lines like "Use /skills", "Context compacted", "? for shortcuts", and percentage context left.
	- COMPLETED/WAITING means short completion or handoff prompts such as %s.
	- If unclear, set STATUS=UNKNOWN and ACTION=SKIP.
	- REASON must include concrete evidence from the capture in one short sentence.
	- Do not output anything except those 6 lines.

	Few-shot examples:
	- Example 1:
		Terminal:
		1) Build project
		2) Run tests
		3) Exit
		Which one do you want?
		ACTION: INJECT_SELECT
		STATUS: ASKING
		WORKING: false
		MULTIPLE_CHOICE: true
		RECOMMENDED_CHOICE: 1
		REASON: %s

	- Example 2:
		Terminal:
		Tests finished with warnings.
		%s
		ACTION: INJECT_CONTINUE
		STATUS: COMPLETED
		WORKING: false
		MULTIPLE_CHOICE: false
		RECOMMENDED_CHOICE: none
		REASON: %s

	- Example 3:
		Terminal:
		Running lint...
		████████ 78%% [elapsed: 00:01:12] esc to interrupt
		ACTION: SKIP
		STATUS: WORKING
		WORKING: true
		MULTIPLE_CHOICE: false
		RECOMMENDED_CHOICE: none
		REASON: %s

[Terminal Output Start]
%s
[Terminal Output End]
%s`, spec.workingLabel, spec.userIntentLine, llmName, llmModel, spec.reasonLanguage, spec.workingSignals, spec.completionExamples, spec.example1Reason, spec.example2Prompt, spec.example2Reason, spec.example3Reason, captureForLLM, llmPromptHint(llmName, llmModel))
}

func CheckTmuxSessionState(
	ctx context.Context,
	p llm.Provider,
	client tmux.API,
	target string,
	logger func(string, ...interface{}),
	locale string,
	writeFile func(string, []byte) error,
	promptFile string,
	stateFile string,
) string {
	sessions, err := client.ListSessions(ctx)
	if err != nil {
		logger(i18n.T(locale, "cmd.warn_tmux_session_list_failed", err))
		exists, hasErr := client.HasSession(ctx, target)
		if hasErr != nil {
			logger(i18n.T(locale, "cmd.warn_tmux_session_fallback", hasErr))
			return "MISSING"
		}
		if exists {
			return "EXISTS"
		}
		return "MISSING"
	}

	var names []string
	for _, session := range sessions {
		names = append(names, session.Name)
	}

	prompt := fmt.Sprintf(`You are a strict binary classifier.
Decide whether the target tmux session currently exists among the listed sessions.

Return exactly 1 line in this format:
STATE: EXISTS or MISSING

Rules:
- Return EXISTS only if the target session name is present.
- Return MISSING if the target session name is not present.
- Output only that one line, nothing else.

Target session: %s
Sessions:
%s
`, target, strings.Join(names, "\n"))

	if writeFile != nil {
		if err := writeFile(promptFile, []byte(prompt)); err != nil {
			logger(i18n.T(locale, "cmd.warn_session_state_prompt_save", promptFile, err))
		}
	}

	startAt := time.Now()
	logger(i18n.T(locale, "cmd.watch_session_state_request", p.Binary(), len(prompt)))
	out, err := p.RunPrompt(ctx, prompt)
	elapsed := time.Since(startAt)
	if err != nil {
		logger(i18n.T(locale, "cmd.watch_session_state_response_fail", err, elapsed))
		exists, hasErr := client.HasSession(ctx, target)
		if hasErr != nil {
			logger(i18n.T(locale, "cmd.warn_tmux_session_fallback", hasErr))
			return "MISSING"
		}
		if exists {
			return "EXISTS"
		}
		return "MISSING"
	}
	logger(i18n.T(locale, "cmd.watch_session_state_response_ok", elapsed))
	if writeFile != nil {
		if err := writeFile(stateFile, []byte(out)); err != nil {
			logger(i18n.T(locale, "cmd.warn_session_state_result_save", stateFile, err))
		}
	}

	if strings.Contains(out, "EXISTS") {
		return "EXISTS"
	}
	if strings.Contains(out, "MISSING") {
		return "MISSING"
	}

	logger(i18n.T(locale, "cmd.warn_session_state_parse_failed", out))
	exists, hasErr := client.HasSession(ctx, target)
	if hasErr != nil {
		logger(i18n.T(locale, "cmd.warn_tmux_session_fallback", hasErr))
		return "MISSING"
	}
	if exists {
		return "EXISTS"
	}
	return "MISSING"
}

func ClassifyNeedToContinue(
	ctx context.Context,
	p llm.Provider,
	capture string,
	logger func(string, ...interface{}),
	locale string,
	writeFile func(string, []byte) error,
	promptPath string,
	decisionPath string,
	capturePath string,
	llmName string,
	llmModel string,
) Decision {
	return ClassifyNeedToContinueFromReader(
		ctx,
		p,
		strings.NewReader(capture),
		logger,
		locale,
		writeFile,
		promptPath,
		decisionPath,
		capturePath,
		llmName,
		llmModel,
	)
}

func ClassifyNeedToContinueFromReader(
	ctx context.Context,
	p llm.Provider,
	capture io.Reader,
	logger func(string, ...interface{}),
	locale string,
	writeFile func(string, []byte) error,
	promptPath string,
	decisionPath string,
	capturePath string,
	llmName string,
	llmModel string,
) Decision {
	providerName := strings.TrimSpace(llmName)
	if providerName == "" {
		providerName = p.Name()
	}

	captureObj, err := llm.NewCaptureFromReader(capture)
	if err != nil {
		return Decision{
			Action: "SKIP",
			Status: "UNKNOWN",
			Reason: i18n.T(locale, "cmd.reason_capture_read_failed"),
		}
	}
	captureText := captureObj.Text
	captureForLLM := stripANSICodesAll(captureText)
	captureForLLM = stripCodexFooterNoise(captureForLLM)
	captureForLLM = trimTailLines(captureForLLM, 25)
	trimmedCapture := strings.TrimSpace(captureForLLM)
	if trimmedCapture == "" {
		return Decision{
			Action:  "SKIP",
			Status:  "EMPTY",
			Reason:  i18n.T(locale, "cmd.reason_empty_capture"),
			Working: false,
		}
	}

	if IsCompletedCapturePath(capturePath) {
		return CompletedCaptureDecision(capturePath, locale)
	}

	if working, evidence := llm.IsProgressingCaptureForLocale(p, llm.NewCapture(captureText), locale); working {
		if strings.TrimSpace(evidence) == "" {
			evidence = i18n.T(locale, "cmd.reason_progress_default_evidence")
		}
		return Decision{
			Action:  "SKIP",
			Status:  "WORKING",
			Working: true,
			Reason:  i18n.T(locale, "cmd.reason_working", evidence),
		}
	}

	prompt := BuildNeedToContinuePromptForLocale(captureForLLM, providerName, llmModel, locale)
	if writeFile != nil {
		if err := writeFile(promptPath, []byte(prompt)); err != nil {
			logger(i18n.T(locale, "cmd.warn_classify_prompt_save", promptPath, err))
		}
	}

	startAt := time.Now()
	logger(i18n.T(locale, "cmd.classify_request", p.Name(), p.Binary(), len(prompt)))
	out, err := p.RunPrompt(ctx, prompt)
	elapsed := time.Since(startAt)
	if err != nil {
		logger(i18n.T(locale, "cmd.classify_response_fail", err, elapsed))
		if writeFile != nil {
			_ = writeFile(decisionPath, []byte(fmt.Sprintf("ERROR: %s", err)))
		}
		return Decision{
			Action: "SKIP",
			Status: "UNKNOWN",
			Reason: i18n.T(locale, "cmd.reason_llm_exec_failed", err),
		}
	}
	logger(i18n.T(locale, "cmd.classify_response_ok", elapsed))
	if writeFile != nil {
		if err := writeFile(decisionPath, []byte(out)); err != nil {
			logger(i18n.T(locale, "cmd.warn_classify_result_save", decisionPath, err))
		}
	}

	decision := ParseDecision(out, locale)
	if decision.Status == "WORKING" && isContinuationReadySignal(captureForLLM) {
		logger(i18n.T(locale, "watch.reason_completion_ready_transition"))
		decision.Status = "COMPLETED"
		decision.Completed = true
		decision.Working = false
		if decision.Action == "SKIP" {
			decision.Action = "INJECT_CONTINUE"
		}
	}
	if decision.Reason == "" {
		decision.Reason = i18n.T(locale, "cmd.reason_no_reason")
	}
	return decision
}

func ParseDecision(out string, locale string) Decision {
	result := Decision{
		Action: "SKIP",
		Status: "UNKNOWN",
		Reason: i18n.T(locale, "cmd.reason_no_reason"),
	}

	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		upper := strings.ToUpper(trimmed)
		switch {
		case strings.HasPrefix(upper, "ACTION:"):
			result.Action = strings.TrimSpace(trimmed[len("ACTION:"):])
		case strings.HasPrefix(upper, "STATUS:"):
			result.Status = strings.TrimSpace(trimmed[len("STATUS:"):])
		case strings.HasPrefix(upper, "WORKING:"):
			result.Working = ParseTruthy(strings.TrimSpace(trimmed[len("WORKING:"):]))
		case strings.HasPrefix(upper, "MULTIPLE_CHOICE:"):
			result.MultiChoice = ParseTruthy(strings.TrimSpace(trimmed[len("MULTIPLE_CHOICE:"):]))
		case strings.HasPrefix(upper, "RECOMMENDED_CHOICE:"):
			value := strings.TrimSpace(trimmed[len("RECOMMENDED_CHOICE:"):])
			if value != "" && !strings.EqualFold(value, "none") {
				result.RecommendedChoice = value
			}
		case strings.HasPrefix(upper, "REASON:"):
			result.Reason = strings.TrimSpace(trimmed[len("REASON:"):])
		}
	}

	status := strings.ToUpper(strings.TrimSpace(result.Status))
	switch status {
	case "WORKING":
		result.Working = true
	case "ASKING", "MULTIPLE_CHOICE":
		result.MultiChoice = true
	case "COMPLETED":
		result.Completed = true
	}
	switch strings.ToUpper(strings.TrimSpace(result.Action)) {
	case "SEND", "INJECT_CONTINUE":
		result.Action = "INJECT_CONTINUE"
	case "INJECT_INPUT":
		result.Action = "INJECT_INPUT"
	case "INJECT_SELECT":
		result.Action = "INJECT_SELECT"
	case "SKIP":
		if status == "COMPLETED" || status == "WAITING" {
			result.Action = "INJECT_CONTINUE"
		} else {
			result.Action = "SKIP"
		}
	default:
		result.Action = "SKIP"
	}
	if result.MultiChoice && result.RecommendedChoice != "" && !result.Working {
		result.Action = "INJECT_SELECT"
	}
	if result.Status == "WORKING" {
		result.Working = true
	}
	if result.Working {
		result.Action = "SKIP"
	}
	if strings.TrimSpace(out) != "" && (result.Reason == i18n.T(locale, "cmd.reason_no_reason") || result.Reason == i18n.T(i18n.DefaultLocale, "cmd.reason_no_reason")) {
		result.Reason = strings.Join(strings.Fields(out), " ")
	}
	return result
}

func NormalizeChoiceCandidate(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	value = strings.Fields(value)[0]
	value = strings.TrimSuffix(strings.TrimSuffix(value, "."), ")")
	if isNumericChoice(value) {
		return value
	}
	numPrefix := 0
	for numPrefix < len(value) {
		ch := value[numPrefix]
		if ch < '0' || ch > '9' {
			break
		}
		numPrefix++
	}
	if numPrefix > 0 {
		return value[:numPrefix]
	}
	return value
}

func ParseChoiceCandidateAndLabel(raw string) (string, string) {
	choice := NormalizeChoiceCandidate(strings.TrimSpace(raw))
	if choice == "" || !isNumericChoice(choice) {
		return "", ""
	}
	label := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), strings.TrimSpace(choice)))
	label = strings.TrimLeft(label, ").:-")
	label = strings.TrimSpace(label)
	return choice, label
}

func ParseTruthy(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

type needContinuePromptSpec struct {
	workingLabel       string
	userIntentLine     string
	workingSignals     string
	completionExamples string
	example2Prompt     string
	example1Reason     string
	example2Reason     string
	example3Reason     string
	reasonLanguage     string
}

func promptReasonLanguage(locale string) string {
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

func needContinuePromptSpecForLocale(locale string) needContinuePromptSpec {
	spec := needContinuePromptSpec{
		workingLabel:       "working",
		userIntentLine:     "The user may be asking for free-text input, a post-completion follow-up input, or a multiple-choice selection.",
		workingSignals:     `"working", "in progress"`,
		completionExamples: `"next input", "next step", "Ready for next step", "what would you like me to do next"`,
		example2Prompt:     "Should I proceed with the next task? (yes / no)",
		example1Reason:     "A numbered menu is visible and asks for exactly one choice.",
		example2Reason:     "The screen looks completed and is waiting for the next input.",
		example3Reason:     "Progress indicators and an interrupt hint show the task is still running.",
		reasonLanguage:     promptReasonLanguage(locale),
	}
	if i18n.NormalizeLocale(locale) == "ko" {
		spec.workingLabel = "작업중"
		spec.userIntentLine = "사용자 요청은 자유 응답, 완료 후 다음 입력 대기, 또는 객관식 선택지 중 하나일 수 있다."
		spec.workingSignals = `"작업 중", "진행 중"`
		spec.completionExamples = `"원하면", "그럼 다음", "다음 작업", "Ready for next step", "what would you like me to do next"`
		spec.example2Prompt = "다음 작업을 진행할까요? (yes / no)"
		spec.example1Reason = "번호형 메뉴가 표시되고 하나의 번호 입력이 요구됨."
		spec.example2Reason = "완료 후 다음 입력을 기다리는 완료 상태로 판단됨."
		spec.example3Reason = "진행률/진행중 표기와 인터럽트 안내가 있어 아직 작업이 진행 중임."
	}
	return spec
}

func llmPromptHint(name string, model string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case "gemini":
		return `
LLM-specific note: For gemini, prioritize WAITING/COMPLETED only when the visible text clearly indicates completion handoff.
Avoid false positives from progress/status metadata.`
	case "codex":
		return `
LLM-specific note: For codex, return exactly six lines and only from terminal evidence.
Prefer ACTION=INJECT_CONTINUE with STATUS=COMPLETED/WAITING when a prompt is ready for free input.`
	case "copilot":
		return `
LLM-specific note: For copilot, treat short completion confirmations (yes/no, proceed?, next?) as STATUS=WAITING or COMPLETED.
Output INJECT_CONTINUE if the screen is not working.`
	case "glm":
		return `
LLM-specific note: For glm, classify only by terminal content and set COMPLETED/WAITING when a handoff prompt is shown.`
	case "ollama":
		normalizedModel := strings.TrimSpace(model)
		if normalizedModel == "" {
			normalizedModel = "default"
		}
		return fmt.Sprintf(`
LLM-specific note: For ollama/%s, prefer STATUS=COMPLETED and ACTION=INJECT_CONTINUE when ready for next input.
If screen is fully idle, do not return WORKING/ASKING.`, normalizedModel)
	default:
		return ""
	}
}

func isNumericChoice(raw string) bool {
	value := strings.TrimSpace(raw)
	if value == "" || strings.EqualFold(value, "none") {
		return false
	}
	value = strings.TrimSuffix(value, ".")
	value = strings.TrimSuffix(value, ")")
	_, err := strconv.Atoi(value)
	return err == nil
}

func isContinuationReadySignal(raw string) bool {
	text := strings.TrimSpace(strings.ToLower(raw))
	if text == "" {
		return false
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isCodexFooterLine(trimmed) {
			continue
		}
		compact := strings.Join(strings.Fields(trimmed), " ")
		if strings.Contains(compact, "원하면") {
			if strings.Contains(compact, "다음") || strings.Contains(compact, "진행") || strings.Contains(compact, "작업") || strings.Contains(compact, "확장") {
				return true
			}
		}
		if strings.Contains(compact, "그럼 다음") || strings.Contains(compact, "다음 꺼") || strings.Contains(compact, "next input") || strings.Contains(compact, "next step") {
			return true
		}
		if strings.Contains(compact, "다음 작업") && (strings.Contains(compact, "진행") || strings.Contains(compact, "요청")) {
			return true
		}
	}
	return false
}

func trimTailLines(value string, maxLines int) string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	if maxLines <= 0 || len(lines) <= maxLines {
		return strings.TrimRight(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	}
	start := len(lines) - maxLines
	return strings.TrimRight(strings.Join(lines[start:], "\n"), "\n")
}

func stripANSICodesAll(value string) string {
	return capture.StripANSI(value)
}

var codexFooterLinePattern = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^›?\s*Use\s+/skills\b.*$`),
	regexp.MustCompile(`(?i)^•\s*Context\s+compacted$`),
	regexp.MustCompile(`(?i)^\?\s*for\s+shortcuts\b.*$`),
	regexp.MustCompile(`(?i)^\d+\s*%\s*context\s+left\b.*$`),
}

func stripCodexFooterNoise(value string) string {
	normalized := strings.ReplaceAll(value, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	end := len(lines)
	for end > 0 {
		line := strings.TrimSpace(lines[end-1])
		if line == "" || isCodexFooterLine(line) {
			end--
			continue
		}
		break
	}
	return strings.TrimRight(strings.Join(lines[:end], "\n"), "\n")
}

func isCodexFooterLine(line string) bool {
	for _, pattern := range codexFooterLinePattern {
		if pattern.MatchString(line) {
			return true
		}
	}
	return false
}
