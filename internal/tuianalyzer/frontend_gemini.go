package tuianalyzer

import (
	"regexp"
)

// Gemini CLI frontend patterns.
var (
	geminiSignaturePattern    = regexp.MustCompile(`(?i)(gemini code assist|type your message or @path|gemini 3)`)
	geminiASCIIArtPattern     = regexp.MustCompile(`[█░▀▄]`)
	geminiBorderPattern       = regexp.MustCompile(`^[▀▄]+$`)
	geminiPromptPattern       = regexp.MustCompile(`^[[:space:]]*\*[[:space:]]`)
	geminiPlaceholderPattern  = regexp.MustCompile(`(?i)(type your message|type @ to mention)`)
	geminiFooterPattern       = regexp.MustCompile(`(?i)(/workspace.*sandbox|/model\s+auto|/auth|/help|yojo\s+ctrl\+y|\?\s*for\s*shortcuts)`)
	geminiSeparatorPattern    = regexp.MustCompile(`^[▀▄─]+$`)
)

// GeminiFrontEndAnalyzer detects and classifies Gemini CLI terminal layouts.
type GeminiFrontEndAnalyzer struct{}

func (a *GeminiFrontEndAnalyzer) FrontEnd() string { return "gemini" }

func (a *GeminiFrontEndAnalyzer) DetectSignatures(combined string) bool {
	return geminiSignaturePattern.MatchString(combined)
}

func (a *GeminiFrontEndAnalyzer) ClassifyLine(idx int, plain, ansi, position string) lineHint {
	// Gemini ASCII art header (█░ characters)
	if position == "top" && geminiASCIIArtPattern.MatchString(plain) {
		return lineHint{Type: SectionHeader, Confidence: 0.9}
	}

	// Gemini border decorations (▀▀▀, ▄▄▄)
	if geminiBorderPattern.MatchString(plain) {
		return lineHint{Type: SectionSeparator, Confidence: 0.9}
	}

	// Gemini separator lines
	if geminiSeparatorPattern.MatchString(plain) {
		return lineHint{Type: SectionSeparator, Confidence: 0.9}
	}

	// Gemini prompt (* Type your message)
	if geminiPromptPattern.MatchString(plain) {
		return lineHint{Type: SectionUserPrompt, Confidence: 0.9}
	}
	if geminiPlaceholderPattern.MatchString(plain) {
		return lineHint{Type: SectionUserPrompt, Confidence: 0.9}
	}

	// Gemini footer (/workspace ... sandbox ... /model)
	if geminiFooterPattern.MatchString(plain) {
		return lineHint{Type: SectionFooter, Confidence: 0.9}
	}

	return lineHint{Type: SectionUnknown, Confidence: 0}
}
