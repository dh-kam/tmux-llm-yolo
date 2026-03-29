package tuianalyzer

import (
	"regexp"
	"strings"
)

// Claude Code CLI frontend patterns (also used by GLM and other Claude-based services).
var (
	claudeCodeSignaturePattern  = regexp.MustCompile(`(?i)(claude code|fermenting|opus 4\.6|api usage billing|cogitated|brewed for|symbioting|whatchamacalliting|crunched|cultivating|cogitating|polishing|finagling|sautéed|cooked for|⏵⏵|shift\+tab to cycle|ctrl\+b ctrl\+b|ctrl\+e to explain|do you want to proceed)`)
	claudeCodeBoxTopPattern     = regexp.MustCompile(`^[[:space:]]*╭`)
	claudeCodeBoxBottomPattern  = regexp.MustCompile(`^[[:space:]]*╰`)
	claudeCodeWelcomePattern    = regexp.MustCompile(`(?i)(welcome back|claude code)`)
	claudeCodeSpinnerPattern    = regexp.MustCompile(`(?i)(✻|✽|✢|fermenting|brewed for|symbioting|whatchamacalliting|cogitated|polishing|finagling|sautéed|cooked for)`)
	claudeCodePromptPattern     = regexp.MustCompile(`^[[:space:]]*❯[[:space:]]?`)
	claudeCodeFooterHintPattern = regexp.MustCompile(`(?i)(esc to interrupt|esc to cancel|tab to amend|\?\s*for\s*shortcut|ctrl\+t to hide)`)
	claudeCodeToolResultPattern = regexp.MustCompile(`^[[:space:]]*[•●✓✦][[:space:]]`)
	claudeCodeApprovalPattern   = regexp.MustCompile(`(?i)(do you want to|allow.*\?|approve.*\?|bash command|read file|esc to cancel.*tab to amend)`)
)

// ClaudeCodeFrontEndAnalyzer detects and classifies Claude Code CLI terminal layouts.
type ClaudeCodeFrontEndAnalyzer struct{}

func (a *ClaudeCodeFrontEndAnalyzer) FrontEnd() string { return "claude-code" }

func (a *ClaudeCodeFrontEndAnalyzer) DetectSignatures(combined string) bool {
	return claudeCodeSignaturePattern.MatchString(combined)
}

func (a *ClaudeCodeFrontEndAnalyzer) ClassifyLine(idx int, plain, ansi, position string) lineHint {
	// Claude Code header box (╭─ Claude Code ─╮)
	if claudeCodeBoxTopPattern.MatchString(plain) || claudeCodeBoxBottomPattern.MatchString(plain) {
		return lineHint{Type: SectionHeader, Confidence: 0.9}
	}
	// Welcome content inside box
	if position == "top" && claudeCodeWelcomePattern.MatchString(plain) {
		return lineHint{Type: SectionHeader, Confidence: 0.9}
	}

	// Claude Code spinner (✻ Fermenting…, ✻ Brewed for…)
	if claudeCodeSpinnerPattern.MatchString(plain) {
		return lineHint{Type: SectionSpinner, Confidence: 0.95}
	}

	// Claude Code prompt (❯)
	if claudeCodePromptPattern.MatchString(plain) {
		// If it's just "❯" with nothing after it, it's an empty prompt
		trimmed := strings.TrimLeft(plain, " \t❯")
		if strings.TrimSpace(trimmed) == "" {
			return lineHint{Type: SectionUserPrompt, Confidence: 0.9}
		}
		return lineHint{Type: SectionUserPrompt, Confidence: 0.85}
	}

	// Claude Code footer hints
	if claudeCodeFooterHintPattern.MatchString(plain) {
		return lineHint{Type: SectionFooter, Confidence: 0.9}
	}

	// Claude Code tool results (• Read, • Edited, ✓, ✦)
	if claudeCodeToolResultPattern.MatchString(plain) {
		return lineHint{Type: SectionAssistantOutput, Confidence: 0.85}
	}

	// Claude Code approval prompts
	if claudeCodeApprovalPattern.MatchString(plain) {
		return lineHint{Type: SectionAssistantQuestion, Confidence: 0.9}
	}

	return lineHint{Type: SectionUnknown, Confidence: 0}
}
