package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/dh-kam/tmux-llm-yolo/internal/llm"
	"github.com/spf13/viper"
)

type providerSpy struct {
	name      string
	runCalled bool
}

func (p *providerSpy) Name() string { return p.name }

func (p *providerSpy) Binary() string { return p.name }

func (p *providerSpy) ValidateBinary() (string, error) {
	return "gemini", nil
}

func (p *providerSpy) CheckUsage(context.Context) (llm.Usage, error) {
	return llm.Usage{}, nil
}

func (p *providerSpy) RunPrompt(context.Context, string) (string, error) {
	p.runCalled = true
	return "", nil
}

func (p *providerSpy) IsProgressingCapture(llm.Capture) (bool, string) {
	return false, ""
}

func TestCompletedCapturePathDetection(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{path: "location-gemini.completed.capture", want: true},
		{path: "/tmp/location-gemini.completed.ansi.capture", want: true},
		{path: "r8-codex.completed", want: true},
		{path: "location-gemini.working.capture", want: false},
		{path: "some/other/output.txt", want: false},
	}

	for _, tc := range cases {
		if got := isCompletedCapturePath(tc.path); got != tc.want {
			t.Fatalf("isCompletedCapturePath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestCompletedCaptureDecisionOverridesLLM(t *testing.T) {
	spy := &providerSpy{name: "gemini"}
	capture := bytes.NewBufferString("Terminal output that would otherwise require LLM")
	decision := classifyNeedToContinueFromReader(
		context.Background(),
		spy,
		capture,
		func(string, ...interface{}) {},
		"/tmp/prompt.txt",
		"/tmp/decision.txt",
		"/tmp/location-gemini.completed.capture",
		"gemini",
		"",
	)

	if spy.runCalled {
		t.Fatalf("expected RunPrompt to be skipped for completed fixture")
	}
	if decision.Action != "INJECT_CONTINUE" {
		t.Fatalf("Action=%s, want INJECT_CONTINUE", decision.Action)
	}
	if decision.Status != "COMPLETED" {
		t.Fatalf("Status=%s, want COMPLETED", decision.Status)
	}
	if !decision.Completed {
		t.Fatalf("Completed=false, want true")
	}

	if !strings.Contains(decision.Reason, "completed fixture") {
		t.Fatalf("Reason=%q, expected override reason", decision.Reason)
	}
}

func TestCompletedFixtureConsistentAcrossProviders(t *testing.T) {
	providers := []string{"gemini", "codex", "copilot", "glm", "ollama"}
	for _, name := range providers {
		spy := &providerSpy{name: name}
		decision := classifyNeedToContinueFromReader(
			context.Background(),
			spy,
			bytes.NewBufferString("some completed output"),
			func(string, ...interface{}) {},
			"/tmp/prompt.txt",
			"/tmp/decision.txt",
			"/tmp/location-gemini.completed.capture",
			name,
			"glm4:9b",
		)
		if spy.runCalled {
			t.Fatalf("%s: expected RunPrompt to be skipped", name)
		}
		if decision.Action != "INJECT_CONTINUE" || decision.Status != "COMPLETED" || !decision.Completed {
			t.Fatalf("%s: got action=%s status=%s completed=%v", name, decision.Action, decision.Status, decision.Completed)
		}
	}
}

func TestNeedToContinuePromptAddsProviderHint(t *testing.T) {
	capture := "SAMPLE OUTPUT"
	table := []struct {
		name   string
		model  string
		needle string
	}{
		{name: "gemini", model: "", needle: "For gemini"},
		{name: "codex", model: "", needle: "For codex"},
		{name: "copilot", model: "", needle: "For copilot"},
		{name: "glm", model: "", needle: "For glm"},
		{name: "ollama", model: "glm4:9b", needle: "ollama/glm4:9b"},
	}

	for _, tc := range table {
		prompt := buildNeedToContinuePrompt(capture, tc.name, tc.model)
		if !strings.Contains(prompt, tc.needle) {
			t.Fatalf("prompt for %s does not contain %q", tc.name, tc.needle)
		}
	}
}

func TestParseCaptureDecisionSupportsInjectInput(t *testing.T) {
	out := strings.Join([]string{
		"ACTION: INJECT_INPUT",
		"STATUS: UNKNOWN",
		"WORKING: false",
		"MULTIPLE_CHOICE: false",
		"RECOMMENDED_CHOICE: confirm",
		"REASON: 사용자 입력을 요구하는 선택지로 판단됨",
	}, "\n")
	decision := parseCaptureDecision(out)

	if decision.Action != "INJECT_INPUT" {
		t.Fatalf("Action=%s, want INJECT_INPUT", decision.Action)
	}
	if decision.RecommendedChoice != "confirm" {
		t.Fatalf("RecommendedChoice=%s, want confirm", decision.RecommendedChoice)
	}
}

func TestParseCaptureDecisionSelectFallbackFromMultiChoice(t *testing.T) {
	out := strings.Join([]string{
		"ACTION: SKIP",
		"STATUS: WAITING",
		"MULTIPLE_CHOICE: true",
		"RECOMMENDED_CHOICE: 3)",
		"REASON: 멀티초이스로 보임",
	}, "\n")
	decision := parseCaptureDecision(out)

	if decision.Action != "INJECT_SELECT" {
		t.Fatalf("Action=%s, want INJECT_SELECT", decision.Action)
	}
	if normalized := normalizeChoiceCandidate(decision.RecommendedChoice); normalized != "3" {
		t.Fatalf("normalized choice=%s, want 3", normalized)
	}
}

func TestParseChoiceCandidateAndLabel(t *testing.T) {
	choice, label := parseChoiceCandidateAndLabel("3) Exit")
	if choice != "3" {
		t.Fatalf("choice=%s, want 3", choice)
	}
	if label != "Exit" {
		t.Fatalf("label=%q, want Exit", label)
	}

	choice, label = parseChoiceCandidateAndLabel("  4.  Run check-all  ")
	if choice != "4" {
		t.Fatalf("choice=%s, want 4", choice)
	}
	if label != "Run check-all" {
		t.Fatalf("label=%q, want Run check-all", label)
	}

	choice, label = parseChoiceCandidateAndLabel("confirm now")
	if choice != "" || label != "" {
		t.Fatalf("want empty choice/label, got choice=%q label=%q", choice, label)
	}
}

func TestLoadConfigParsesFallbackLLM(t *testing.T) {
	t.Setenv("FALLBACK_LLM", "")
	t.Setenv("FALLBACK_LLM_MODEL", "")
	origLLM := viper.GetString("llm")
	origLLMModel := viper.GetString("llm-model")
	origFallbackLLM := viper.GetString("fallback-llm")
	origFallbackLLMModel := viper.GetString("fallback-llm-model")
	t.Cleanup(func() {
		viper.Set("llm", origLLM)
		viper.Set("llm-model", origLLMModel)
		viper.Set("fallback-llm", origFallbackLLM)
		viper.Set("fallback-llm-model", origFallbackLLMModel)
	})

	viper.Set("llm", "codex")
	viper.Set("llm-model", "gpt-5")
	viper.Set("fallback-llm", "ollama/qwen2.5-coder")
	viper.Set("fallback-llm-model", "")

	cfg, err := loadConfig(nil)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if cfg.llm != "codex" || cfg.llmModel != "gpt-5" {
		t.Fatalf("primary llm parsed incorrectly: %s/%s", cfg.llm, cfg.llmModel)
	}
	if cfg.fallbackLLM != "ollama" || cfg.fallbackLLMModel != "qwen2.5-coder" {
		t.Fatalf("fallback llm parsed incorrectly: %s/%s", cfg.fallbackLLM, cfg.fallbackLLMModel)
	}
}
