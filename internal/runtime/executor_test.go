package runtime

import (
	"context"
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
	if profile.submitKey != "C-s" {
		t.Fatalf("submitKey=%q want C-s", profile.submitKey)
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
