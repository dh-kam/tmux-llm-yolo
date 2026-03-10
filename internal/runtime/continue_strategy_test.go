package runtime

import "testing"

func TestContinueStrategyUsesAuditPromptEvery20thContinue(t *testing.T) {
	strategy := newContinueStrategy("fallback")

	if got := strategy.messageFor(1); got == "fallback" {
		t.Fatalf("count 1 should use rotating continue prompt, got fallback")
	}
	if got := strategy.messageFor(20); got != auditContinuePrompts[0] {
		t.Fatalf("count 20 = %q, want %q", got, auditContinuePrompts[0])
	}
	if got := strategy.messageFor(40); got != auditContinuePrompts[1] {
		t.Fatalf("count 40 = %q, want %q", got, auditContinuePrompts[1])
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
		0:  20,
		1:  19,
		19: 1,
		20: 20,
		21: 19,
	}
	for count, want := range cases {
		if got := strategy.nextAuditIn(count); got != want {
			t.Fatalf("count %d nextAuditIn=%d want %d", count, got, want)
		}
	}
}
