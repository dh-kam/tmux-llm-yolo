package runtime

import (
	"strconv"
	"strings"

	"github.com/dh-kam/tmux-llm-yolo/internal/i18n"
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
	return newContinueStrategyWithPolicy(policy.Default(), baseFallback, i18n.DefaultAppLocale)
}

func newContinueStrategyWithPolicy(active policy.Policy, baseFallback string, locale string) continueStrategy {
	baseFallback = strings.TrimSpace(baseFallback)
	if active == nil {
		active = policy.Default()
	}
	spec := active.Continuation()
	loc := i18n.NormalizeLocale(locale)
	if basePrompts := localizedPromptList(loc, "policy.base."+strings.TrimSpace(active.Name())); len(basePrompts) > 0 {
		spec.BasePrompts = basePrompts
	}
	if auditPrompts := localizedPromptList(loc, "policy.audit."+strings.TrimSpace(active.Name())); len(auditPrompts) > 0 {
		spec.AuditPrompts = auditPrompts
	}
	fallbackKey := "policy.fallback." + strings.TrimSpace(active.Name())
	overrideFallback := strings.TrimSpace(i18n.T(loc, fallbackKey))
	if overrideFallback != "" && overrideFallback != fallbackKey {
		spec.FallbackMessage = overrideFallback
	}
	if strings.TrimSpace(spec.FallbackMessage) == "" {
		spec.FallbackMessage = i18n.T(loc, "policy.fallback.default")
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

func localizedPromptList(locale string, keyPrefix string) []string {
	result := make([]string, 0, 8)
	for i := 1; i <= 32; i++ {
		key := keyPrefix + "." + strconv.Itoa(i)
		value := strings.TrimSpace(i18n.T(locale, key))
		if value == "" || value == key {
			break
		}
		result = append(result, value)
	}
	return result
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
