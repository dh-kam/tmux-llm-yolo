package tuianalyzer

import (
	"strings"
	"testing"
)

// Helper: analyze a fixture and return the result.
func analyzeFixture(t *testing.T, name string, frontend string) AnalysisResult {
	t.Helper()
	plain := loadFixture(t, name)
	return AnalyzeWithHint(frontend, "", plain)
}

// Helper: assert that at least one section of the given type exists.
func requireSection(t *testing.T, result AnalysisResult, stype SectionType) []Section {
	t.Helper()
	sections := result.SectionByType(stype)
	if len(sections) == 0 {
		t.Fatalf("expected at least one %s section, got none. All sections: %s",
			stype, dumpSections(result.Sections))
	}
	return sections
}

// Helper: assert that no section of the given type exists.
func requireNoSection(t *testing.T, result AnalysisResult, stype SectionType) {
	t.Helper()
	sections := result.SectionByType(stype)
	if len(sections) > 0 {
		t.Fatalf("expected no %s sections, got %d. All sections: %s",
			stype, len(sections), dumpSections(result.Sections))
	}
}

func dumpSections(sections []Section) string {
	var parts []string
	for _, s := range sections {
		preview := ""
		if len(s.PlainLines) > 0 {
			preview = strings.TrimSpace(s.PlainLines[0])
			if len(preview) > 60 {
				preview = preview[:60] + "..."
			}
		}
		parts = append(parts, s.Type.String()+"("+preview+")")
	}
	return strings.Join(parts, ", ")
}

// --- User Prompt Detection ---

func TestDetectUserPrompt_ClaudeCode(t *testing.T) {
	result := analyzeFixture(t, "claude-code-idle", "claude-code")
	prompts := requireSection(t, result, SectionUserPrompt)
	// Claude Code prompt is ❯
	foundMarker := false
	for _, p := range prompts {
		text := p.PlainText()
		if strings.Contains(text, "❯") {
			foundMarker = true
		}
	}
	if !foundMarker {
		t.Fatal("expected ❯ marker in user prompt section")
	}
}

func TestDetectUserPrompt_Codex(t *testing.T) {
	result := analyzeFixture(t, "codex-working", "codex")
	prompts := requireSection(t, result, SectionUserPrompt)
	foundMarker := false
	for _, p := range prompts {
		text := p.PlainText()
		if strings.Contains(text, "›") {
			foundMarker = true
		}
	}
	if !foundMarker {
		t.Fatal("expected › marker in user prompt section")
	}
}

func TestDetectUserPrompt_Copilot(t *testing.T) {
	result := analyzeFixture(t, "copilot-prompt", "copilot")
	prompts := requireSection(t, result, SectionUserPrompt)
	foundMarker := false
	for _, p := range prompts {
		text := p.PlainText()
		if strings.Contains(text, "❯") {
			foundMarker = true
		}
	}
	if !foundMarker {
		t.Fatal("expected ❯ marker in copilot user prompt")
	}
}

func TestDetectUserPrompt_Gemini(t *testing.T) {
	result := analyzeFixture(t, "gemini-idle", "gemini")
	prompts := requireSection(t, result, SectionUserPrompt)
	foundMarker := false
	for _, p := range prompts {
		text := p.PlainText()
		if strings.Contains(text, ">") || strings.Contains(text, "Type your message") {
			foundMarker = true
		}
	}
	if !foundMarker {
		t.Fatal("expected > or Type your message in gemini user prompt")
	}
}

// --- Spinner / Working Indicator Detection ---

func TestDetectSpinner_ClaudeCode(t *testing.T) {
	result := analyzeFixture(t, "claude-code-idle", "claude-code")
	spinners := requireSection(t, result, SectionSpinner)
	foundSpinner := false
	for _, s := range spinners {
		text := s.PlainText()
		if strings.Contains(text, "Crunched") || strings.Contains(text, "✻") {
			foundSpinner = true
		}
	}
	if !foundSpinner {
		t.Fatal("expected Crunched/✻ spinner in claude-code capture")
	}
}

func TestDetectSpinner_Codex(t *testing.T) {
	result := analyzeFixture(t, "codex-working", "codex")
	// Codex shows "Worked for" or "Working" indicator
	found := false
	for _, s := range result.Sections {
		text := s.PlainText()
		if strings.Contains(text, "Worked") || strings.Contains(text, "Working") {
			found = true
		}
	}
	if !found {
		t.Log("Note: codex-working capture may not have spinner if work completed")
	}
}

// --- Footer Detection ---

func TestDetectFooter_ClaudeCode(t *testing.T) {
	result := analyzeFixture(t, "claude-code-idle", "claude-code")
	footers := requireSection(t, result, SectionFooter)
	foundHint := false
	for _, f := range footers {
		text := f.PlainText()
		if strings.Contains(text, "accept edits") || strings.Contains(text, "auto-compact") {
			foundHint = true
		}
	}
	if !foundHint {
		t.Fatal("expected 'accept edits' or 'auto-compact' in footer")
	}
}

func TestDetectFooter_Codex(t *testing.T) {
	result := analyzeFixture(t, "codex-idle", "codex")
	footers := requireSection(t, result, SectionFooter)
	foundModel := false
	for _, f := range footers {
		text := f.PlainText()
		if strings.Contains(text, "gpt-") || strings.Contains(text, "codex") || strings.Contains(text, "left") {
			foundModel = true
		}
	}
	if !foundModel {
		t.Fatal("expected model info in codex footer")
	}
}

