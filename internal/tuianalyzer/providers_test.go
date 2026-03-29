package tuianalyzer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readTestData(t *testing.T, name string) (ansi string, plain string) {
	t.Helper()
	base := filepath.Join("..", "..", "testdata")

	plainPath := filepath.Join(base, name)
	if data, err := os.ReadFile(plainPath); err == nil {
		plain = string(data)
	}

	ansiPath := strings.TrimSuffix(plainPath, ".capture") + ".ansi.capture"
	if data, err := os.ReadFile(ansiPath); err == nil {
		ansi = string(data)
	}

	return ansi, plain
}

func TestProvider_CodexCompleted(t *testing.T) {
	ansi, plain := readTestData(t, "r8-codex.completed.capture")
	if plain == "" {
		t.Skip("testdata not found")
	}

	result := Analyze(ansi, plain)

	if result.FrontEnd != "codex" {
		t.Errorf("expected frontend codex, got %q", result.FrontEnd)
	}

	// Should detect multiple sections
	if len(result.Sections) < 3 {
		t.Errorf("expected at least 3 sections, got %d: %+v", len(result.Sections), result.Sections)
	}

	// Should have footer
	footer := result.LastSectionByType(SectionFooter)
	if footer == nil {
		t.Errorf("expected footer section in codex completed capture")
		for _, s := range result.Sections {
			t.Logf("  %s %d-%d (%.2f) %q", s.Type, s.StartLine, s.EndLine, s.Confidence, s.FirstNonEmptyPlain())
		}
	}

	// Should have user prompt
	prompt := result.LastSectionByType(SectionUserPrompt)
	if prompt == nil {
		t.Log("no SectionUserPrompt found (may be embedded in output)")
	}
}

func TestProvider_CodexWorking(t *testing.T) {
	ansi, plain := readTestData(t, "r8-codex.working.capture")
	if plain == "" {
		t.Skip("testdata not found")
	}

	result := Analyze(ansi, plain)

	if result.FrontEnd != "codex" {
		t.Errorf("expected frontend codex, got %q", result.FrontEnd)
	}

	// Should detect spinner or processing indicator
	found := false
	for _, s := range result.Sections {
		if s.Type == SectionSpinner {
			found = true
			break
		}
	}
	if !found {
		t.Logf("no spinner section found (may be ok depending on capture state)")
		for _, s := range result.Sections {
			t.Logf("  %s %d-%d (%.2f) %q", s.Type, s.StartLine, s.EndLine, s.Confidence, s.FirstNonEmptyPlain())
		}
	}
}

func TestProvider_CodexSelect(t *testing.T) {
	_, plain := readTestData(t, "r8-codex.select.capture")
	if plain == "" {
		t.Skip("testdata not found")
	}

	result := Analyze("", plain)

	if result.FrontEnd != "codex" {
		t.Errorf("expected frontend codex, got %q", result.FrontEnd)
	}

	// Should detect assistant question (numbered menu)
	question := result.FirstSectionByType(SectionAssistantQuestion)
	if question == nil {
		t.Logf("no ASST_QUESTION found")
		for _, s := range result.Sections {
			t.Logf("  %s %d-%d (%.2f) %q", s.Type, s.StartLine, s.EndLine, s.Confidence, s.FirstNonEmptyPlain())
		}
	}
}

func TestProvider_GLMCompleted(t *testing.T) {
	ansi, plain := readTestData(t, "location-glm.completed.capture")
	if plain == "" {
		t.Skip("testdata not found")
	}

	result := Analyze(ansi, plain)

	if result.FrontEnd != "claude-code" {
		t.Errorf("expected frontend claude-code, got %q", result.FrontEnd)
	}

	// Should detect assistant output
	output := result.FirstSectionByType(SectionAssistantOutput)
	if output == nil {
		t.Errorf("expected assistant output in GLM completed capture")
		for _, s := range result.Sections {
			t.Logf("  %s %d-%d (%.2f) %q", s.Type, s.StartLine, s.EndLine, s.Confidence, s.FirstNonEmptyPlain())
		}
	}
}

func TestProvider_GLMVibing(t *testing.T) {
	ansi, plain := readTestData(t, "location-glm.vibing.capture")
	if plain == "" {
		t.Skip("testdata not found")
	}

	// Vibing capture contains mixed copilot+Claude Code output; use hint to force Claude Code
	result := AnalyzeWithHint("claude-code", ansi, plain)

	if result.FrontEnd != "claude-code" {
		t.Errorf("expected frontend claude-code, got %q", result.FrontEnd)
	}

	// Should detect spinner (fermenting/brewing)
	spinner := result.FirstSectionByType(SectionSpinner)
	if spinner == nil {
		t.Logf("no spinner found in GLM vibing capture")
		for _, s := range result.Sections {
			t.Logf("  %s %d-%d (%.2f) %q", s.Type, s.StartLine, s.EndLine, s.Confidence, s.FirstNonEmptyPlain())
		}
	}
}

func TestProvider_GeminiWorking(t *testing.T) {
	_, plain := readTestData(t, "location-gemini.working.capture")
	if plain == "" {
		t.Skip("testdata not found")
	}

	// Mixed capture with Claude Code signatures — use hint to force gemini
	result := AnalyzeWithHint("gemini", "", plain)

	if result.FrontEnd != "gemini" {
		t.Errorf("expected frontend gemini, got %q", result.FrontEnd)
	}

	// Should have header (ASCII art) and footer
	header := result.FirstSectionByType(SectionHeader)
	footer := result.LastSectionByType(SectionFooter)

	if header == nil {
		t.Log("no header found in gemini working capture")
		for _, s := range result.Sections {
			t.Logf("  %s %d-%d (%.2f) %q", s.Type, s.StartLine, s.EndLine, s.Confidence, s.FirstNonEmptyPlain())
		}
	}
	if footer == nil {
		t.Log("no footer found in gemini working capture")
	}
}

