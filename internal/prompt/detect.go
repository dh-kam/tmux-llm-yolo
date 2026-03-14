package prompt

import (
	"regexp"
	"strconv"
	"strings"
)

type Classification string

const (
	ClassUnknownWaiting         Classification = "unknown_waiting"
	ClassContinueAfterDone      Classification = "continue_after_completion"
	ClassFreeTextRequest        Classification = "free_text_request"
	ClassNumberedMultipleChoice Classification = "numbered_multiple_choice"
	ClassCursorBasedChoice      Classification = "cursor_based_choice"
	ClassCompletedNoOp          Classification = "completed_no_further_action"
)

type Analysis struct {
	PromptDetected    bool
	PromptLine        int
	PromptText        string
	PromptActive      bool
	PromptPlaceholder bool
	OutputBlock       string
	Provider          string
	AssistantUI       bool
	Processing        bool
	InteractivePrompt bool
	Classification    Classification
	RecommendedChoice string
	Reason            string
}

var (
	promptPlaceholderPattern       = regexp.MustCompile(`(?i)^[[:space:]]*([›❯>]|[#$%])\s*(type|type here|enter|press|input|입력|입력하세요|message|답변|선택|select|질문|명령|command|placeholder|press enter|다음|continue).*`)
	promptMarkerPattern            = regexp.MustCompile(`^[[:space:]]*([›❯>]|[#$%])\s*$`)
	numberedMenuPattern            = regexp.MustCompile(`(?m)^[[:space:]]*(?:[›❯>]\s*)?(\d+)[\).]\s+.+$`)
	selectedNumberedMenuPattern    = regexp.MustCompile(`(?m)^[[:space:]]*[›❯>]\s*(\d+)[\).]\s+.+$`)
	numberedMenuOptionPattern      = regexp.MustCompile(`(?m)^[[:space:]]*(?:[›❯>]\s*)?(\d+)[\).]\s+(.+)$`)
	cursorMenuPattern              = regexp.MustCompile(`(?m)^[[:space:]]*([•*→]|-\>)\s+.+$`)
	selectionContextPattern        = regexp.MustCompile(`(?i)(which|choose|select|enter to select|type something|chat about this|do you want to proceed|allow|approve|approved?|permission|bash command|read file|read\(|allow reading|during this session|don.?t ask again|yes[, ]|no[, ]|어떤 .*작업|어떤 .*개선|무엇을 할까요|선택지|선택하세요|선택 항목|선택할)`)
	approvalPromptPattern          = regexp.MustCompile(`(?i)(do you want to proceed|bash command|read file|read\(|allow reading|during this session|allow|approve|permission|don.?t ask again|esc to cancel|tab to amend)`)
	approvalPersistentAllowPattern = regexp.MustCompile(`(?i)(don.?t ask again|always allow|remember (?:this )?(?:choice|decision)|allow this command(?: pattern)?|approve and remember|allow .* during this session|yes,? and don.?t ask again)`)
	approvalAllowPattern           = regexp.MustCompile(`(?i)^(yes|allow|approve|proceed|run|continue)\b`)
	approvalRejectPattern          = regexp.MustCompile(`(?i)^(no|deny|reject|cancel)\b`)
	freeTextPattern                = regexp.MustCompile(`(?i)(enter|input|type|reply|respond|what should|provide|입력|답변|응답|작성)`)
	continuePattern                = regexp.MustCompile(`(?i)(next step|next input|next turn|continue|proceed|go ahead|원하면|다음 작업|다음 턴|다음 우선순위|다음 단계|대기 중인 작업|작업 완료 요약|진행할까요|계속 진행|이어서 진행|이어가겠|이어가겠습니다|무엇을 할까요|tasks \(\d+ done, \d+ open\))`)
	completedNoOpPattern           = regexp.MustCompile(`(?i)(all tasks (are )?complete|nothing more to do|no further action|done for now|모든 작업.*완료|더 할 일 없음|추가 작업 없음)`)
	separatorPattern               = regexp.MustCompile(`^[[:space:]─-]+$`)
	strongProcessingPattern        = regexp.MustCompile(`(?i)(esc to interrupt|esc to cancel|fermenting|thinking|refining implementation strategy|pre-heating the servers|ctrl\+b ctrl\+b|processing\.\.\.|processing request|processing your request|tokens\))`)
	weakProcessingPattern          = regexp.MustCompile(`(?i)(readfolder|readfile|writefile|search\(|listed \d+ item|tool uses|implementing|running)`)
	codexSignaturePattern          = regexp.MustCompile(`(?i)(openai codex|gpt-5\.[0-9]|/model to change|use /skills|context left|tab to queue)`)
	copilotSignaturePattern        = regexp.MustCompile(`(?i)(github copilot|describe a task to get started|remaining reqs\.|copilot instructions found)`)
	geminiSignaturePattern         = regexp.MustCompile(`(?i)(gemini code assist|yolo ctrl\+y|type your message or @path/to/file|gemini 3)`)
	glmSignaturePattern            = regexp.MustCompile(`(?i)(claude code|fermenting|opus 4\.6|api usage billing)`)
	promptZoneSpinnerPattern       = regexp.MustCompile(`[●○◌◍◐◓◑◒•]`)
	promptZoneDigitsPattern        = regexp.MustCompile(`\b\d+\b`)
	promptZoneSpacePattern         = regexp.MustCompile(`\s+`)
)

