package tuianalyzer

// FrontEndLayoutAnalyzer detects and classifies terminal sections for a specific CLI frontend.
type FrontEndLayoutAnalyzer interface {
	// FrontEnd returns the frontend identifier (e.g., "claude-code", "codex").
	FrontEnd() string

	// DetectSignatures returns true if the capture content matches this frontend.
	DetectSignatures(combinedText string) bool

	// ClassifyLine classifies a single line for this frontend.
	// Returns a lineHint with SectionType and Confidence.
	// If the line is not recognized by this frontend, returns SectionUnknown with confidence 0.
	ClassifyLine(idx int, plain, ansi string, position string) lineHint
}

// Registry holds all registered frontend analyzers, checked in order.
// More specific signatures (gemini, copilot, codex) are checked before
// broader signatures (claude-code) to avoid false matches.
var Registry = []FrontEndLayoutAnalyzer{
	&CopilotFrontEndAnalyzer{},
	&GeminiFrontEndAnalyzer{},
	&CodexFrontEndAnalyzer{},
	&ClaudeCodeFrontEndAnalyzer{},
}
