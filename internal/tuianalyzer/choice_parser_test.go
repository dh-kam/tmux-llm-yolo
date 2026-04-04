package tuianalyzer

import (
	"testing"
)

func TestParseChoiceOptions_CodexApproval(t *testing.T) {
	lines := loadFixtureLines(t, "codex-approval-prompt")
	options := ParseChoiceOptions(lines)

	if len(options) < 3 {
		t.Fatalf("expected ≥3 options from codex approval, got %d", len(options))
	}

	// Option 1: "Yes, proceed" with shortcut (y)
	if options[0].Index != 1 {
		t.Errorf("option 0 index = %d, want 1", options[0].Index)
	}
	if options[0].Shortcut != "y" {
		t.Errorf("option 0 shortcut = %q, want 'y'", options[0].Shortcut)
	}
	if !options[0].Selected {
		t.Error("option 0 should be selected (has › marker)")
	}

	// Option 2: "Yes, and don't ask again..." with shortcut (p)
	if options[1].Index != 2 {
		t.Errorf("option 1 index = %d, want 2", options[1].Index)
	}
	if options[1].Shortcut != "p" {
		t.Errorf("option 1 shortcut = %q, want 'p'", options[1].Shortcut)
	}
	if options[1].Selected {
		t.Error("option 1 should not be selected")
	}

	// Option 3: "No, and tell Codex..." with shortcut (esc)
	if options[2].Index != 3 {
		t.Errorf("option 2 index = %d, want 3", options[2].Index)
	}
	if options[2].Shortcut != "esc" {
		t.Errorf("option 2 shortcut = %q, want 'esc'", options[2].Shortcut)
	}

	t.Logf("Codex options: %+v", options)
}

func TestParseChoiceOptions_GeminiApproval(t *testing.T) {
	lines := loadFixtureLines(t, "gemini-approval-prompt")
	options := ParseChoiceOptions(lines)

	if len(options) < 3 {
		t.Fatalf("expected ≥3 options from gemini approval, got %d", len(options))
	}

	// Option 1: "Allow once" with ● marker (selected)
	if options[0].Index != 1 {
		t.Errorf("option 0 index = %d, want 1", options[0].Index)
	}
	if options[0].Selected != true {
		t.Error("option 0 should be selected (● marker)")
	}
	if got := options[0].Label; got != "Allow once" {
		t.Errorf("option 0 label = %q, want 'Allow once'", got)
	}

	// Option 2: "Allow for this session"
	if options[1].Index != 2 {
		t.Errorf("option 1 index = %d, want 2", options[1].Index)
	}
	if got := options[1].Label; got != "Allow for this session" {
		t.Errorf("option 1 label = %q, want 'Allow for this session'", got)
	}

	// Option 3: "No, suggest changes" with shortcut (esc)
	if options[2].Index != 3 {
		t.Errorf("option 2 index = %d, want 3", options[2].Index)
	}
	if options[2].Shortcut != "esc" {
		t.Errorf("option 2 shortcut = %q, want 'esc'", options[2].Shortcut)
	}

	t.Logf("Gemini options: %+v", options)
}

func TestIsApprovalPrompt(t *testing.T) {
	codexLines := loadFixtureLines(t, "codex-approval-prompt")
	codexOpts := ParseChoiceOptions(codexLines)
	if !IsApprovalPrompt(codexOpts) {
		t.Fatal("codex approval should be detected as approval prompt")
	}

	geminiLines := loadFixtureLines(t, "gemini-approval-prompt")
	geminiOpts := ParseChoiceOptions(geminiLines)
	if !IsApprovalPrompt(geminiOpts) {
		t.Fatal("gemini approval should be detected as approval prompt")
	}
}

func TestFindOptionByShortcut(t *testing.T) {
	codexLines := loadFixtureLines(t, "codex-approval-prompt")
	options := ParseChoiceOptions(codexLines)

	opt := FindOptionByShortcut(options, "y")
	if opt == nil {
		t.Fatal("expected to find option with shortcut 'y'")
	}
	if opt.Index != 1 {
		t.Errorf("shortcut 'y' should be option 1, got %d", opt.Index)
	}

	opt = FindOptionByShortcut(options, "p")
	if opt == nil {
		t.Fatal("expected to find option with shortcut 'p'")
	}
	if opt.Index != 2 {
		t.Errorf("shortcut 'p' should be option 2, got %d", opt.Index)
	}

	opt = FindOptionByShortcut(options, "x")
	if opt != nil {
		t.Fatal("should not find option with shortcut 'x'")
	}
}

func TestSelectedOption(t *testing.T) {
	codexLines := loadFixtureLines(t, "codex-approval-prompt")
	options := ParseChoiceOptions(codexLines)

	sel := SelectedOption(options)
	if sel == nil {
		t.Fatal("expected a selected option")
	}
	if sel.Index != 1 {
		t.Errorf("selected option index = %d, want 1", sel.Index)
	}
}

func TestParseChoiceOptions_GeminiLiveApproval(t *testing.T) {
	lines := loadFixtureLines(t, "gemini-approval-prompt-live")
	options := ParseChoiceOptions(lines)

	if len(options) < 3 {
		t.Fatalf("expected >= 3 options from live gemini approval, got %d", len(options))
	}

	// Verify option 1 is selected
	sel := SelectedOption(options)
	if sel == nil {
		t.Fatal("expected a selected option in live capture")
	}
	if sel.Index != 1 {
		t.Errorf("selected option index = %d, want 1", sel.Index)
	}

	// Verify it's recognized as approval
	if !IsApprovalPrompt(options) {
		t.Fatal("live gemini capture should be detected as approval prompt")
	}

	t.Logf("Live Gemini options: %+v", options)
}

func TestParseChoiceOptions_NoOptions(t *testing.T) {
	lines := loadFixtureLines(t, "claude-code-idle")
	options := ParseChoiceOptions(lines)
	// Claude Code idle screen has no numbered choice options
	// (it might pick up task numbers, so just check it doesn't crash)
	t.Logf("claude-code-idle options found: %d", len(options))
}
