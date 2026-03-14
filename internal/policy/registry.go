package policy

var defaultBasePrompts = []string{
	"Continue and keep moving until the remaining work is complete.",
	"Proceed with the next task, and briefly explain the reason and impact of each change as you go.",
	"Work through the remaining items in priority order, clearly distinguishing completed and pending items.",
	"Apply the most immediately actionable structural improvements first and keep going.",
	"Start from units that can be verified easily and keep progressing with small validation steps.",
}

var defaultAuditPrompts = []string{
	"Review progress so far, list missing work and risks, then continue from the highest-priority items.",
	"I prefer clean architecture, single responsibility, and interface-oriented design. Find mismatches in the current code and improve them one by one.",
	"Analyze the structure from a software architect perspective and apply the highest-impact improvements for coupling, responsibility split, extensibility, and testability.",
	"Re-check the current structure, find unclear boundaries, oversized files, or places needing interface abstraction, and improve them in order.",
	"Pause for a midpoint review: separate completed work from incomplete work, then reinforce areas that are implemented but still weakly verified.",
}

var builtins = map[string]Policy{
	"default": Static{
		name:        "default",
		description: "Balanced policy that preserves the watcher's default continue and audit behavior.",
		continuation: ContinuationSpec{
			BasePrompts:     defaultBasePrompts,
			AuditPrompts:    defaultAuditPrompts,
			AuditEvery:      DefaultAuditEvery,
			FallbackMessage: "Continue execution and stay focused until completion.",
		},
		decision: DecisionSpec{
			PreferDeterministic:    true,
			AggressiveContinuation: true,
			StrictCompletion:       false,
		},
		validation: ValidationSpec{
			RequireBuildChecks:        true,
			RequireUnitTests:          true,
			RequireIntegrationTests:   false,
			RequireTODOScan:           true,
			RequireProfiling:          false,
			RequireArchitectureReview: true,
		},
		quality: QualitySpec{
			EmphasizeArchitecture: true,
			EmphasizePerformance:  true,
			EmphasizeModularity:   true,
			EmphasizeReadability:  true,
		},
	},
	"poc-completion": Static{
		name:        "poc-completion",
		description: "Policy that prioritizes end-to-end completion with minimal structural overhead.",
		continuation: ContinuationSpec{
			BasePrompts: []string{
				"Finish a working end-to-end POC first. If blocked, take the shortest viable detour and keep going.",
				"Focus on the minimum implementation needed for the core flow to actually work, and postpone cleanup for later.",
			},
			AuditPrompts: []string{
				"For the POC, isolate only the blocked critical path and the remaining must-connect work, then close them quickly.",
			},
			AuditEvery:      DefaultAuditEvery,
			FallbackMessage: "Continue to complete the POC first and keep moving forward.",
		},
		decision: DecisionSpec{
			PreferDeterministic:    true,
			AggressiveContinuation: true,
			StrictCompletion:       false,
		},
		validation: ValidationSpec{
			RequireBuildChecks:        true,
			RequireUnitTests:          false,
			RequireIntegrationTests:   false,
			RequireTODOScan:           false,
			RequireProfiling:          false,
			RequireArchitectureReview: false,
		},
		quality: QualitySpec{
			EmphasizeReadability: true,
		},
	},
	"aggressive-architecture": Static{
		name:        "aggressive-architecture",
		description: "Policy that aggressively pushes structural cleanup, responsibility split, and interface boundaries.",
		continuation: ContinuationSpec{
			BasePrompts: []string{
				"Start with the biggest structural problem from a clean architecture and SRP perspective, then apply the fix immediately.",
				"Reduce oversized responsibilities and sharpen interface boundaries as you continue through the code.",
			},
			AuditPrompts: []string{
				"Pause and review: prioritize blurred layer boundaries, hard-to-test coupling points, and places that need interface-driven separation.",
			},
			AuditEvery:      DefaultAuditEvery,
			FallbackMessage: "Continue by improving the strongest architectural concern first.",
		},
		decision: DecisionSpec{
			PreferDeterministic:    true,
			AggressiveContinuation: true,
			StrictCompletion:       true,
		},
		validation: ValidationSpec{
			RequireBuildChecks:        true,
			RequireUnitTests:          true,
			RequireIntegrationTests:   true,
			RequireTODOScan:           true,
			RequireProfiling:          true,
			RequireArchitectureReview: true,
		},
		quality: QualitySpec{
			EmphasizeArchitecture: true,
			EmphasizePerformance:  true,
			EmphasizeModularity:   true,
			EmphasizeReadability:  true,
		},
	},
	"parity-porting": Static{
		name:        "parity-porting",
		description: "Policy that drives behavior and output parity toward 100%% against another implementation.",
		continuation: ContinuationSpec{
			BasePrompts: []string{
				"List differences from the reference implementation by priority, then verify and close them one by one toward near-100%% parity.",
				"Separate remaining diffs from regression risks, and keep improving parity using baseline behavior and test evidence.",
			},
			AuditPrompts: []string{
				"Review why parity is still below 100%% by grouping causes into diffs, missing cases, test gaps, and structural constraints, then fix the largest gaps first.",
			},
			AuditEvery:      DefaultAuditEvery,
			FallbackMessage: "Continue iteratively to reduce parity differences.",
		},
		decision: DecisionSpec{
			PreferDeterministic:    true,
			AggressiveContinuation: true,
			StrictCompletion:       true,
		},
		validation: ValidationSpec{
			RequireBuildChecks:        true,
			RequireUnitTests:          true,
			RequireIntegrationTests:   true,
			RequireTODOScan:           true,
			RequireProfiling:          true,
			RequireArchitectureReview: true,
		},
		quality: QualitySpec{
			EmphasizeArchitecture: true,
			EmphasizePerformance:  true,
			EmphasizeParity:       true,
			EmphasizeModularity:   true,
			EmphasizeReadability:  true,
		},
	},
	"creative-exploration": Static{
		name:        "creative-exploration",
		description: "Policy that prioritizes autonomy and turning fresh ideas into concrete implementations.",
		continuation: ContinuationSpec{
			BasePrompts: []string{
				"Do not stay trapped in the current implementation. Turn fresh ideas into concrete experiments and keep moving.",
				"Compare alternatives quickly and turn the most compelling direction into real code.",
			},
			AuditPrompts: []string{
				"Pause to sort the ideas so far into experiments worth pushing further and directions worth dropping, then define the next concrete attempt.",
			},
			AuditEvery:      DefaultAuditEvery,
			FallbackMessage: "Keep exploring practical ideas and continue with concrete code changes.",
		},
		decision: DecisionSpec{
			PreferDeterministic:    true,
			AggressiveContinuation: true,
			StrictCompletion:       false,
		},
		validation: ValidationSpec{
			RequireBuildChecks:        true,
			RequireUnitTests:          false,
			RequireIntegrationTests:   false,
			RequireTODOScan:           false,
			RequireProfiling:          false,
			RequireArchitectureReview: false,
		},
		quality: QualitySpec{
			EmphasizeCreativity:  true,
			EmphasizeModularity:  true,
			EmphasizeReadability: true,
		},
	},
}

func Default() Policy {
	return builtins["default"]
}

func Resolve(name string) Policy {
	normalized := normalizeName(name)
	if normalized == "" {
		return Default()
	}
	if policy, ok := builtins[normalized]; ok {
		return policy
	}
	return Default()
}

func Available() []Policy {
	names := []string{
		"default",
		"poc-completion",
		"aggressive-architecture",
		"parity-porting",
		"creative-exploration",
	}
	policies := make([]Policy, 0, len(names))
	for _, name := range names {
		policies = append(policies, builtins[name])
	}
	return policies
}