func Analyze(ansiCapture string, plainCapture string) Analysis {
	return AnalyzeWithHintAndWidth("", ansiCapture, plainCapture, 0)
}

func AnalyzeWithHint(providerHint string, ansiCapture string, plainCapture string) Analysis {
	return AnalyzeWithHintAndWidth(providerHint, ansiCapture, plainCapture, 0)
}

func AnalyzeWithHintAndWidth(providerHint string, ansiCapture string, plainCapture string, paneWidth int) Analysis {
	ansiLines := strings.Split(normalize(ansiCapture), "\n")
	plainLines := strings.Split(normalize(plainCapture), "\n")
	detectedProvider := detectProviderFromCaptures(ansiCapture, plainCapture)
	provider := detectedProvider
	if provider == "" {
		provider = normalizeProviderHint(providerHint)
	}
	heuristics := heuristicsFor(provider)
	promptLine := heuristics.detectPromptLine(ansiLines, plainLines)
	if promptLine < 0 {
		promptLine = detectLastNonEmptyLine(plainLines)
	}

	analysis := Analysis{
		PromptDetected: promptLine >= 0,
		PromptLine:     promptLine,
	}
	if promptLine >= 0 && promptLine < len(plainLines) {
		analysis.PromptText = collectPromptText(promptLine, plainLines, paneWidth)
	}
	analysis.PromptActive = heuristics.isActivePromptLine(promptLine, ansiLines, plainLines)
	analysis.PromptPlaceholder = heuristics.isPlaceholderPromptLine(promptLine, ansiLines, plainLines)

	analysis.Provider = provider
	analysis.AssistantUI = heuristics.hasAssistantUI(ansiCapture, plainCapture)
	if !analysis.AssistantUI && provider != "" && provider == normalizeProviderHint(providerHint) && analysis.PromptDetected {
		analysis.AssistantUI = true
	}
	analysis.OutputBlock = extractOutputBlock(plainLines, promptLine)
	analysis.Processing = isProcessing(analysis, plainLines)
	analysis.Classification, analysis.RecommendedChoice, analysis.Reason = classify(analysis)
	analysis.InteractivePrompt = hasInteractivePrompt(analysis)
	return analysis
}

