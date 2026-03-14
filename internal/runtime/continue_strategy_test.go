package runtime

import (
	"testing"

	"github.com/dh-kam/tmux-llm-yolo/internal/policy"
)

func TestContinueStrategyUsesAuditPromptAtConfiguredInterval(t *testing.T) {
	strategy := newContinueStrategy("fallback")

	if got := strategy.messageFor(1); got == "fallback" {
		t.Fatalf("count 1 should use rotating continue prompt, got fallback")
	}
	if got := strategy.messageFor(auditPromptEvery); got != auditContinuePrompts[0] {
		t.Fatalf("count %d = %q, want %q", auditPromptEvery, got, auditContinuePrompts[0])
	}
	if got := strategy.messageFor(auditPromptEvery * 2); got != auditContinuePrompts[1] {
		t.Fatalf("count %d = %q, want %q", auditPromptEvery*2, got, auditContinuePrompts[1])
	}
}

func TestContinueStrategyFallsBackWhenBaseMessageEmpty(t *testing.T) {
	strategy := newContinueStrategy("")
	if got := strategy.messageFor(0); got == "" {
		t.Fatalf("count 0 should not return empty message")
	}
}

func TestContinueStrategyNextAuditIn(t *testing.T) {
	strategy := newContinueStrategy("fallback")

	cases := map[int]int{
		0:                    auditPromptEvery,
		1:                    auditPromptEvery - 1,
		auditPromptEvery - 1: 1,
		auditPromptEvery:     auditPromptEvery,
		auditPromptEvery + 1: auditPromptEvery - 1,
	}
	for count, want := range cases {
		if got := strategy.nextAuditIn(count); got != want {
			t.Fatalf("count %d nextAuditIn=%d want %d", count, got, want)
		}
	}
}

func TestContinueStrategyUsesConfiguredPolicyContinuation(t *testing.T) {
	strategy := newContinueStrategyWithPolicy(policy.Resolve("poc-completion"), "")

	if got := strategy.messageFor(1); got == "" {
		t.Fatal("messageFor(1) returned empty message")
	}
	if got := strategy.baseFallback; got != policy.Resolve("poc-completion").Continuation().FallbackMessage {
		t.Fatalf("baseFallback=%q want policy fallback %q", got, policy.Resolve("poc-completion").Continuation().FallbackMessage)
	}
}
