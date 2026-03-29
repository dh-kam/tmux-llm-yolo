package tuianalyzer

import (
	"regexp"
	"strings"

	"github.com/dh-kam/tmux-llm-yolo/internal/capture"
)

// Analyze parses a terminal capture into semantic sections.
func Analyze(ansiCapture string, plainCapture string) AnalysisResult {
	ansiLines := splitLines(ansiCapture)
	plainLines := splitLines(plainCapture)

	frontEnd := detectFrontEnd(ansiCapture, plainCapture)

	classifier := newLineClassifier(frontEnd, ansiLines, plainLines)
	hints := classifier.classifyAll()

	sections := mergeIntoSections(hints, ansiLines, plainLines)
	sections = refineSections(sections, frontEnd, plainLines, ansiLines)

	return AnalysisResult{
		FrontEnd:   frontEnd,
		Sections:   sections,
		PlainLines: plainLines,
		ANSILines:  ansiLines,
	}
}

// AnalyzeWithHint is like Analyze but accepts a frontend hint to override auto-detection.
func AnalyzeWithHint(frontendHint string, ansiCapture string, plainCapture string) AnalysisResult {
	ansiLines := splitLines(ansiCapture)
	plainLines := splitLines(plainCapture)

	frontEnd := normalizeFrontEnd(frontendHint)
	if frontEnd == "" {
		frontEnd = detectFrontEnd(ansiCapture, plainCapture)
	}

	classifier := newLineClassifier(frontEnd, ansiLines, plainLines)
	hints := classifier.classifyAll()

	sections := mergeIntoSections(hints, ansiLines, plainLines)
	sections = refineSections(sections, frontEnd, plainLines, ansiLines)

	return AnalysisResult{
		FrontEnd:   frontEnd,
		Sections:   sections,
		PlainLines: plainLines,
		ANSILines:  ansiLines,
	}
}

type lineHint struct {
	Type       SectionType
	Confidence float64
}

type lineClassifier struct {
	provider   string
	ansiLines  []string
	plainLines []string
}

func newLineClassifier(provider string, ansiLines []string, plainLines []string) *lineClassifier {
	return &lineClassifier{
		provider:   provider,
		ansiLines:  ansiLines,
		plainLines: plainLines,
	}
}

func (c *lineClassifier) classifyAll() []lineHint {
	n := len(c.plainLines)
	hints := make([]lineHint, n)
	for i := 0; i < n; i++ {
		hints[i] = c.classifyLine(i)
	}
	return hints
}

func (c *lineClassifier) classifyLine(idx int) lineHint {
	plain := c.plainLine(idx)
	ansi := c.ansiLine(idx)
	position := linePosition(idx, len(c.plainLines))

	// Check provider-specific patterns first, they have higher confidence.
	if hint := c.classifyProviderSpecific(idx, plain, ansi, position); hint.Type != SectionUnknown {
		return hint
	}

	// Generic patterns
	if hint := c.classifyGeneric(plain, position); hint.Type != SectionUnknown {
		return hint
	}

	return lineHint{Type: SectionUnknown, Confidence: 0.1}
}

func (c *lineClassifier) plainLine(idx int) string {
	if idx >= 0 && idx < len(c.plainLines) {
		return strings.TrimRight(c.plainLines[idx], " \t\r")
	}
	return ""
}

func (c *lineClassifier) ansiLine(idx int) string {
	if idx >= 0 && idx < len(c.ansiLines) {
		return c.ansiLines[idx]
	}
	return ""
}

func linePosition(idx int, total int) string {
	if total <= 1 {
		return "middle"
	}
	ratio := float64(idx) / float64(total-1)
	switch {
	case ratio < 0.15:
		return "top"
	case ratio > 0.85:
		return "bottom"
	default:
		return "middle"
	}
}

func (c *lineClassifier) classifyGeneric(plain string, position string) lineHint {
	// Spinner patterns (generic across all providers)
	if spinnerPattern.MatchString(plain) {
		return lineHint{Type: SectionSpinner, Confidence: 0.85}
	}

	// Separator lines
	if separatorOnlyPattern.MatchString(plain) {
		return lineHint{Type: SectionSeparator, Confidence: 0.9}
	}

	// Footer patterns
	if footerPattern.MatchString(plain) {
		return lineHint{Type: SectionFooter, Confidence: 0.85}
	}

	// Prompt markers
	if promptMarkerGenericPattern.MatchString(plain) {
		conf := 0.8
		if position == "bottom" {
			conf = 0.9
		}
		return lineHint{Type: SectionUserPrompt, Confidence: conf}
	}

	// Approval/question patterns
	if approvalPattern.MatchString(plain) {
		return lineHint{Type: SectionAssistantQuestion, Confidence: 0.8}
	}

	// Content lines (default to assistant output if not empty)
	trimmed := strings.TrimSpace(plain)
	if trimmed != "" {
		return lineHint{Type: SectionAssistantOutput, Confidence: 0.5}
	}

	return lineHint{Type: SectionUnknown, Confidence: 0.1}
}

