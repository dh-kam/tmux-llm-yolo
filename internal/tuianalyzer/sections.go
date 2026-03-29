package tuianalyzer

import (
	"strings"
)

// SectionType represents a semantic region within a terminal capture.
type SectionType int

const (
	SectionUnknown           SectionType = iota
	SectionHeader                        // Welcome/branding box, ASCII art logo
	SectionAssistantOutput               // The assistant's text/code/tool response
	SectionSeparator                     // Horizontal rules: ───, ▀▀▀/▄▄▄, box borders
	SectionSpinner                       // Processing indicators: ◐, ✻, Running…, Thinking
	SectionAssistantQuestion             // Approval prompts, numbered menus, confirmation requests
	SectionUserPrompt                    // The input line: ❯, ›, *, >
	SectionFooter                        // Status bars, hints, model info, context percentages
)

func (t SectionType) String() string {
	switch t {
	case SectionHeader:
		return "HEADER"
	case SectionAssistantOutput:
		return "ASST_OUTPUT"
	case SectionSeparator:
		return "SEPARATOR"
	case SectionSpinner:
		return "SPINNER"
	case SectionAssistantQuestion:
		return "ASST_QUESTION"
	case SectionUserPrompt:
		return "USER_PROMPT"
	case SectionFooter:
		return "FOOTER"
	default:
		return "UNKNOWN"
	}
}

// ShortLabel returns a compact label for display.
func (t SectionType) ShortLabel() string {
	switch t {
	case SectionHeader:
		return "HDR"
	case SectionAssistantOutput:
		return "OUT"
	case SectionSeparator:
		return "SEP"
	case SectionSpinner:
		return "SPN"
	case SectionAssistantQuestion:
		return "ASK"
	case SectionUserPrompt:
		return "PRM"
	case SectionFooter:
		return "FTR"
	default:
		return "UNK"
	}
}

// Section represents a contiguous range of lines classified as a single semantic region.
type Section struct {
	Type       SectionType
	StartLine  int      // 0-based, inclusive
	EndLine    int      // 0-based, inclusive
	ANSILines  []string // Raw ANSI lines in this section
	PlainLines []string // ANSI-stripped lines in this section
	Confidence float64  // 0.0 to 1.0
}

// PlainText returns the plain text content of the section.
func (s Section) PlainText() string {
	return strings.Join(s.PlainLines, "\n")
}

// ANSIText returns the ANSI text content of the section.
func (s Section) ANSIText() string {
	return strings.Join(s.ANSILines, "\n")
}

// LineCount returns the number of lines in the section.
func (s Section) LineCount() int {
	return s.EndLine - s.StartLine + 1
}

// FirstNonEmptyPlain returns the first non-empty plain line, or "".
func (s Section) FirstNonEmptyPlain() string {
	for _, line := range s.PlainLines {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}

// AnalysisResult is a backward-compatible alias for FrontEndLayout.
type AnalysisResult = FrontEndLayout

func sliceLines(lines []string, start int, end int) []string {
	if start < 0 {
		start = 0
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start >= end {
		return nil
	}
	out := make([]string, end-start)
	copy(out, lines[start:end])
	return out
}
