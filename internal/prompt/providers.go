package prompt

import (
	"strings"

	"github.com/dh-kam/tmux-llm-yolo/internal/capture"
)

type providerHeuristics struct {
	name string
}

func detectProviderFromCaptures(ansiCapture string, plainCapture string) string {
	combined := normalizeSpaces(strings.Join([]string{capture.StripANSI(ansiCapture), plainCapture}, "\n"))
	switch {
	case codexSignaturePattern.MatchString(combined):
		return "codex"
	case copilotSignaturePattern.MatchString(combined):
		return "copilot"
	case geminiSignaturePattern.MatchString(combined):
		return "gemini"
	case glmSignaturePattern.MatchString(combined):
		return "glm"
	default:
		return ""
	}
}

func heuristicsFor(provider string) providerHeuristics {
	return providerHeuristics{name: strings.TrimSpace(provider)}
}

func (h providerHeuristics) hasAssistantUI(ansiCapture string, plainCapture string) bool {
	combined := normalizeSpaces(strings.Join([]string{capture.StripANSI(ansiCapture), plainCapture}, "\n"))
	switch h.name {
	case "codex":
		return codexSignaturePattern.MatchString(combined)
	case "copilot":
		return copilotSignaturePattern.MatchString(combined)
	case "gemini":
		return geminiSignaturePattern.MatchString(combined)
	case "glm":
		return glmSignaturePattern.MatchString(combined)
	default:
		return false
	}
}

func (h providerHeuristics) detectPromptLine(ansiLines []string, plainLines []string) int {
	switch h.name {
	case "codex":
		if idx := detectCodexPromptLine(ansiLines, plainLines); idx >= 0 {
			return idx
		}
	case "copilot":
		if idx := detectCopilotPromptLine(ansiLines, plainLines); idx >= 0 {
			return idx
		}
	case "gemini":
		if idx := detectGeminiPromptLine(ansiLines, plainLines); idx >= 0 {
			return idx
		}
	case "glm":
		if idx := detectGLMPromptLine(ansiLines, plainLines); idx >= 0 {
			return idx
		}
	}
	return detectGenericPromptLine(ansiLines, plainLines)
}

func (h providerHeuristics) isActivePromptLine(promptLine int, ansiLines []string, plainLines []string) bool {
	if promptLine < 0 || promptLine >= len(plainLines) || promptLine >= len(ansiLines) {
		return false
	}
	switch h.name {
	case "codex":
		return isCodexActivePromptLine(promptLine, ansiLines, plainLines)
	case "copilot":
		return isCopilotActivePromptLine(promptLine, ansiLines, plainLines)
	case "gemini":
		return isGeminiActivePromptLine(promptLine, ansiLines, plainLines)
	case "glm":
		return isGLMActivePromptLine(promptLine, ansiLines, plainLines)
	default:
		return isGenericActivePromptLine(promptLine, ansiLines, plainLines)
	}
}

func (h providerHeuristics) isPlaceholderPromptLine(promptLine int, ansiLines []string, plainLines []string) bool {
	if promptLine < 0 || promptLine >= len(plainLines) || promptLine >= len(ansiLines) {
		return false
	}
	switch h.name {
	case "codex":
		return isCodexPlaceholderPromptLine(promptLine, ansiLines, plainLines)
	case "copilot":
		return isCopilotPlaceholderPromptLine(promptLine, ansiLines, plainLines)
	case "gemini":
		return isGeminiPlaceholderPromptLine(promptLine, ansiLines, plainLines)
	case "glm":
		return isGLMPlaceholderPromptLine(promptLine, ansiLines, plainLines)
	default:
		return isGenericPlaceholderPromptLine(promptLine, ansiLines, plainLines)
	}
}

func detectGenericPromptLine(ansiLines []string, plainLines []string) int {
	if idx := detectApprovalPromptLine(plainLines); idx >= 0 {
		return idx
	}
	for i := len(plainLines) - 1; i >= 0; i-- {
		plain := normalizeSpaces(strings.TrimSpace(capture.StripANSI(plainLines[i])))
		if plain == "" {
			continue
		}
		if promptPlaceholderPattern.MatchString(plain) || promptMarkerPattern.MatchString(plain) {
			return i
		}
	}

	limit := len(ansiLines)
	if len(plainLines) < limit {
		limit = len(plainLines)
	}
	for i := limit - 1; i >= 0; i-- {
		plain := normalizeSpaces(strings.TrimSpace(capture.StripANSI(plainLines[i])))
		ansi := ansiLines[i]
		if plain == "" {
			continue
		}
		if strings.Contains(ansi, "\x1b[") && strings.HasPrefix(plain, ">") {
			return i
		}
		if strings.Contains(ansi, "\x1b[7m") && (strings.HasPrefix(plain, ">") || strings.HasPrefix(plain, "❯") || promptPlaceholderPattern.MatchString(plain)) {
			return i
		}
		if strings.Contains(ansi, "\x1b[") && freeTextPattern.MatchString(plain) && len(strings.Fields(plain)) <= 8 {
			return i
		}
	}
	return -1
}

