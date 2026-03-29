package tuianalyzer

import (
	"regexp"
)

// Grok CLI frontend patterns.
var (
	grokSignaturePattern = regexp.MustCompile(`(?i)(grok cli|grok-code|grok-4|grok-\d|auto-edit:.*shift\+tab|GROK\.md|≋\s*grok)`)
	grokBoxTopPattern    = regexp.MustCompile(`^[[:space:]]*╭`)
	grokBoxBottomPattern = regexp.MustCompile(`^[[:space:]]*╰`)
	grokPromptPattern    = regexp.MustCompile(`^[[:space:]]*❯[[:space:]]?`)
	grokFooterPattern    = regexp.MustCompile(`(?i)(auto-edit:.*shift\+tab|≋\s*grok)`)
	grokStatusPattern    = regexp.MustCompile(`⏺`)
	grokInputPattern     = regexp.MustCompile(`^[[:space:]]*>[[:space:]]`)
)

// GrokFrontEndAnalyzer detects and classifies Grok CLI terminal layouts.
type GrokFrontEndAnalyzer struct{}

func (a *GrokFrontEndAnalyzer) FrontEnd() string { return "grok" }

func (a *GrokFrontEndAnalyzer) DetectSignatures(combined string) bool {
	return grokSignaturePattern.MatchString(combined)
}

func (a *GrokFrontEndAnalyzer) ClassifyLine(idx int, plain, ansi, position string) lineHint {
	// Grok header box
	if grokBoxTopPattern.MatchString(plain) || grokBoxBottomPattern.MatchString(plain) {
		return lineHint{Type: SectionHeader, Confidence: 0.9}
	}

	// Grok footer (auto-edit mode, model name)
	if grokFooterPattern.MatchString(plain) {
		return lineHint{Type: SectionFooter, Confidence: 0.9}
	}

	// Grok status indicator (⏺)
	if grokStatusPattern.MatchString(plain) {
		return lineHint{Type: SectionAssistantOutput, Confidence: 0.85}
	}

	// Grok command input (>)
	if grokInputPattern.MatchString(plain) {
		return lineHint{Type: SectionUserPrompt, Confidence: 0.85}
	}

	// Grok prompt (❯)
	if grokPromptPattern.MatchString(plain) {
		return lineHint{Type: SectionUserPrompt, Confidence: 0.9}
	}

	return lineHint{Type: SectionUnknown, Confidence: 0}
}
