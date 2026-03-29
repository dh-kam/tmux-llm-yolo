package tuianalyzer

import (
	"regexp"
)

// Codex CLI frontend patterns.
var (
	codexSignaturePattern   = regexp.MustCompile(`(?i)(openai codex|gpt-5\.\d|use /skills|context left|press enter to confirm or esc to cancel|tell codex what to do differently|don.t ask again for these files)`)
	codexBoxTopPattern      = regexp.MustCompile(`^[[:space:]]*╭`)
	codexBoxBottomPattern   = regexp.MustCompile(`^[[:space:]]*╰`)
	codexWorkSummaryPattern = regexp.MustCompile(`─ Worked for \d+m \d+s ─`)
	codexFooterPattern      = regexp.MustCompile(`(?i)(gpt-5\.\d+.*·\s*\d+%\s*left|use /skills|\?\s*for\s*shortcuts|\d+%\s*context\s*left)`)
	codexPromptPattern      = regexp.MustCompile(`^[[:space:]]*›[[:space:]]`)
	codexSpinnerPattern     = regexp.MustCompile(`(?i)working\s*\(\d|running…`)
	codexToolResultPattern  = regexp.MustCompile(`^[[:space:]]*[•●][[:space:]]`)
)

// CodexFrontEndAnalyzer detects and classifies OpenAI Codex CLI terminal layouts.
type CodexFrontEndAnalyzer struct{}

func (a *CodexFrontEndAnalyzer) FrontEnd() string { return "codex" }

func (a *CodexFrontEndAnalyzer) DetectSignatures(combined string) bool {
	return codexSignaturePattern.MatchString(combined)
}

func (a *CodexFrontEndAnalyzer) ClassifyLine(idx int, plain, ansi, position string) lineHint {
	// Codex header box (╭──OpenAI Codex──╮)
	if codexBoxTopPattern.MatchString(plain) || codexBoxBottomPattern.MatchString(plain) {
		return lineHint{Type: SectionHeader, Confidence: 0.9}
	}
	if position == "top" && codexBoxBottomPattern.MatchString(plain) {
		return lineHint{Type: SectionHeader, Confidence: 0.9}
	}

	// Work summary separator (─ Worked for Nm Ns ─)
	if codexWorkSummaryPattern.MatchString(plain) {
		return lineHint{Type: SectionSeparator, Confidence: 0.95}
	}

	// Codex footer (model info, context %, shortcuts)
	if codexFooterPattern.MatchString(plain) {
		return lineHint{Type: SectionFooter, Confidence: 0.9}
	}

	// Codex prompt (›)
	if codexPromptPattern.MatchString(plain) {
		return lineHint{Type: SectionUserPrompt, Confidence: 0.9}
	}

	// Codex spinner (• Working, • Running)
	if codexSpinnerPattern.MatchString(plain) {
		return lineHint{Type: SectionSpinner, Confidence: 0.9}
	}

	// Codex tool results (• Ran, • Edited, • Explored)
	if codexToolResultPattern.MatchString(plain) {
		return lineHint{Type: SectionAssistantOutput, Confidence: 0.85}
	}

	return lineHint{Type: SectionUnknown, Confidence: 0}
}
