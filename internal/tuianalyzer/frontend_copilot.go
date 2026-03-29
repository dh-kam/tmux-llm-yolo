package tuianalyzer

import (
	"regexp"
	"strings"
)

// Copilot CLI frontend patterns.
var (
	copilotSignaturePattern   = regexp.MustCompile(`(?i)(github copilot|describe a task to get started|remaining reqs|copilot instructions|↑↓ to navigate|shift\+tab switch mode|ctrl\+q enqueue)`)
	copilotBoxTopPattern      = regexp.MustCompile(`^[[:space:]]*╭`)
	copilotBoxBottomPattern   = regexp.MustCompile(`^[[:space:]]*╰`)
	copilotPromptPattern      = regexp.MustCompile(`^[[:space:]]*❯[[:space:]]?`)
	copilotFooterPattern      = regexp.MustCompile(`(?i)(remaining reqs|shift\+tab|ctrl\+q|v\d+\.\d+\.\d+\s+available|run\s+/update)`)
	copilotProcessingPattern  = regexp.MustCompile(`(?i)(thinking|processing|◐|◉)`)
)

// CopilotFrontEndAnalyzer detects and classifies GitHub Copilot CLI terminal layouts.
type CopilotFrontEndAnalyzer struct{}

func (a *CopilotFrontEndAnalyzer) FrontEnd() string { return "copilot" }

func (a *CopilotFrontEndAnalyzer) DetectSignatures(combined string) bool {
	return copilotSignaturePattern.MatchString(combined)
}

func (a *CopilotFrontEndAnalyzer) ClassifyLine(idx int, plain, ansi, position string) lineHint {
	// Copilot header box
	if copilotBoxTopPattern.MatchString(plain) || copilotBoxBottomPattern.MatchString(plain) {
		return lineHint{Type: SectionHeader, Confidence: 0.9}
	}
	if position == "top" && strings.Contains(plain, "GitHub Copilot") {
		return lineHint{Type: SectionHeader, Confidence: 0.9}
	}

	// Copilot processing/thinking
	if copilotProcessingPattern.MatchString(plain) {
		return lineHint{Type: SectionSpinner, Confidence: 0.9}
	}

	// Copilot prompt (❯)
	if copilotPromptPattern.MatchString(plain) {
		return lineHint{Type: SectionUserPrompt, Confidence: 0.9}
	}

	// Copilot footer
	if copilotFooterPattern.MatchString(plain) {
		return lineHint{Type: SectionFooter, Confidence: 0.9}
	}

	return lineHint{Type: SectionUnknown, Confidence: 0}
}
