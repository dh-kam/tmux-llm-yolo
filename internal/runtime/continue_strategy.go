package runtime

import (
	"strings"

	"github.com/dh-kam/tmux-llm-yolo/internal/policy"
)

const auditPromptEvery = policy.DefaultAuditEvery

var defaultContinuePrompts = append([]string(nil), policy.Default().Continuation().BasePrompts...)
var auditContinuePrompts = append([]string(nil), policy.Default().Continuation().AuditPrompts...)

type continueStrategy struct {
	basePrompts  []string
	auditPrompts []string
	baseFallback string
	auditEvery   int
}

func newContinueStrategy(baseFallback string) continueStrategy {
	return newContinueStrategyWithPolicy(policy.Default(), baseFallback)
}

func newContinueStrategyWithPolicy(active policy.Policy, baseFallback string) continueStrategy {
	baseFallback = strings.TrimSpace(baseFallback)
	if active == nil {
		active = policy.Default()
	}
	spec := active.Continuation()
	if strings.TrimSpace(spec.FallbackMessage) == "" {
		spec.FallbackMessage = "계속 진행하되 완료까지 이어서 처리해보자."
	}
	if spec.AuditEvery <= 0 {
		spec.AuditEvery = auditPromptEvery
	}
	if baseFallback == "" {
		baseFallback = spec.FallbackMessage
	}
	return continueStrategy{
		basePrompts:  append([]string(nil), spec.BasePrompts...),
		auditPrompts: append([]string(nil), spec.AuditPrompts...),
		baseFallback: baseFallback,
		auditEvery:   spec.AuditEvery,
	}
}

func (s continueStrategy) messageFor(continueSentCount int) string {
	if continueSentCount <= 0 {
		return s.baseFallback
	}
	if s.auditEvery > 0 && continueSentCount%s.auditEvery == 0 && len(s.auditPrompts) > 0 {
		idx := ((continueSentCount / s.auditEvery) - 1) % len(s.auditPrompts)
		return s.auditPrompts[idx]
	}
	if len(s.basePrompts) == 0 {
		return s.baseFallback
	}
	idx := (continueSentCount - 1) % len(s.basePrompts)
	return s.basePrompts[idx]
}

func (s continueStrategy) nextAuditIn(continueSentCount int) int {
	if continueSentCount < 0 {
		continueSentCount = 0
	}
	if s.auditEvery <= 0 {
		return 0
	}
	remainder := continueSentCount % s.auditEvery
	if remainder == 0 {
		return s.auditEvery
	}
	return s.auditEvery - remainder
}