func (c *lineClassifier) classifyProviderSpecific(idx int, plain string, ansi string, position string) lineHint {
	for _, fe := range Registry {
		if fe.FrontEnd() == c.provider {
			return fe.ClassifyLine(idx, plain, ansi, position)
		}
	}
	return lineHint{}
}

func mergeIntoSections(hints []lineHint, ansiLines []string, plainLines []string) []Section {
	if len(hints) == 0 {
		return nil
	}

	var sections []Section
	current := Section{
		Type:       hints[0].Type,
		StartLine:  0,
		EndLine:    0,
		Confidence: hints[0].Confidence,
	}

	for i := 1; i < len(hints); i++ {
		if hints[i].Type == current.Type {
			current.EndLine = i
			current.Confidence = max(current.Confidence, hints[i].Confidence)
		} else {
			current.ANSILines = sliceLines(ansiLines, current.StartLine, current.EndLine+1)
			current.PlainLines = sliceLines(plainLines, current.StartLine, current.EndLine+1)
			sections = append(sections, current)
			current = Section{
				Type:       hints[i].Type,
				StartLine:  i,
				EndLine:    i,
				Confidence: hints[i].Confidence,
			}
		}
	}

	// Finalize last section
	current.ANSILines = sliceLines(ansiLines, current.StartLine, current.EndLine+1)
	current.PlainLines = sliceLines(plainLines, current.StartLine, current.EndLine+1)
	sections = append(sections, current)

	return sections
}

func refineSections(sections []Section, provider string, plainLines []string, ansiLines []string) []Section {
	if len(sections) <= 1 {
		return sections
	}
	total := len(plainLines)

	// Phase 1: merge box ranges into single HEADER sections
	sections = mergeBoxRanges(sections, plainLines)

	// Phase 2: absorb small unknown sections into neighbors
	refined := make([]Section, 0, len(sections))
	for i := 0; i < len(sections); i++ {
		s := sections[i]
		if s.Type == SectionUnknown && s.LineCount() <= 2 {
			// Try to absorb into previous section
			if len(refined) > 0 {
				prev := &refined[len(refined)-1]
				prev.EndLine = s.EndLine
				prev.ANSILines = append(prev.ANSILines, s.ANSILines...)
				prev.PlainLines = append(prev.PlainLines, s.PlainLines...)
				continue
			}
		}
		// Refine: boost header confidence for top sections
		if s.Type == SectionHeader && s.StartLine < total/4 {
			s.Confidence = min(1.0, s.Confidence+0.1)
		}
		// Refine: boost footer confidence for bottom sections
		if s.Type == SectionFooter && s.EndLine >= total*3/4 {
			s.Confidence = min(1.0, s.Confidence+0.1)
		}
		refined = append(refined, s)
	}
	return refined
}

// mergeBoxRanges detects ╭...╰ box-drawing ranges and merges all sections
// within each range into a single section. Boxes in the top third become HEADER,
// boxes elsewhere become ASST_OUTPUT (e.g. code tables in assistant responses).
func mergeBoxRanges(sections []Section, plainLines []string) []Section {
	boxRanges := findAllBoxRanges(plainLines)
	if len(boxRanges) == 0 {
		return sections
	}

	total := len(plainLines)
	headerThreshold := total / 3

	var result []Section
	i := 0
	for i < len(sections) {
		s := sections[i]

		// Check if this section overlaps with any box range
		if overlapsBoxRange(s, boxRanges) {
			// Determine type based on position: top → HEADER, else → ASST_OUTPUT
			sectionType := SectionAssistantOutput
			confidence := 0.85
			if s.StartLine < headerThreshold {
				sectionType = SectionHeader
				confidence = 0.9
			}

			// Expand forward to absorb all sections within the same box range
			merged := Section{
				Type:       sectionType,
				StartLine:  s.StartLine,
				EndLine:    s.EndLine,
				Confidence: confidence,
				ANSILines:  make([]string, len(s.ANSILines)),
				PlainLines: make([]string, len(s.PlainLines)),
			}
			copy(merged.ANSILines, s.ANSILines)
			copy(merged.PlainLines, s.PlainLines)

			// Absorb subsequent sections that are also in the same box range
			for i+1 < len(sections) {
				next := sections[i+1]
				if overlapsBoxRange(next, boxRanges) {
					merged.EndLine = next.EndLine
					merged.ANSILines = append(merged.ANSILines, next.ANSILines...)
					merged.PlainLines = append(merged.PlainLines, next.PlainLines...)
					i++
				} else {
					break
				}
			}

			// Check if the section just after the box is also a box bottom (╰)
			// that belongs to this range - absorb it too
			result = append(result, merged)
		} else {
			result = append(result, s)
		}
		i++
	}
	return result
}