func isGenericActivePromptLine(promptLine int, ansiLines []string, plainLines []string) bool {
	plain := normalizeSpaces(strings.TrimSpace(capture.StripANSI(plainLines[promptLine])))
	ansi := ansiLines[promptLine]
	if plain == "" {
		return false
	}
	return strings.Contains(ansi, "\x1b[") && (promptPlaceholderPattern.MatchString(plain) || promptMarkerPattern.MatchString(plain))
}

func isGenericPlaceholderPromptLine(promptLine int, ansiLines []string, plainLines []string) bool {
	plain := normalizeSpaces(strings.TrimSpace(capture.StripANSI(plainLines[promptLine])))
	ansi := ansiLines[promptLine]
	return promptPlaceholderPattern.MatchString(plain) && strings.Contains(ansi, "\x1b[2m")
}

func detectCodexPromptLine(ansiLines []string, plainLines []string) int {
	if idx := detectApprovalPromptLine(plainLines); idx >= 0 {
		return idx
	}
	for i := len(plainLines) - 1; i >= 0; i-- {
		plain := normalizeSpaces(strings.TrimSpace(capture.StripANSI(plainLines[i])))
		if strings.HasPrefix(plain, "›") {
			return i
		}
		if strings.Contains(plain, "Use /skills") {
			return i
		}
	}
	return -1
}

func isCodexActivePromptLine(promptLine int, ansiLines []string, plainLines []string) bool {
	plain := normalizeSpaces(strings.TrimSpace(capture.StripANSI(plainLines[promptLine])))
	if !strings.HasPrefix(plain, "›") {
		return false
	}
	for i := max(0, promptLine-2); i <= min(len(ansiLines)-1, promptLine+2); i++ {
		if strings.Contains(ansiLines[i], "\x1b[") {
			return promptLine >= len(plainLines)-12 || hasOnlyPromptTail(promptLine, plainLines)
		}
	}
	return false
}

func isCodexPlaceholderPromptLine(promptLine int, ansiLines []string, plainLines []string) bool {
	plain := normalizeSpaces(strings.TrimSpace(capture.StripANSI(plainLines[promptLine])))
	if !strings.HasPrefix(plain, "›") {
		return false
	}
	ansi := ansiLines[promptLine]
	return strings.Contains(ansi, "\x1b[2m")
}

func detectGeminiPromptLine(ansiLines []string, plainLines []string) int {
	if idx := detectApprovalPromptLine(plainLines); idx >= 0 {
		return idx
	}
	for i := len(plainLines) - 1; i >= 0; i-- {
		plain := normalizeSpaces(strings.TrimSpace(capture.StripANSI(plainLines[i])))
		if strings.HasPrefix(plain, "* ") {
			return i
		}
		if strings.Contains(plain, "Type your message or @path/to/file") {
			return i
		}
		if strings.HasPrefix(plain, "> ") || strings.HasPrefix(plain, "*   Type your message") {
			return i
		}
	}
	return -1
}

func isGeminiActivePromptLine(promptLine int, ansiLines []string, plainLines []string) bool {
	plain := normalizeSpaces(strings.TrimSpace(capture.StripANSI(plainLines[promptLine])))
	ansi := ansiLines[promptLine]
	if !(strings.HasPrefix(plain, "* ") || strings.Contains(plain, "Type your message or @path/to/file")) {
		return false
	}
	if !strings.Contains(ansi, "\x1b[") {
		return false
	}
	if strings.Contains(ansi, "\x1b[7m") || strings.Contains(ansi, "\x1b[48;") {
		return true
	}
	return promptLine >= len(plainLines)-8
}

func isGeminiPlaceholderPromptLine(promptLine int, ansiLines []string, plainLines []string) bool {
	plain := normalizeSpaces(strings.TrimSpace(capture.StripANSI(plainLines[promptLine])))
	ansi := ansiLines[promptLine]
	return strings.Contains(plain, "Type your message") && strings.Contains(ansi, "\x1b[2m")
}

