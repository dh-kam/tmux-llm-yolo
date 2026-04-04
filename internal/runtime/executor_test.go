package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestResolveExecutorProfileForCopilotOverridesSubmitKeys(t *testing.T) {
	profile := resolveExecutorProfile(Config{
		Target:            "tmp-copilot",
		SubmitKey:         "C-m",
		SubmitKeyFallback: "C-m",
	}, "copilot")

	if profile.provider != "copilot" {
		t.Fatalf("provider=%q want copilot", profile.provider)
	}
	if profile.submitKey != "C-m" {
		t.Fatalf("submitKey=%q want C-m", profile.submitKey)
	}
	if profile.fallbackSubmitKey != "" {
		t.Fatalf("fallbackSubmitKey=%q want empty", profile.fallbackSubmitKey)
	}
	if !profile.clearBeforeTyping {
		t.Fatal("clearBeforeTyping=false want true")
	}
}

func TestResolveExecutorProfileForCodexClearsPrompt(t *testing.T) {
	profile := resolveExecutorProfile(Config{
		Target:            "tmp-codex",
		SubmitKey:         "C-m",
		SubmitKeyFallback: "Enter",
	}, "codex")

	if profile.provider != "codex" {
		t.Fatalf("provider=%q want codex", profile.provider)
	}
	if !profile.clearBeforeTyping {
		t.Fatal("clearBeforeTyping=false want true")
	}
	if profile.submitKey != "C-m" {
		t.Fatalf("submitKey=%q want C-m", profile.submitKey)
	}
	if profile.fallbackSubmitKey != "Enter" {
		t.Fatalf("fallbackSubmitKey=%q want Enter", profile.fallbackSubmitKey)
	}
}

func TestNewActionExecutorSelectsProviderSpecificExecutor(t *testing.T) {
	exec := newActionExecutor(&fakeTmuxClient{}, Config{Target: "tmp-gemini", SubmitKey: "C-m"}, "gemini")
	if got := exec.Provider(); got != "gemini" {
		t.Fatalf("Provider()=%q want gemini", got)
	}
}

func TestProviderActionExecutorSendCursorChoiceMovesAndSubmits(t *testing.T) {
	client := &fakeTmuxClient{ansi: "state-0"}
	state := 0
	client.onSend = func(f *fakeTmuxClient, keys []string) {
		if len(keys) != 1 {
			return
		}
		switch keys[0] {
		case "Down":
			state++
			f.ansi = "state-down"
		case "Up":
			state++
			f.ansi = "state-up"
		}
	}

	exec := newActionExecutor(client, Config{
		Target:       "tmp-codex",
		SubmitKey:    "C-m",
		CaptureLines: 80,
	}, "codex")

	if err := exec.SendCursorChoice(context.Background(), cursorChoiceRequest{Choice: "1"}); err != nil {
		t.Fatalf("SendCursorChoice error = %v", err)
	}
	if state != 2 {
		t.Fatalf("cursor movement count = %d, want 2", state)
	}
	if len(client.sendKeys) < 3 {
		t.Fatalf("sendKeys calls = %d, want at least 3", len(client.sendKeys))
	}
	last := client.sendKeys[len(client.sendKeys)-1]
	if len(last) != 1 || last[0] != "C-m" {
		t.Fatalf("last sendKeys = %v, want [C-m]", last)
	}
}

func TestIsPromptLineGrowthDetectsBlankPromptExpansion(t *testing.T) {
	before := strings.Join([]string{
		"review summary line",
		"› Run /review on my current changes",
		"",
	}, "\n")
	after := strings.Join([]string{
		"review summary line",
		"› Run /review on my current changes",
		"",
		"",
	}, "\n")
	if !isPromptLineGrowth(before, after) {
		t.Fatalf("isPromptLineGrowth=false want true")
	}
}

func TestProviderActionExecutorSendChoiceFailsOnPromptLineGrowth(t *testing.T) {
	client := &fakeTmuxClient{
		ansi: strings.Join([]string{
			"review summary line",
			"› Run /review on my current changes",
		}, "\n"),
	}
	client.onSend = func(f *fakeTmuxClient, keys []string) {
		if len(keys) == 1 && keys[0] == "C-m" {
			f.ansi = f.ansi + "\n"
		}
	}

	exec := newActionExecutor(client, Config{
		Target:            "tmp-codex",
		SubmitKey:         "C-m",
		SubmitKeyFallback: "",
		CaptureLines:      80,
	}, "codex")

	err := exec.SendChoice(context.Background(), choiceRequest{Choice: "3"})
	if err == nil {
		t.Fatal("SendChoice error=nil want prompt growth failure")
	}
	if !strings.Contains(err.Error(), "prompt-line growth") {
		t.Fatalf("error=%q want prompt-line growth marker", err)
	}
}

func TestIsPromptClearedWithoutMenuTransitionDetectsClearedPrompt(t *testing.T) {
	before := strings.Join([]string{
		"3. Low: ignored filesystem error in test setup",
		"details line",
		"› Run /review on my current changes",
	}, "\n")
	after := strings.Join([]string{
		"3. Low: ignored filesystem error in test setup",
		"details line",
		"›",
	}, "\n")
	if !isPromptClearedWithoutMenuTransition(before, after) {
		t.Fatalf("isPromptClearedWithoutMenuTransition=false want true")
	}
}

func TestIsPromptClearedWithoutMenuTransitionIgnoresRealMenuContext(t *testing.T) {
	before := strings.Join([]string{
		"Do you want to proceed?",
		"1. Yes",
		"2. No",
		"› 1. Yes",
	}, "\n")
	after := strings.Join([]string{
		"Do you want to proceed?",
		"1. Yes",
		"2. No",
		"›",
	}, "\n")
	if isPromptClearedWithoutMenuTransition(before, after) {
		t.Fatalf("isPromptClearedWithoutMenuTransition=true want false")
	}
}

func TestFooterKeyToTmux(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ctrl+s", "C-s"},
		{"ctrl+q", "C-q"},
		{"alt+x", "M-x"},
		{"shift+tab", ""},
		{"unknown+a", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := footerKeyToTmux(tt.input)
		if got != tt.want {
			t.Errorf("footerKeyToTmux(%q)=%q want %q", tt.input, got, tt.want)
		}
	}
}

func TestFooterSubmitKeyCandidate(t *testing.T) {
	p := &executorProfile{}
	p.ApplyFooterKeyHints(map[string]string{
		"ctrl+s":    "run command",
		"shift+tab": "switch mode",
	})
	got := p.FooterSubmitKeyCandidate()
	if got != "C-s" {
		t.Fatalf("FooterSubmitKeyCandidate()=%q want C-s", got)
	}
}

func TestFooterSubmitKeyCandidateEmpty(t *testing.T) {
	p := &executorProfile{}
	p.ApplyFooterKeyHints(map[string]string{
		"shift+tab": "switch mode",
	})
	got := p.FooterSubmitKeyCandidate()
	if got != "" {
		t.Fatalf("FooterSubmitKeyCandidate()=%q want empty", got)
	}
}
