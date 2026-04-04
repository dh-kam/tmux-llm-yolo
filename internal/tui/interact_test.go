package tui

import (
	"testing"
)

func TestHandleProxyKey_TypeAndSubmit(t *testing.T) {
	s := InteractState{Mode: InteractProxy}

	// Type "hello"
	for _, ch := range "hello" {
		s, _ = HandleProxyKey(s, string(ch))
	}
	if s.ProxyBuffer != "hello" {
		t.Fatalf("expected 'hello', got %q", s.ProxyBuffer)
	}
	if s.ProxyCursor != 5 {
		t.Fatalf("expected cursor 5, got %d", s.ProxyCursor)
	}

	// Submit
	var result interface{}
	s, result = HandleProxyKey(s, "enter")
	if result == nil {
		t.Fatal("expected ProxySubmitMsg")
	}
	msg, ok := result.(ProxySubmitMsg)
	if !ok {
		t.Fatalf("expected ProxySubmitMsg, got %T", result)
	}
	if msg.Text != "hello" {
		t.Fatalf("expected 'hello', got %q", msg.Text)
	}
	if s.ProxyBuffer != "" {
		t.Fatal("buffer should be cleared after submit")
	}
}

func TestHandleProxyKey_Backspace(t *testing.T) {
	s := InteractState{Mode: InteractProxy, ProxyBuffer: "abc", ProxyCursor: 3}
	s, _ = HandleProxyKey(s, "backspace")
	if s.ProxyBuffer != "ab" {
		t.Fatalf("expected 'ab', got %q", s.ProxyBuffer)
	}
}

func TestHandleProxyKey_CursorMovement(t *testing.T) {
	s := InteractState{Mode: InteractProxy, ProxyBuffer: "abc", ProxyCursor: 3}
	s, _ = HandleProxyKey(s, "left")
	if s.ProxyCursor != 2 {
		t.Fatalf("expected 2, got %d", s.ProxyCursor)
	}
	s, _ = HandleProxyKey(s, "home")
	if s.ProxyCursor != 0 {
		t.Fatalf("expected 0, got %d", s.ProxyCursor)
	}
	s, _ = HandleProxyKey(s, "end")
	if s.ProxyCursor != 3 {
		t.Fatalf("expected 3, got %d", s.ProxyCursor)
	}
}

func TestHandleProxyKey_Esc(t *testing.T) {
	s := InteractState{Mode: InteractProxy, ProxyBuffer: "text"}
	s, _ = HandleProxyKey(s, "esc")
	if s.Mode != InteractNone {
		t.Fatal("expected InteractNone")
	}
	if s.ProxyBuffer != "" {
		t.Fatal("buffer should be cleared")
	}
}

func TestHandleProxyKey_ClearLine(t *testing.T) {
	s := InteractState{Mode: InteractProxy, ProxyBuffer: "test", ProxyCursor: 4}
	s, _ = HandleProxyKey(s, "ctrl+u")
	if s.ProxyBuffer != "" || s.ProxyCursor != 0 {
		t.Fatal("ctrl+u should clear line")
	}
}

func TestHandleProxyKey_EmptySubmit(t *testing.T) {
	s := InteractState{Mode: InteractProxy}
	s, result := HandleProxyKey(s, "enter")
	if result != nil {
		t.Fatal("empty submit should return nil")
	}
	_ = s
}

