package tuianalyzer

// FrontEndLayout represents the parsed semantic layout of a CLI frontend's terminal output.
type FrontEndLayout struct {
	FrontEnd   string    // "claude-code", "codex", "gemini", "copilot"
	Sections   []Section
	PlainLines []string // All plain lines (for reference)
	ANSILines  []string // All ANSI lines (for reference)
}

// SectionByType returns all sections matching the given type.
func (r *FrontEndLayout) SectionByType(t SectionType) []Section {
	var result []Section
	for _, s := range r.Sections {
		if s.Type == t {
			result = append(result, s)
		}
	}
	return result
}

// FirstSectionByType returns the first section matching the given type, or nil.
func (r *FrontEndLayout) FirstSectionByType(t SectionType) *Section {
	for i := range r.Sections {
		if r.Sections[i].Type == t {
			return &r.Sections[i]
		}
	}
	return nil
}

// LastSectionByType returns the last section matching the given type, or nil.
func (r *FrontEndLayout) LastSectionByType(t SectionType) *Section {
	for i := len(r.Sections) - 1; i >= 0; i-- {
		if r.Sections[i].Type == t {
			return &r.Sections[i]
		}
	}
	return nil
}

// TotalLines returns the total number of lines in the capture.
func (r *FrontEndLayout) TotalLines() int {
	return len(r.PlainLines)
}