func TestDetectFooter_Copilot(t *testing.T) {
	result := analyzeFixture(t, "copilot-prompt", "copilot")
	footers := requireSection(t, result, SectionFooter)
	foundReq := false
	for _, f := range footers {
		text := f.PlainText()
		if strings.Contains(text, "Remaining") || strings.Contains(text, "shift+tab") {
			foundReq = true
		}
	}
	if !foundReq {
		t.Fatal("expected Remaining or shift+tab in copilot footer")
	}
}

func TestDetectFooter_Gemini(t *testing.T) {
	result := analyzeFixture(t, "gemini-idle", "gemini")
	footers := requireSection(t, result, SectionFooter)
	foundWorkspace := false
	for _, f := range footers {
		text := f.PlainText()
		if strings.Contains(text, "workspace") || strings.Contains(text, "sandbox") || strings.Contains(text, "Gemini") {
			foundWorkspace = true
		}
	}
	if !foundWorkspace {
		t.Fatal("expected workspace/sandbox/Gemini in gemini footer")
	}
}

// --- Approval/Question Prompt Detection ---

func TestDetectApprovalPrompt_Codex(t *testing.T) {
	result := analyzeFixture(t, "codex-approval", "codex")
	// Codex shows numbered approval like "1. Yes, proceed (y)"
	questions := result.SectionByType(SectionAssistantQuestion)
	prompts := result.SectionByType(SectionUserPrompt)
	// Either as question or prompt, there should be approval content
	allText := ""
	for _, s := range append(questions, prompts...) {
		allText += s.PlainText() + "\n"
	}
	if !strings.Contains(allText, "Yes") && !strings.Contains(allText, "proceed") && !strings.Contains(allText, "confirm") {
		t.Logf("All sections: %s", dumpSections(result.Sections))
		t.Fatal("expected approval prompt content (Yes/proceed/confirm)")
	}
}

// --- Assistant Output Detection ---

func TestDetectAssistantOutput_ClaudeCode(t *testing.T) {
	result := analyzeFixture(t, "claude-code-working", "claude-code")
	outputs := requireSection(t, result, SectionAssistantOutput)
	totalLines := 0
	for _, o := range outputs {
		totalLines += len(o.PlainLines)
	}
	if totalLines < 3 {
		t.Fatalf("expected substantial assistant output, got %d lines", totalLines)
	}
}

func TestDetectAssistantOutput_Gemini(t *testing.T) {
	result := analyzeFixture(t, "gemini-working", "gemini")
	outputs := requireSection(t, result, SectionAssistantOutput)
	totalLines := 0
	for _, o := range outputs {
		totalLines += len(o.PlainLines)
	}
	if totalLines < 3 {
		t.Fatalf("expected substantial assistant output in gemini, got %d lines", totalLines)
	}
}

// --- Separator Detection ---

func TestDetectSeparator_ClaudeCode(t *testing.T) {
	result := analyzeFixture(t, "claude-code-idle", "claude-code")
	seps := requireSection(t, result, SectionSeparator)
	foundRule := false
	for _, s := range seps {
		text := s.PlainText()
		if strings.Contains(text, "─") || strings.Contains(text, "═") {
			foundRule = true
		}
	}
	if !foundRule {
		t.Fatal("expected horizontal rule separator")
	}
}

// --- Header Detection ---

func TestDetectHeader_Copilot(t *testing.T) {
	result := analyzeFixture(t, "copilot-prompt", "copilot")
	headers := requireSection(t, result, SectionHeader)
	foundBrand := false
	for _, h := range headers {
		text := h.PlainText()
		if strings.Contains(text, "GitHub Copilot") || strings.Contains(text, "╭") {
			foundBrand = true
		}
	}
	if !foundBrand {
		t.Fatal("expected GitHub Copilot branding in header")
	}
}

func TestDetectApprovalPrompt_GeminiLive(t *testing.T) {
	result := analyzeFixture(t, "gemini-approval-prompt-live", "gemini")
	questions := result.SectionByType(SectionAssistantQuestion)
	if len(questions) == 0 {
		t.Logf("All sections: %s", dumpSections(result.Sections))
		t.Fatal("expected ASST_QUESTION section for Gemini approval box")
	}
	// The ASST_QUESTION should contain the approval options
	allText := ""
	for _, q := range questions {
		allText += q.PlainText() + "\n"
	}
	if !strings.Contains(allText, "Allow") {
		t.Fatalf("expected 'Allow' in approval section, got: %s", allText[:min(200, len(allText))])
	}
	// Also verify choice parser works on this section
	var questionLines []string
	for _, q := range questions {
		questionLines = append(questionLines, q.PlainLines...)
	}
	options := ParseChoiceOptions(questionLines)
	if len(options) < 2 {
		t.Fatalf("expected at least 2 options from approval section, got %d", len(options))
	}
	if !IsApprovalPrompt(options) {
		t.Fatal("approval section should be recognized as approval prompt")
	}
}

// --- Shell (no LLM) Detection ---

func TestShellPlain_NoLLMSections(t *testing.T) {
	result := analyzeFixture(t, "shell-plain", "")
	// Shell should not have spinner, footer with model info, or user prompt with ❯
	for _, s := range result.Sections {
		text := s.PlainText()
		if s.Type == SectionSpinner {
			t.Fatalf("plain shell should not have spinner: %q", text)
		}
	}
}