func collectPromptText(promptLine int, plainLines []string, paneWidth int) string {
	if promptLine < 0 || promptLine >= len(plainLines) {
		return ""
	}
	line := normalizeSpaces(strings.TrimRight(plainLines[promptLine], " "))
	if strings.TrimSpace(line) == "" {
		return ""
	}
	parts := []string{strings.TrimSpace(line)}
	if selectedNumberedMenuPattern.MatchString(parts[0]) {
		for i := promptLine + 1; i < len(plainLines) && len(parts) < 5; i++ {
			next := normalizeSpaces(strings.TrimRight(plainLines[i], " "))
			trimmed := strings.TrimSpace(next)
			if trimmed == "" {
				break
			}
			if !looksLikeMenuContinuation(trimmed) {
				break
			}
			parts = append(parts, trimmed)
		}
		return strings.Join(parts, "\n")
	}
	if paneWidth <= 0 {
		return parts[0]
	}
	if !startsPromptBlock(parts[0]) {
		return parts[0]
	}
	for i := promptLine + 1; i < len(plainLines) && len(parts) < 4; i++ {
		next := normalizeSpaces(strings.TrimRight(plainLines[i], " "))
		trimmed := strings.TrimSpace(next)
		if trimmed == "" || isNonContentLine(trimmed) {
			break
		}
		if startsPromptBlock(trimmed) {
			break
		}
		prev := normalizeSpaces(strings.TrimRight(plainLines[i-1], " "))
		if visualWidth(strings.TrimSpace(prev)) < paneWidth-8 && !looksLikeWrappedContinuation(next) {
			break
		}
		parts = append(parts, trimmed)
	}
	return strings.Join(parts, " ")
}

func looksLikeMenuContinuation(value string) bool {
	trimmed := normalizeSpaces(strings.TrimSpace(value))
	if trimmed == "" {
		return false
	}
	if numberedMenuPattern.MatchString(trimmed) {
		return true
	}
	lower := strings.ToLower(trimmed)
	return strings.Contains(lower, "esc to cancel") ||
		strings.Contains(lower, "tab to amend") ||
		strings.Contains(lower, "enter to select")
}

func startsPromptBlock(value string) bool {
	trimmed := normalizeSpaces(strings.TrimSpace(value))
	return strings.HasPrefix(trimmed, "›") ||
		strings.HasPrefix(trimmed, "❯") ||
		strings.HasPrefix(trimmed, "* ") ||
		strings.HasPrefix(trimmed, "> ")
}

func looksLikeWrappedContinuation(value string) bool {
	if value == "" {
		return false
	}
	return strings.HasPrefix(value, "  ") || strings.HasPrefix(value, "\t")
}

func visualWidth(value string) int {
	return len([]rune(normalizeSpaces(value)))
}

func normalizeProviderHint(providerHint string) string {
	switch strings.ToLower(strings.TrimSpace(providerHint)) {
	case "codex", "gemini", "glm", "copilot":
		return strings.ToLower(strings.TrimSpace(providerHint))
	default:
		return ""
	}
}

func detectLastNonEmptyLine(lines []string) int {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return i
		}
	}
	return -1
}

func extractOutputBlock(lines []string, promptLine int) string {
	if promptLine < 0 {
		promptLine = len(lines)
	}
	end := promptLine - 1
	for end >= 0 && isNonContentLine(lines[end]) {
		end--
	}
	if end < 0 {
		return ""
	}

	start := end
	blankRun := 0
	for start >= 0 {
		trimmed := strings.TrimSpace(lines[start])
		if trimmed == "" {
			blankRun++
			if blankRun >= 2 {
				start++
				break
			}
		} else {
			blankRun = 0
		}
		if isNonContentLine(trimmed) {
			start--
			continue
		}
		if promptPlaceholderPattern.MatchString(trimmed) || promptMarkerPattern.MatchString(trimmed) {
			start++
			break
		}
		if end-start >= 11 {
			break
		}
		start--
	}
	if start < 0 {
		start = 0
	}
	return strings.TrimSpace(strings.Join(lines[start:end+1], "\n"))
}

