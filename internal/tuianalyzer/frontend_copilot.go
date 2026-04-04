package tuianalyzer

import (
	"regexp"
	"strings"
)

// Copilot CLI frontend patterns.
var (
	copilotSignaturePattern  = regexp.MustCompile(`(?i)(github copilot|describe a task to get started|remaining reqs|copilot instructions|↑↓ to navigate|shift\+tab switch mode|ctrl\+q enqueue)`)
	copilotBoxTopPattern     = regexp.MustCompile(`^[[:space:]]*╭`)
	copilotBoxBottomPattern  = regexp.MustCompile(`^[[:space:]]*╰`)
	copilotPromptPattern     = regexp.MustCompile(`^[[:space:]]*❯[[:space:]]?`)
	copilotFooterPattern     = regexp.MustCompile(`(?i)(remaining reqs|shift\+tab|ctrl\+q|v\d+\.\d+\.\d+\s+available|run\s+/update)`)
	copilotProcessingPattern = regexp.MustCompile(`(?i)(thinking|processing|◐|◉)`)
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

// copilotFooterKeyPattern matches "key action" pairs in Copilot footer lines,
// e.g. "ctrl+s run command", "shift+tab switch mode".
var copilotFooterKeyPattern = regexp.MustCompile(`(?i)((?:ctrl|shift|alt)\+[a-z0-9]+)\s+([\w][\w ]*[\w])`)

// ParseFooterKeyHints extracts key-action pairs from Copilot footer lines.
// Input lines are plain-text footer lines (e.g. from FOOTER sections).
// Returns a map like {"ctrl+s": "run command", "shift+tab": "switch mode"}.
func ParseFooterKeyHints(footerLines []string) map[string]string {
	hints := make(map[string]string)
	for _, line := range footerLines {
		// Split on common separators (· or │ or multiple spaces)
		parts := strings.FieldsFunc(line, func(r rune) bool {
			return r == '·' || r == '│'
		})
		for _, part := range parts {
			part = strings.TrimSpace(part)
			matches := copilotFooterKeyPattern.FindAllStringSubmatch(part, -1)
			for _, m := range matches {
				key := strings.ToLower(strings.TrimSpace(m[1]))
				action := strings.TrimSpace(m[2])
				// Skip version info fragments like "v1.0.17 available"
				if strings.HasPrefix(key, "v") {
					continue
				}
				hints[key] = action
			}
		}
	}
	return hints
}