// overlapsBoxRange checks if a section's lines overlap with any detected box range.
func overlapsBoxRange(s Section, ranges [][2]int) bool {
	for _, r := range ranges {
		if s.StartLine <= r[1] && s.EndLine >= r[0] {
			return true
		}
	}
	return false
}

func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	normalized := strings.TrimRight(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if normalized == "" {
		return nil
	}
	return strings.Split(normalized, "\n")
}

// Shared detection patterns

var (
	// Spinner patterns
	spinnerPattern = regexp.MustCompile(`(?i)(fermenting|brewed for|cogitated|◐|◉|✢|✽|running…|thinking|working\s*\(\d|worked for \d+m \d+s|orchestrating|polishing|just a moment)`)
)

var (
	// Separator patterns
	separatorOnlyPattern = regexp.MustCompile(`^[[:space:]─═▀▄╭╰│├┤┘]*$`)
)

var (
	// Footer patterns
	footerPattern = regexp.MustCompile(`(?i)(\?\s*for\s*shortcuts|context\s*left|remaining\s*reqs|esc\s*to\s*interrupt|esc\s*to\s*cancel|tab\s*to\s*amend|shift\+tab|ctrl\+q|ctrl\+y|use\s*/skills|/model\s+to\s+change|/workspace.*sandbox.*model)`)
)

var (
	// Prompt marker patterns
	promptMarkerGenericPattern = regexp.MustCompile(`^[[:space:]]*[❯›*>][[:space:]]`)
)

var (
	// Approval patterns
	approvalPattern = regexp.MustCompile(`(?i)(do you want to|proceed\?|approve|yes.*don'?t ask|always allow|1\.\s*yes|2\.\s*no)`)
)

func detectFrontEnd(ansiCapture string, plainCapture string) string {
	combined := strings.Join([]string{capture.StripANSI(ansiCapture), plainCapture}, "\n")
	for _, fe := range Registry {
		if fe.DetectSignatures(combined) {
			return fe.FrontEnd()
		}
	}
	return ""
}

func normalizeFrontEnd(hint string) string {
	switch strings.ToLower(strings.TrimSpace(hint)) {
	case "claude-code", "glm", "claude":
		return "claude-code"
	case "codex":
		return "codex"
	case "gemini":
		return "gemini"
	case "copilot":
		return "copilot"
	default:
		return ""
	}
}

// findAllBoxRanges finds all box-drawing ranges in the capture.
func findAllBoxRanges(lines []string) [][2]int {
	var ranges [][2]int
	start := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if start < 0 {
			if strings.HasPrefix(trimmed, "╭") || strings.HasPrefix(trimmed, "┌") {
				start = i
			}
		} else {
			if strings.HasPrefix(trimmed, "╰") || strings.HasPrefix(trimmed, "└") {
				ranges = append(ranges, [2]int{start, i})
				start = -1
			}
		}
	}
	return ranges
}

// detectBoxRange finds the extent of a box-drawing character block.
// Returns (startLine, endLine) or (-1, -1) if no box found.
func detectBoxRange(lines []string) (int, int) {
	start := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if start < 0 {
			if strings.HasPrefix(trimmed, "╭") || strings.HasPrefix(trimmed, "┌") {
				start = i
			}
		} else {
			if strings.HasPrefix(trimmed, "╰") || strings.HasPrefix(trimmed, "└") {
				return start, i
			}
		}
	}
	return -1, -1
}

// isInBoxRange checks if a line index is within a detected box range.
func isInBoxRange(idx int, ranges [][2]int) bool {
	for _, r := range ranges {
		if idx >= r[0] && idx <= r[1] {
			return true
		}
	}
	return false
}