func classify(analysis Analysis) (Classification, string, string) {
	block := strings.TrimSpace(analysis.OutputBlock)
	promptText := strings.TrimSpace(analysis.PromptText)
	combined := strings.TrimSpace(strings.Join([]string{block, promptText}, "\n"))
	if combined == "" {
		return ClassUnknownWaiting, "", "프롬프트 주변 텍스트가 충분하지 않음"
	}

	if approvalPromptPattern.MatchString(combined) && selectedNumberedMenuPattern.MatchString(strings.Join([]string{block, promptText}, "\n")) {
		return ClassCursorBasedChoice, preferredApprovalChoice(combined), "승인 프롬프트의 커서 선택 메뉴가 감지됨"
	}

	if matches := numberedMenuPattern.FindAllStringSubmatch(strings.Join([]string{block, promptText}, "\n"), -1); len(matches) > 0 && selectionContextPattern.MatchString(combined) {
		return ClassNumberedMultipleChoice, matches[0][1], "번호형 선택지가 감지됨"
	}

	if !analysis.AssistantUI {
		return ClassUnknownWaiting, "", "assistant UI 시그니처가 없어 자동 입력 대상 화면으로 확신할 수 없음"
	}

	if cursorMenuPattern.MatchString(block) && (selectionContextPattern.MatchString(combined) || approvalPromptPattern.MatchString(combined)) {
		return ClassCursorBasedChoice, "", "커서형 선택 메뉴로 보이는 출력이 감지됨"
	}

	if analysis.Processing {
		return ClassUnknownWaiting, "", "진행중 신호가 감지되어 입력 대기 확정으로 보기 어려움"
	}

	if continuePattern.MatchString(combined) && analysis.AssistantUI && analysis.PromptActive {
		return ClassContinueAfterDone, "", "완료 후 다음 진행 여부를 묻는 문맥으로 해석됨"
	}

	if completedNoOpPattern.MatchString(combined) {
		return ClassCompletedNoOp, "", "추가 작업이 없다는 완료 요약으로 해석됨"
	}

	if analysis.PromptActive && promptMarkerPattern.MatchString(promptText) && numberedMenuPattern.MatchString(block) && !selectionContextPattern.MatchString(combined) && !approvalPromptPattern.MatchString(combined) {
		return ClassFreeTextRequest, "", "번호 목록은 계획 항목이고 하단 prompt는 자유 텍스트 입력창으로 해석됨"
	}

	if isEditablePromptText(promptText) && analysis.PromptActive {
		return ClassFreeTextRequest, "", "입력 가능한 프롬프트 박스가 감지됨"
	}

	if analysis.PromptActive && (freeTextPattern.MatchString(combined) || promptPlaceholderPattern.MatchString(promptText)) {
		return ClassFreeTextRequest, "", "자유 텍스트 입력 요청으로 해석됨"
	}

	return ClassUnknownWaiting, "", "결정적 규칙으로 분류하지 못함"
}

func hasInteractivePrompt(analysis Analysis) bool {
	switch analysis.Classification {
	case ClassNumberedMultipleChoice, ClassCursorBasedChoice:
		return true
	case ClassFreeTextRequest:
		return analysis.PromptActive
	}

	combined := strings.TrimSpace(strings.Join([]string{analysis.OutputBlock, analysis.PromptText}, "\n"))
	if combined == "" {
		return false
	}
	if approvalPromptPattern.MatchString(combined) && numberedMenuPattern.MatchString(combined) {
		return true
	}
	return false
}

func preferredApprovalChoice(combined string) string {
	if options := numberedMenuOptionPattern.FindAllStringSubmatch(combined, -1); len(options) > 0 {
		for _, option := range options {
			if len(option) < 3 {
				continue
			}
			label := strings.TrimSpace(option[2])
			if approvalPersistentAllowPattern.MatchString(label) {
				return strings.TrimSpace(option[1])
			}
		}
		for _, option := range options {
			if len(option) < 3 {
				continue
			}
			label := strings.TrimSpace(option[2])
			if approvalAllowPattern.MatchString(label) && !approvalRejectPattern.MatchString(label) {
				return strings.TrimSpace(option[1])
			}
		}
	}
	if matches := selectedNumberedMenuPattern.FindStringSubmatch(combined); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return "1"
}