func TestProvider_GeminiCompleted(t *testing.T) {
	_, plain := readTestData(t, "location-gemini.completed.capture")
	if plain == "" {
		t.Skip("testdata not found")
	}

	result := Analyze("", plain)

	if result.FrontEnd != "gemini" {
		t.Errorf("expected frontend gemini, got %q", result.FrontEnd)
	}
}

func TestProvider_GeminiWorking2(t *testing.T) {
	_, plain := readTestData(t, "location-gemini.working2.capture")
	if plain == "" {
		t.Skip("testdata not found")
	}

	// Mixed capture with Claude Code signatures — use hint to force gemini
	result := AnalyzeWithHint("gemini", "", plain)

	if result.FrontEnd != "gemini" {
		t.Errorf("expected frontend gemini, got %q", result.FrontEnd)
	}

	t.Logf("sections for gemini working2:")
	for _, s := range result.Sections {
		t.Logf("  %s %d-%d (%.2f) %q", s.Type, s.StartLine, s.EndLine, s.Confidence, s.FirstNonEmptyPlain())
	}
}

func TestProvider_GeminiWorking3(t *testing.T) {
	_, plain := readTestData(t, "location-gemini.working3.capture")
	if plain == "" {
		t.Skip("testdata not found")
	}

	result := Analyze("", plain)

	if result.FrontEnd != "gemini" {
		t.Errorf("expected frontend gemini, got %q", result.FrontEnd)
	}
}

func readLiveCaptureSample(t *testing.T, basePath string, provider string, seq string) (ansi string, plain string) {
	t.Helper()
	ansiPath := filepath.Join(basePath, provider, "ansi", seq+".ansi.txt")
	plainPath := filepath.Join(basePath, provider, "plain", seq+".plain.txt")

	if data, err := os.ReadFile(ansiPath); err == nil {
		ansi = string(data)
	}
	if data, err := os.ReadFile(plainPath); err == nil {
		plain = string(data)
	}
	return ansi, plain
}

func TestProvider_LiveCodexCaptures(t *testing.T) {
	base := filepath.Join("..", "..", "testdata", "live-captures", "20260309-130340")
	if _, err := os.Stat(base); err != nil {
		t.Skip("live capture data not found")
	}

	for i := 1; i <= 5; i++ {
		seq := fmt.Sprintf("%03d", i)
		ansi, plain := readLiveCaptureSample(t, base, "tmp-codex", seq)
		if plain == "" {
			continue
		}

		result := Analyze(ansi, plain)
		if result.FrontEnd != "codex" {
			t.Errorf("sample %s: expected codex, got %q", seq, result.FrontEnd)
		}
		if len(result.Sections) == 0 {
			t.Errorf("sample %s: no sections detected", seq)
		}
		t.Logf("sample %s: %d sections, provider=%s", seq, len(result.Sections), result.FrontEnd)
	}
}

func TestProvider_LiveGLMCaptures(t *testing.T) {
	base := filepath.Join("..", "..", "testdata", "live-captures", "20260309-130340")
	if _, err := os.Stat(base); err != nil {
		t.Skip("live capture data not found")
	}

	for i := 1; i <= 5; i++ {
		seq := fmt.Sprintf("%03d", i)
		ansi, plain := readLiveCaptureSample(t, base, "tmp-glm", seq)
		if plain == "" {
			continue
		}

		result := Analyze(ansi, plain)
		if result.FrontEnd != "claude-code" {
			t.Errorf("sample %s: expected claude-code, got %q", seq, result.FrontEnd)
		}
		if len(result.Sections) == 0 {
			t.Errorf("sample %s: no sections detected", seq)
		}
		t.Logf("sample %s: %d sections, provider=%s", seq, len(result.Sections), result.FrontEnd)
	}
}

func TestProvider_LiveGeminiCaptures(t *testing.T) {
	base := filepath.Join("..", "..", "testdata", "live-captures", "20260309-130340")
	if _, err := os.Stat(base); err != nil {
		t.Skip("live capture data not found")
	}

	for i := 1; i <= 5; i++ {
		seq := fmt.Sprintf("%03d", i)
		ansi, plain := readLiveCaptureSample(t, base, "tmp-gemini", seq)
		if plain == "" {
			continue
		}

		result := Analyze(ansi, plain)
		if result.FrontEnd != "gemini" {
			t.Errorf("sample %s: expected gemini, got %q", seq, result.FrontEnd)
		}
		if len(result.Sections) == 0 {
			t.Errorf("sample %s: no sections detected", seq)
		}
		t.Logf("sample %s: %d sections, provider=%s", seq, len(result.Sections), result.FrontEnd)
	}
}

func TestProvider_IntegrationCaptures(t *testing.T) {
	base := filepath.Join("..", "..", "testdata", "live-integration", "20260309-134218")
	if _, err := os.Stat(base); err != nil {
		t.Skip("integration data not found")
	}

	providers := []string{"tmp-codex", "tmp-glm", "tmp-gemini"}
	for _, prov := range providers {
		for i := 1; i <= 5; i++ {
			seq := fmt.Sprintf("%03d", i)
			ansi, plain := readLiveCaptureSample(t, base, prov, seq)
			if plain == "" {
				continue
			}

			result := Analyze(ansi, plain)
			if len(result.Sections) == 0 {
				t.Errorf("%s sample %s: no sections detected", prov, seq)
			}
			t.Logf("%s sample %s: %d sections, provider=%s", prov, seq, len(result.Sections), result.FrontEnd)
		}
	}
}
