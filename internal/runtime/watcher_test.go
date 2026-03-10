package runtime

import (
	"strings"
	"testing"
	"time"

	"github.com/dh-kam/tmux-llm-yolo/internal/prompt"
)

func TestLLMStatusLineIncludesPrimaryFallbackAndActive(t *testing.T) {
	r := &Runner{
		cfg: Config{
			LLMName:          "codex",
			LLMModel:         "gpt-5",
			FallbackLLMName:  "gemini",
			FallbackLLMModel: "gemini-3",
		},
		primaryInitDone:  true,
		fallbackInitDone: true,
		lastLLMProvider:  "fallback:gemini",
	}

	got := r.llmStatusLine()
	wantParts := []string{
		"primary=codex/gpt-5:ready",
		"fallback=gemini/gemini-3:ready",
		"active=fallback:gemini",
	}
	for _, part := range wantParts {
		if !strings.Contains(got, part) {
			t.Fatalf("status %q missing part %q", got, part)
		}
	}
}

func TestLLMStatusLineShowsFailure(t *testing.T) {
	r := &Runner{
		cfg: Config{
			LLMName: "codex",
		},
		primaryInitDone: true,
		primaryInitErr:  errStop,
	}
	got := r.llmStatusLine()
	if !strings.Contains(got, "primary=codex:failed") {
		t.Fatalf("status %q missing failed primary state", got)
	}
}

func TestScopeLineIncludesSessionModeAndCapture(t *testing.T) {
	r := &Runner{
		cfg: Config{
			Target:       "tmp-codex",
			CaptureLines: 40,
			Once:         true,
		},
	}

	got := r.scopeLine()
	wantParts := []string{
		"session=tmp-codex",
		"mode=once",
		"capture=40",
	}
	for _, part := range wantParts {
		if !strings.Contains(got, part) {
			t.Fatalf("scope %q missing part %q", got, part)
		}
	}
}

func TestPolicyLineIncludesWaitContinueAndFallbackPlan(t *testing.T) {
	r := &Runner{
		cfg: Config{
			BaseInterval:     60 * time.Second,
			FallbackLLMName:  "gemini",
			FallbackLLMModel: "gemini-3",
		},
		continuePlan:      newContinueStrategy("fallback"),
		continueSentCount: 7,
	}

	got := r.policyLine()
	wantParts := []string{
		"wait=1m0s->5s->5s->5s",
		"continue=7 sent,next-audit=13",
		"llm=primary->fallback",
	}
	for _, part := range wantParts {
		if !strings.Contains(got, part) {
			t.Fatalf("policy %q missing part %q", got, part)
		}
	}
}

func TestCopilotSubmitKeyDefaultsToCtrlS(t *testing.T) {
	r := &Runner{
		cfg: Config{
			Target:            "tmp-copilot",
			SubmitKey:         "C-m",
			SubmitKeyFallback: "C-m",
		},
	}

	if got := r.submitKey(); got != "C-s" {
		t.Fatalf("submitKey=%q want C-s", got)
	}
	if got := r.submitKeyFallback(); got != "" {
		t.Fatalf("submitKeyFallback=%q want empty", got)
	}
}

func TestCopilotSlashPendingInputRequiresReplace(t *testing.T) {
	r := &Runner{}
	analysis := prompt.Analysis{
		Provider:     "copilot",
		PromptText:   "❯ /계속 진행하되 중간에 막히는 지점이 있으면 스스로 가설을 세우고 검증까지 이어서 진행해보자.ss",
		PromptActive: true,
	}

	if !r.hasPendingPromptInput(analysis) {
		t.Fatalf("hasPendingPromptInput=false want true")
	}
	if !r.shouldReplacePendingPromptInput(analysis) {
		t.Fatalf("shouldReplacePendingPromptInput=false want true")
	}
}

func TestCopilotSlashCommandStateDetectedFromPromptAndMenu(t *testing.T) {
	r := &Runner{}

	if !r.hasCopilotSlashCommandState(prompt.Analysis{
		Provider:     "copilot",
		PromptText:   "❯ /add-dir",
		PromptActive: true,
	}) {
		t.Fatalf("slash command prompt should be detected")
	}

	if !r.hasCopilotSlashCommandState(prompt.Analysis{
		Provider:   "copilot",
		OutputBlock: "▋ /add-dir <directory>\n▋ /agent",
	}) {
		t.Fatalf("slash command menu should be detected")
	}
}

func TestCodexPlaceholderPromptIsNotPendingInput(t *testing.T) {
	r := &Runner{}
	analysis := prompt.Analysis{
		Provider:          "codex",
		PromptText:        "› Find and fix a bug in @filename",
		PromptActive:      true,
		PromptPlaceholder: true,
	}

	if r.hasPendingPromptInput(analysis) {
		t.Fatalf("hasPendingPromptInput=true want false")
	}
}