func detectCopilotPromptLine(ansiLines []string, plainLines []string) int {
	if idx := detectApprovalPromptLine(plainLines); idx >= 0 {
		return idx
	}
	for i := len(plainLines) - 1; i >= 0; i-- {
		plain := normalizeSpaces(strings.TrimSpace(capture.StripANSI(plainLines[i])))
		if strings.HasPrefix(plain, "❯  Type @ to mention files") {
			return i
		}
		if strings.Contains(plain, "Type @ to mention files, / for commands, or ? for shortcuts") {
			return i
		}
	}
	return -1
}

func isCopilotActivePromptLine(promptLine int, ansiLines []string, plainLines []string) bool {
	plain := normalizeSpaces(strings.TrimSpace(capture.StripANSI(plainLines[promptLine])))
	ansi := ansiLines[promptLine]
	if !strings.HasPrefix(plain, "❯") {
		return false
	}
	if !strings.Contains(ansi, "\x1b[") {
		return false
	}
	return promptLine >= len(plainLines)-8
}

func isCopilotPlaceholderPromptLine(promptLine int, ansiLines []string, plainLines []string) bool {
	plain := normalizeSpaces(strings.TrimSpace(capture.StripANSI(plainLines[promptLine])))
	ansi := ansiLines[promptLine]
	if !strings.HasPrefix(plain, "❯") {
		return false
	}
	return strings.Contains(plain, "Type @ to mention files") || strings.Contains(ansi, "\x1b[2m")
}

func detectGLMPromptLine(ansiLines []string, plainLines []string) int {
	if idx := detectApprovalPromptLine(plainLines); idx >= 0 {
		return idx
	}
	for i := len(plainLines) - 1; i >= 0; i-- {
		plain := normalizeSpaces(strings.TrimSpace(capture.StripANSI(plainLines[i])))
		if plain == "❯" || strings.HasPrefix(plain, "❯ ") {
			return i
		}
		if strings.Contains(plain, "esc to interrupt") {
			for j := i - 1; j >= 0 && j >= i-4; j-- {
				candidate := normalizeSpaces(strings.TrimSpace(capture.StripANSI(plainLines[j])))
				if candidate == "❯" || strings.HasPrefix(candidate, "❯ ") {
					return j
				}
			}
		}
	}
	return -1
}

func isGLMActivePromptLine(promptLine int, ansiLines []string, plainLines []string) bool {
	plain := normalizeSpaces(strings.TrimSpace(capture.StripANSI(plainLines[promptLine])))
	ansi := ansiLines[promptLine]
	if plain != "❯" && !strings.HasPrefix(plain, "❯ ") {
		return false
	}
	if strings.Contains(ansi, "\x1b[7m") || strings.Contains(ansi, "\x1b[48;") {
		return true
	}
	return promptLine >= len(plainLines)-8
}

func isGLMPlaceholderPromptLine(promptLine int, ansiLines []string, plainLines []string) bool {
	plain := normalizeSpaces(strings.TrimSpace(capture.StripANSI(plainLines[promptLine])))
	ansi := ansiLines[promptLine]
	if plain != "❯" && !strings.HasPrefix(plain, "❯ ") {
		return false
	}
	if strings.Contains(ansi, "\x1b[38;5;246m") || strings.Contains(ansi, "\x1b[2m") {
		return true
	}
	return !strings.Contains(ansi, "\x1b[38;5;231m") && strings.Contains(ansi, "\x1b[7m")
}

func hasOnlyPromptTail(promptLine int, plainLines []string) bool {
	if promptLine < 0 || promptLine >= len(plainLines) {
		return false
	}
	for i := promptLine + 1; i < len(plainLines); i++ {
		trimmed := normalizeSpaces(strings.TrimSpace(capture.StripANSI(plainLines[i])))
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "context left") ||
			strings.Contains(lower, "gpt-") ||
			strings.Contains(lower, "/workspace/") ||
			strings.Contains(lower, "tab to queue") {
			continue
		}
		return false
	}
	return true
}

func detectApprovalPromptLine(plainLines []string) int {
	for i := len(plainLines) - 1; i >= 0; i-- {
		plain := normalizeSpaces(strings.TrimSpace(capture.StripANSI(plainLines[i])))
		if plain == "" || !selectedNumberedMenuPattern.MatchString(plain) {
			continue
		}
		windowStart := max(0, i-6)
		window := strings.Join(plainLines[windowStart:min(len(plainLines), i+4)], "\n")
		if approvalPromptPattern.MatchString(window) && numberedMenuPattern.MatchString(window) {
			return i
		}
	}
	return -1
}