func TestHandleInterviewKey_MultipleChoice(t *testing.T) {
	q := &Question{
		ID:      "test_q",
		Type:    QuestionMultipleChoice,
		Options: []string{"Option A", "Option B", "Option C"},
	}
	s := InteractState{
		Mode:            InteractInterview,
		CurrentQuestion: q,
		ChoiceCursor:    0,
	}

	// Move down
	s, _ = HandleInterviewKey(s, "down")
	if s.ChoiceCursor != 1 {
		t.Fatalf("expected cursor 1, got %d", s.ChoiceCursor)
	}

	// Move down again
	s, _ = HandleInterviewKey(s, "j")
	if s.ChoiceCursor != 2 {
		t.Fatalf("expected cursor 2, got %d", s.ChoiceCursor)
	}

	// Wrap around
	s, _ = HandleInterviewKey(s, "down")
	if s.ChoiceCursor != 0 {
		t.Fatalf("expected wrap to 0, got %d", s.ChoiceCursor)
	}

	// Move up wraps
	s, _ = HandleInterviewKey(s, "up")
	if s.ChoiceCursor != 2 {
		t.Fatalf("expected wrap to 2, got %d", s.ChoiceCursor)
	}

	// Select with enter
	s.ChoiceCursor = 1
	var result interface{}
	s, result = HandleInterviewKey(s, "enter")
	if result == nil {
		t.Fatal("expected InterviewAnswerMsg")
	}
	msg, ok := result.(InterviewAnswerMsg)
	if !ok {
		t.Fatalf("expected InterviewAnswerMsg, got %T", result)
	}
	if msg.Answer != "Option B" || msg.Index != 1 {
		t.Fatalf("unexpected answer: %+v", msg)
	}
}

func TestHandleInterviewKey_NumberShortcut(t *testing.T) {
	q := &Question{
		ID:      "test_q",
		Type:    QuestionMultipleChoice,
		Options: []string{"A", "B", "C"},
	}
	s := InteractState{
		Mode:            InteractInterview,
		CurrentQuestion: q,
	}

	s, result := HandleInterviewKey(s, "2")
	msg, ok := result.(InterviewAnswerMsg)
	if !ok {
		t.Fatal("expected InterviewAnswerMsg from number key")
	}
	if msg.Answer != "B" || msg.Index != 1 {
		t.Fatalf("unexpected: %+v", msg)
	}
	_ = s
}

func TestHandleInterviewKey_Skip(t *testing.T) {
	q := &Question{ID: "test_q", Type: QuestionMultipleChoice, Options: []string{"A"}}
	s := InteractState{Mode: InteractInterview, CurrentQuestion: q}

	s, result := HandleInterviewKey(s, "esc")
	skip, ok := result.(InterviewSkipMsg)
	if !ok {
		t.Fatal("expected InterviewSkipMsg")
	}
	if skip.QuestionID != "test_q" {
		t.Fatal("wrong question ID")
	}
	_ = s
}

func TestHandleInterviewKey_FreeText(t *testing.T) {
	q := &Question{ID: "free_q", Type: QuestionFreeText}
	s := InteractState{
		Mode:            InteractInterview,
		CurrentQuestion: q,
	}

	// Type answer
	for _, ch := range "my answer" {
		s, _ = HandleInterviewKey(s, string(ch))
	}
	if s.FreeTextBuffer != "my answer" {
		t.Fatalf("expected 'my answer', got %q", s.FreeTextBuffer)
	}

	// Submit
	s, result := HandleInterviewKey(s, "enter")
	msg, ok := result.(InterviewAnswerMsg)
	if !ok {
		t.Fatal("expected InterviewAnswerMsg")
	}
	if msg.Answer != "my answer" || msg.Index != -1 {
		t.Fatalf("unexpected: %+v", msg)
	}
}

func TestRenderInteractPanel_Modes(t *testing.T) {
	// None mode shows hint bar
	s := InteractState{Mode: InteractNone}
	out := RenderInteractPanel(s, 80)
	if out == "" {
		t.Fatal("expected hint bar")
	}

	// Proxy mode shows input
	s.Mode = InteractProxy
	out = RenderInteractPanel(s, 80)
	if out == "" {
		t.Fatal("expected proxy panel")
	}

	// Interview mode with no question shows hint
	s.Mode = InteractInterview
	out = RenderInteractPanel(s, 80)
	if out == "" {
		t.Fatal("expected hint bar when no question")
	}

	// Interview mode with question
	s.CurrentQuestion = &Question{
		ID:       "q1",
		Category: "테스트",
		Text:     "질문입니다",
		Type:     QuestionMultipleChoice,
		Options:  []string{"A", "B"},
	}
	out = RenderInteractPanel(s, 80)
	if out == "" {
		t.Fatal("expected interview panel")
	}
}
