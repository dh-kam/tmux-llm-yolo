package policy

import "strings"

const DefaultAuditEvery = 20

type ContinuationSpec struct {
	BasePrompts     []string
	AuditPrompts    []string
	AuditEvery      int
	FallbackMessage string
}

type DecisionSpec struct {
	PreferDeterministic    bool
	AggressiveContinuation bool
	StrictCompletion       bool
}

type ValidationSpec struct {
	RequireBuildChecks        bool
	RequireUnitTests          bool
	RequireIntegrationTests   bool
	RequireTODOScan           bool
	RequireProfiling          bool
	RequireArchitectureReview bool
}

type QualitySpec struct {
	EmphasizeArchitecture bool
	EmphasizePerformance  bool
	EmphasizeParity       bool
	EmphasizeCreativity   bool
	EmphasizeModularity   bool
	EmphasizeReadability  bool
}

type Policy interface {
	Name() string
	Description() string
	Continuation() ContinuationSpec
	Decision() DecisionSpec
	Validation() ValidationSpec
	Quality() QualitySpec
}

type Static struct {
	name         string
	description  string
	continuation ContinuationSpec
	decision     DecisionSpec
	validation   ValidationSpec
	quality      QualitySpec
}

func (p Static) Name() string {
	return p.name
}

func (p Static) Description() string {
	return p.description
}

func (p Static) Continuation() ContinuationSpec {
	spec := p.continuation
	spec.BasePrompts = append([]string(nil), spec.BasePrompts...)
	spec.AuditPrompts = append([]string(nil), spec.AuditPrompts...)
	return spec
}

func (p Static) Decision() DecisionSpec {
	return p.decision
}

func (p Static) Validation() ValidationSpec {
	return p.validation
}

func (p Static) Quality() QualitySpec {
	return p.quality
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
