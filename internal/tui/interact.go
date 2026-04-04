package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// InteractMode determines what the bottom panel shows.
type InteractMode int

const (
	InteractNone      InteractMode = iota // watcher-only, no input
	InteractProxy                         // user types text → forwarded to tmux
	InteractInterview                     // preference question displayed
)

// QuestionType distinguishes how the user answers.
type QuestionType int

const (
	QuestionMultipleChoice QuestionType = iota
	QuestionFreeText
)

// Question is a single developer-preference interview question.
type Question struct {
	ID       string
	Category string
	Text     string
	Type     QuestionType
	Options  []string // for multiple-choice
}

// InteractState holds the mutable state for the interaction panel.
type InteractState struct {
	Mode InteractMode

	// proxy input
	ProxyBuffer string
	ProxyCursor int

	// interview state
	CurrentQuestion *Question
	ChoiceCursor    int    // index of highlighted option (multiple-choice)
	FreeTextBuffer  string // buffer for free-text answer
	FreeTextCursor  int
}

// --- callbacks sent back to the Runner ---

// ProxySubmitMsg is sent when the user presses Enter in proxy mode.
type ProxySubmitMsg struct{ Text string }

// InterviewAnswerMsg is sent when the user answers a question.
type InterviewAnswerMsg struct {
	QuestionID string
	Answer     string // chosen option text or free-text
	Index      int    // chosen option index (-1 for free-text)
}

// InterviewSkipMsg is sent when the user presses Esc on a question.
type InterviewSkipMsg struct{ QuestionID string }

// ShowQuestionMsg tells the TUI to display an interview question.
type ShowQuestionMsg struct{ Question Question }

// ClearInterviewMsg tells the TUI to clear the interview panel.
type ClearInterviewMsg struct{}

// SetInteractModeMsg tells the TUI to switch interaction mode.
type SetInteractModeMsg struct{ Mode InteractMode }

// --- key handling ---

// HandleProxyKey processes a key press in proxy mode.
// Returns (updatedState, cmd) where cmd is non-nil if the user submitted.
func HandleProxyKey(s InteractState, key string) (InteractState, interface{}) {
	switch key {
	case "enter":
		text := strings.TrimSpace(s.ProxyBuffer)
		s.ProxyBuffer = ""
		s.ProxyCursor = 0
		if text == "" {
			return s, nil
		}
		return s, ProxySubmitMsg{Text: text}

	case "backspace", "ctrl+h":
		if s.ProxyCursor > 0 {
			runes := []rune(s.ProxyBuffer)
			s.ProxyBuffer = string(runes[:s.ProxyCursor-1]) + string(runes[s.ProxyCursor:])
			s.ProxyCursor--
		}

	case "delete":
		runes := []rune(s.ProxyBuffer)
		if s.ProxyCursor < len(runes) {
			s.ProxyBuffer = string(runes[:s.ProxyCursor]) + string(runes[s.ProxyCursor+1:])
		}

	case "left":
		if s.ProxyCursor > 0 {
			s.ProxyCursor--
		}
	case "right":
		if s.ProxyCursor < len([]rune(s.ProxyBuffer)) {
			s.ProxyCursor++
		}

	case "home", "ctrl+a":
		s.ProxyCursor = 0
	case "end", "ctrl+e":
		s.ProxyCursor = len([]rune(s.ProxyBuffer))

	case "ctrl+u":
		s.ProxyBuffer = ""
		s.ProxyCursor = 0

	case "esc":
		// switch to none mode
		s.ProxyBuffer = ""
		s.ProxyCursor = 0
		s.Mode = InteractNone
		return s, nil

	default:
		// Insert printable character
		if len(key) == 1 || (len(key) > 1 && !strings.HasPrefix(key, "ctrl+") && !strings.HasPrefix(key, "alt+")) {
			runes := []rune(s.ProxyBuffer)
			keyRunes := []rune(key)
			newBuf := make([]rune, 0, len(runes)+len(keyRunes))
			newBuf = append(newBuf, runes[:s.ProxyCursor]...)
			newBuf = append(newBuf, keyRunes...)
			newBuf = append(newBuf, runes[s.ProxyCursor:]...)
			s.ProxyBuffer = string(newBuf)
			s.ProxyCursor += len(keyRunes)
		}
	}
	return s, nil
}

