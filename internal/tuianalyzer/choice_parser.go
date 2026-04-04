package tuianalyzer

import (
	"regexp"
	"strings"
)

// ChoiceOption represents a single option in a cursor-based or numbered selection prompt.
type ChoiceOption struct {
	Index    int    // 1-based option number
	Label    string // display text (e.g. "Yes, proceed")
	Shortcut string // keyboard shortcut if any (e.g. "y", "p", "esc")
	Selected bool   // true if this option has the cursor marker (›, ●, ▸)
}

var (
	// Matches lines like: "› 1. Yes, proceed (y)" or "  2. Allow for this session"
	choiceLinePattern = regexp.MustCompile(
		`^\s*` +
			`([›●▸*]\s*)?` + // optional cursor marker
			`(\d+)[.)]\s+` + // number + delimiter
			`(.+)$`) // label text

	// Matches shortcut in parentheses at end: "(y)", "(esc)", "(p)"
	shortcutPattern = regexp.MustCompile(`\(([a-zA-Z]+)\)\s*$`)

	// Cursor markers indicating the currently selected option
	cursorMarkers = []string{"›", "●", "▸", "*"}
)

// ParseChoiceOptions extracts structured choice options from lines of text.
// It handles both numbered menus and cursor-based selection prompts from
// Codex, Gemini, Copilot, and Claude Code.
func ParseChoiceOptions(lines []string) []ChoiceOption {
	var options []ChoiceOption

	for _, line := range lines {
		// Strip box-drawing characters (│ etc.) from Gemini-style boxes
		cleaned := stripBoxChars(line)

		m := choiceLinePattern.FindStringSubmatch(cleaned)
		if m == nil {
			continue
		}

		markerGroup := strings.TrimSpace(m[1])
		numStr := m[2]
		labelFull := strings.TrimSpace(m[3])

		idx := 0
		for _, ch := range numStr {
			idx = idx*10 + int(ch-'0')
		}

		// Extract shortcut
		shortcut := ""
		if sm := shortcutPattern.FindStringSubmatch(labelFull); sm != nil {
			shortcut = strings.ToLower(sm[1])
			labelFull = strings.TrimSpace(shortcutPattern.ReplaceAllString(labelFull, ""))
		}

		// Detect cursor selection
		selected := false
		for _, marker := range cursorMarkers {
			if strings.Contains(markerGroup, marker) {
				selected = true
				break
			}
		}

		options = append(options, ChoiceOption{
			Index:    idx,
			Label:    labelFull,
			Shortcut: shortcut,
			Selected: selected,
		})
	}

	return options
}

// IsApprovalPrompt checks if the parsed options represent an approval/permission prompt.
func IsApprovalPrompt(options []ChoiceOption) bool {
	if len(options) < 2 {
		return false
	}
	for _, opt := range options {
		lower := strings.ToLower(opt.Label)
		if strings.Contains(lower, "yes") || strings.Contains(lower, "allow") ||
			strings.Contains(lower, "proceed") || strings.Contains(lower, "approve") {
			return true
		}
	}
	return false
}

// FindOptionByShortcut returns the option matching the given shortcut key, or nil.
func FindOptionByShortcut(options []ChoiceOption, key string) *ChoiceOption {
	key = strings.ToLower(strings.TrimSpace(key))
	for i := range options {
		if options[i].Shortcut == key {
			return &options[i]
		}
	}
	return nil
}

// SelectedOption returns the currently cursor-selected option, or nil.
func SelectedOption(options []ChoiceOption) *ChoiceOption {
	for i := range options {
		if options[i].Selected {
			return &options[i]
		}
	}
	return nil
}

func stripBoxChars(s string) string {
	// Remove common box-drawing characters: │ ╭ ╮ ╰ ╯ ─ ┌ ┐ └ ┘
	replacer := strings.NewReplacer(
		"│", " ", "╭", " ", "╮", " ", "╰", " ", "╯", " ",
		"┌", " ", "┐", " ", "└", " ", "┘", " ",
	)
	return replacer.Replace(s)
}