func PromptZoneFingerprint(plainCapture string) string {
	lines := strings.Split(normalize(plainCapture), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := normalizeSpaces(strings.TrimSpace(line))
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, normalizePromptZoneLine(trimmed))
	}
	return strings.Join(normalized, "\n")
}

func normalizePromptZoneLine(line string) string {
	line = strings.ToLower(normalizeSpaces(strings.TrimSpace(line)))
	line = promptZoneSpinnerPattern.ReplaceAllString(line, "*")
	line = promptZoneDigitsPattern.ReplaceAllString(line, "#")
	line = promptZoneSpacePattern.ReplaceAllString(line, " ")
	return strings.TrimSpace(line)
}

func isProcessing(analysis Analysis, plainLines []string) bool {
	combined := strings.TrimSpace(strings.Join([]string{analysis.OutputBlock, analysis.PromptText}, "\n"))
	if selectionContextPattern.MatchString(combined) && numberedMenuPattern.MatchString(strings.Join([]string{analysis.OutputBlock, analysis.PromptText}, "\n")) {
		return false
	}
	if strongProcessingPattern.MatchString(strings.ToLower(combined)) {
		return true
	}

	windowStart := max(0, analysis.PromptLine-12)
	windowEnd := len(plainLines)
	if analysis.PromptLine >= 0 {
		windowEnd = min(len(plainLines), analysis.PromptLine+3)
	}
	for _, line := range plainLines[windowStart:windowEnd] {
		trimmed := strings.ToLower(strings.TrimSpace(line))
		if strongProcessingPattern.MatchString(trimmed) {
			return true
		}
		if weakProcessingPattern.MatchString(trimmed) && !looksLikeIdlePrompt(analysis.PromptText) {
			return true
		}
	}
	if !looksLikeIdlePrompt(analysis.PromptText) && weakProcessingPattern.MatchString(strings.ToLower(combined)) {
		return true
	}
	return false
}

func looksLikeIdlePrompt(promptText string) bool {
	promptText = normalizeSpaces(strings.TrimSpace(promptText))
	if promptText == "" {
		return false
	}
	if promptPlaceholderPattern.MatchString(promptText) || promptMarkerPattern.MatchString(promptText) {
		return true
	}
	lower := strings.ToLower(promptText)
	return strings.Contains(lower, "type your message") ||
		strings.HasPrefix(lower, "* ")
}

func isEditablePromptText(promptText string) bool {
	promptText = normalizeSpaces(strings.TrimSpace(promptText))
	if promptText == "" {
		return false
	}
	lower := strings.ToLower(promptText)
	return strings.HasPrefix(promptText, "* ") ||
		strings.Contains(lower, "type your message")
}

func ParseNumericChoice(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.TrimSuffix(strings.TrimSuffix(strings.Fields(raw)[0], "."), ")")
	if _, err := strconv.Atoi(raw); err != nil {
		return ""
	}
	return raw
}

func normalize(value string) string {
	return strings.TrimRight(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
}

func normalizeSpaces(value string) string {
	return strings.ReplaceAll(value, "\u00a0", " ")
}

func isNonContentLine(line string) bool {
	trimmed := normalizeSpaces(strings.TrimSpace(line))
	if trimmed == "" {
		return true
	}
	if separatorPattern.MatchString(trimmed) {
		return true
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "use /skills") {
		return true
	}
	if strings.Contains(lower, "? for shortcuts") {
		return true
	}
	if strings.Contains(lower, "context left") {
		return true
	}
	if strings.Contains(lower, "shift+tab to accept edits") {
		return true
	}
	if strings.Contains(lower, "gemini.md file") {
		return true
	}
	if strings.Contains(lower, "esc to interrupt") {
		return true
	}
	return false
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