// HandleInterviewKey processes a key press in interview mode.
func HandleInterviewKey(s InteractState, key string) (InteractState, interface{}) {
	q := s.CurrentQuestion
	if q == nil {
		return s, nil
	}

	switch q.Type {
	case QuestionMultipleChoice:
		return handleMultiChoiceKey(s, key)
	case QuestionFreeText:
		return handleFreeTextKey(s, key)
	}
	return s, nil
}

func handleMultiChoiceKey(s InteractState, key string) (InteractState, interface{}) {
	q := s.CurrentQuestion
	numOpts := len(q.Options)
	if numOpts == 0 {
		return s, nil
	}

	switch key {
	case "up", "k":
		if s.ChoiceCursor > 0 {
			s.ChoiceCursor--
		} else {
			s.ChoiceCursor = numOpts - 1 // wrap
		}
	case "down", "j":
		if s.ChoiceCursor < numOpts-1 {
			s.ChoiceCursor++
		} else {
			s.ChoiceCursor = 0 // wrap
		}
	case "enter":
		answer := q.Options[s.ChoiceCursor]
		return s, InterviewAnswerMsg{
			QuestionID: q.ID,
			Answer:     answer,
			Index:      s.ChoiceCursor,
		}
	case "esc":
		return s, InterviewSkipMsg{QuestionID: q.ID}
	default:
		// Number key shortcuts: 1-9
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			idx := int(key[0]-'1')
			if idx < numOpts {
				s.ChoiceCursor = idx
				answer := q.Options[idx]
				return s, InterviewAnswerMsg{
					QuestionID: q.ID,
					Answer:     answer,
					Index:      idx,
				}
			}
		}
	}
	return s, nil
}

func handleFreeTextKey(s InteractState, key string) (InteractState, interface{}) {
	q := s.CurrentQuestion

	switch key {
	case "enter":
		text := strings.TrimSpace(s.FreeTextBuffer)
		if text == "" {
			return s, nil
		}
		s.FreeTextBuffer = ""
		s.FreeTextCursor = 0
		return s, InterviewAnswerMsg{
			QuestionID: q.ID,
			Answer:     text,
			Index:      -1,
		}

	case "backspace", "ctrl+h":
		if s.FreeTextCursor > 0 {
			runes := []rune(s.FreeTextBuffer)
			s.FreeTextBuffer = string(runes[:s.FreeTextCursor-1]) + string(runes[s.FreeTextCursor:])
			s.FreeTextCursor--
		}

	case "left":
		if s.FreeTextCursor > 0 {
			s.FreeTextCursor--
		}
	case "right":
		if s.FreeTextCursor < len([]rune(s.FreeTextBuffer)) {
			s.FreeTextCursor++
		}

	case "ctrl+u":
		s.FreeTextBuffer = ""
		s.FreeTextCursor = 0

	case "esc":
		s.FreeTextBuffer = ""
		s.FreeTextCursor = 0
		return s, InterviewSkipMsg{QuestionID: q.ID}

	default:
		if len(key) == 1 || (len(key) > 1 && !strings.HasPrefix(key, "ctrl+") && !strings.HasPrefix(key, "alt+")) {
			runes := []rune(s.FreeTextBuffer)
			keyRunes := []rune(key)
			newBuf := make([]rune, 0, len(runes)+len(keyRunes))
			newBuf = append(newBuf, runes[:s.FreeTextCursor]...)
			newBuf = append(newBuf, keyRunes...)
			newBuf = append(newBuf, runes[s.FreeTextCursor:]...)
			s.FreeTextBuffer = string(newBuf)
			s.FreeTextCursor += len(keyRunes)
		}
	}
	return s, nil
}

