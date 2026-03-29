package tuianalyzer

import (
	"strings"
	"testing"
)

func TestCaptureHistory_Empty(t *testing.T) {
	h := NewCaptureHistory()
	s := h.Summary()
	if s.TotalCaptures != 0 {
		t.Errorf("expected 0 captures, got %d", s.TotalCaptures)
	}
	if len(s.StableFooterLines) != 0 {
		t.Errorf("expected no stable footer lines, got %d", len(s.StableFooterLines))
	}
}

func TestCaptureHistory_SingleRecord(t *testing.T) {
	h := NewCaptureHistory()

	plain := strings.Join([]string{
		"╭─── Claude Code ───╮",
		"│     Welcome!      │",
		"╰───────────────────╯",
		"❯ hello",
		"  esc to interrupt",
	}, "\n")

	result := Analyze("", plain)
	h.Record(result)

	s := h.Summary()
	if s.TotalCaptures != 1 {
		t.Errorf("expected 1 capture, got %d", s.TotalCaptures)
	}
	// Need at least 2 captures for stable lines
	if len(s.StableFooterLines) != 0 {
		t.Errorf("expected no stable lines with single capture, got %d", len(s.StableFooterLines))
	}
}

func TestCaptureHistory_StableFooterDetection(t *testing.T) {
	h := NewCaptureHistory()

	// Simulate multiple captures with the same footer
	for i := 0; i < 3; i++ {
		plain := strings.Join([]string{
			"Some output line " + strings.Repeat("x", i),
			"❯ prompt",
			"  glm-5 · 72% left · /workspace/test",
		}, "\n")

		result := Analyze("", plain)
		h.Record(result)
	}

	s := h.Summary()
	if s.TotalCaptures != 3 {
		t.Errorf("expected 3 captures, got %d", s.TotalCaptures)
	}

	// Should detect stable footer at the bottom
	if len(s.StableFooterLines) == 0 {
		t.Error("expected stable footer lines after 3 captures")
	}

	found := false
	for _, sl := range s.StableFooterLines {
		if sl.TypeHint == SectionFooter {
			found = true
			t.Logf("stable footer: fromBottom=%d count=%d fp=%q", sl.FromBottom, sl.Count, sl.Fingerprint)
		}
	}
	if !found {
		t.Error("no stable footer section hint found")
	}
}

func TestCaptureHistory_StableHeaderDetection(t *testing.T) {
	h := NewCaptureHistory()

	for i := 0; i < 3; i++ {
		plain := strings.Join([]string{
			"╭─── Claude Code ───╮",
			"│  Welcome back!    │",
			"╰───────────────────╯",
			"Some output " + strings.Repeat("a", i),
			"❯ prompt",
			"  esc to interrupt",
		}, "\n")

		result := Analyze("", plain)
		h.Record(result)
	}

	s := h.Summary()
	if len(s.StableHeaderLines) == 0 {
		t.Error("expected stable header lines after 3 captures")
	}

	for _, sl := range s.StableHeaderLines {
		t.Logf("stable header: count=%d fp=%q", sl.Count, sl.Fingerprint)
	}
}

func TestCaptureHistory_LearningBoostsConfidence(t *testing.T) {
	h := NewCaptureHistory()

	// Build history with consistent captures
	for i := 0; i < 4; i++ {
		plain := strings.Join([]string{
			"Some assistant output",
			"❯ prompt text",
			"  esc to interrupt",
		}, "\n")
		h.Record(Analyze("", plain))
	}

	// Now analyze a new capture with the same footer
	newPlain := strings.Join([]string{
		"Different output",
		"❯ new prompt",
		"  esc to interrupt",
	}, "\n")

	// Without learning
	withoutLearning := Analyze("", newPlain)

	// With learning
	withLearning := h.AnalyzeWithLearning("", newPlain)

	// Footer confidence should be boosted
	footerWithout := withoutLearning.LastSectionByType(SectionFooter)
	footerWith := withLearning.LastSectionByType(SectionFooter)

	if footerWithout != nil && footerWith != nil {
		if footerWith.Confidence < footerWithout.Confidence {
			t.Errorf("learning should boost footer confidence: before=%.2f after=%.2f",
				footerWithout.Confidence, footerWith.Confidence)
		}
		t.Logf("footer confidence: without=%.2f with=%.2f", footerWithout.Confidence, footerWith.Confidence)
	}
}

func TestCaptureHistory_Reset(t *testing.T) {
	h := NewCaptureHistory()

	plain := strings.Join([]string{
		"❯ test",
		"  esc to interrupt",
	}, "\n")
	h.Record(Analyze("", plain))
	h.Record(Analyze("", plain))

	if h.Summary().TotalCaptures != 2 {
		t.Error("expected 2 captures before reset")
	}

	h.Reset()

	if h.Summary().TotalCaptures != 0 {
		t.Errorf("expected 0 captures after reset, got %d", h.Summary().TotalCaptures)
	}
	if len(h.Summary().StableFooterLines) != 0 {
		t.Error("expected no stable lines after reset")
	}
}

func TestCaptureHistory_MixedProviders(t *testing.T) {
	h := NewCaptureHistory()

	// Alternate between providers
	codexPlain := strings.Join([]string{
		"╭─── OpenAI Codex ───╮",
		"│                     │",
		"╰─────────────────────╯",
		"› test prompt",
		"  gpt-5.4 medium · 100% left · /workspace",
	}, "\n")

	glmPlain := strings.Join([]string{
		"╭─── Claude Code ───╮",
		"│  Welcome!          │",
		"╰────────────────────╯",
		"❯ test prompt",
		"  esc to interrupt",
	}, "\n")

	h.Record(Analyze("", codexPlain))
	h.Record(Analyze("", glmPlain))
	h.Record(Analyze("", codexPlain))

	s := h.Summary()
	// Latest provider should be codex
	if s.FrontEnd != "codex" {
		t.Errorf("expected latest frontend codex, got %q", s.FrontEnd)
	}
}

func TestNormalizeFingerprint(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"gpt-5.4 medium · 100% left · /workspace", "gpt-#.# medium · #% left · /workspace"},
		{"gpt-5.3-codex high · 36% left", "gpt-#.#-codex high · #% left"},
		{"", ""},
		{"   ", ""},
		{"✻ Cogitated for 56s", "✻ cogitated for #s"},
	}

	for _, tc := range tests {
		got := normalizeFingerprint(tc.input)
		if got != tc.expected {
			t.Errorf("normalizeFingerprint(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