// --- rendering ---

var (
	panelBorder    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 1)
	promptStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	cursorStyle    = lipgloss.NewStyle().Background(lipgloss.Color("63")).Foreground(lipgloss.Color("255"))
	selectedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	unselectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	hintStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true)
	questionStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("228")).Bold(true)
)

// RenderInteractPanel renders the bottom interaction panel.
func RenderInteractPanel(s InteractState, width int) string {
	if width <= 4 {
		return ""
	}
	innerWidth := width - 4 // border + padding

	switch s.Mode {
	case InteractProxy:
		return renderProxyPanel(s, innerWidth, width)
	case InteractInterview:
		return renderInterviewPanel(s, innerWidth, width)
	default:
		return renderHintBar(width)
	}
}

func renderProxyPanel(s InteractState, innerWidth int, outerWidth int) string {
	label := promptStyle.Render("> ")
	input := renderTextWithCursor(s.ProxyBuffer, s.ProxyCursor, innerWidth-3)
	hint := hintStyle.Render("Enter: send  Esc: close  Tab: interview")
	content := label + input + "\n" + hint
	return panelBorder.Width(innerWidth).Render(content)
}

func renderInterviewPanel(s InteractState, innerWidth int, outerWidth int) string {
	q := s.CurrentQuestion
	if q == nil {
		return renderHintBar(outerWidth)
	}

	var lines []string

	// Category tag + question
	catTag := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("[" + q.Category + "] ")
	lines = append(lines, catTag+questionStyle.Render(q.Text))
	lines = append(lines, "")

	switch q.Type {
	case QuestionMultipleChoice:
		for i, opt := range q.Options {
			marker := "  "
			style := unselectedStyle
			if i == s.ChoiceCursor {
				marker = "▸ "
				style = selectedStyle
			}
			num := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(
				strings.Repeat(" ", 1) + string(rune('1'+i)) + ") ",
			)
			lines = append(lines, marker+num+style.Render(opt))
		}
		lines = append(lines, "")
		lines = append(lines, hintStyle.Render("↑↓/jk: 이동  Enter/1-9: 선택  Esc: 건너뛰기  Tab: proxy"))

	case QuestionFreeText:
		label := promptStyle.Render("답변> ")
		input := renderTextWithCursor(s.FreeTextBuffer, s.FreeTextCursor, innerWidth-8)
		lines = append(lines, label+input)
		lines = append(lines, "")
		lines = append(lines, hintStyle.Render("Enter: 제출  Esc: 건너뛰기  Tab: proxy"))
	}

	return panelBorder.Width(innerWidth).Render(strings.Join(lines, "\n"))
}

func renderHintBar(width int) string {
	return hintStyle.Copy().Width(width).Align(lipgloss.Center).Render(
		"Tab: proxy input  Ctrl+Q: interview  Ctrl+C: quit",
	)
}

func renderTextWithCursor(text string, cursor int, maxWidth int) string {
	runes := []rune(text)
	if cursor > len(runes) {
		cursor = len(runes)
	}

	// Scroll if text is wider than available space
	viewStart := 0
	if cursor > maxWidth-2 {
		viewStart = cursor - maxWidth + 2
	}
	viewEnd := viewStart + maxWidth
	if viewEnd > len(runes) {
		viewEnd = len(runes)
	}

	before := string(runes[viewStart:cursor])
	var cursorChar string
	if cursor < len(runes) {
		cursorChar = cursorStyle.Render(string(runes[cursor]))
	} else {
		cursorChar = cursorStyle.Render(" ")
	}
	after := ""
	if cursor+1 < viewEnd {
		after = string(runes[cursor+1 : viewEnd])
	}

	return before + cursorChar + after
}
