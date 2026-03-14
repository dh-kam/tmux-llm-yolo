package policy

var defaultBasePrompts = []string{
	"계속 진행하되 중간에 막히는 지점이 있으면 스스로 가설을 세우고 검증까지 이어서 진행해보자.",
	"다음 작업을 이어서 진행하고, 변경 이유와 영향 범위를 짧게 정리하면서 계속 진행해보자.",
	"남은 작업을 우선순위대로 하나씩 처리하고, 끝난 항목과 남은 항목을 구분해서 계속 진행해보자.",
	"구조를 보면서 개선 포인트를 바로 적용 가능한 것부터 하나씩 처리해보자.",
	"테스트 가능한 단위부터 정리하고, 작은 검증을 끼워 넣으면서 계속 진행해보자.",
}

var defaultAuditPrompts = []string{
	"지금까지의 진행률을 점검하고 미진한 부분, 누락된 작업, 남은 리스크를 리스트업한 뒤 우선순위가 높은 것부터 계속 진행해보자.",
	"나는 clean architecture, single responsibility principle, interface-oriented programming을 선호한다. 현재 코드에서 어긋나는 부분을 찾아 개선 목록을 만들고 하나씩 진행해보자.",
	"s/w architect 관점에서 현재 구조를 분석하고 결합도, 책임 분리, 확장성, 테스트 용이성 측면의 개선 포인트를 정리한 뒤 가장 효과 큰 것부터 적용해보자.",
	"지금까지 변경된 구조를 다시 검토해서 경계가 불명확한 모듈, 과도한 책임을 가진 파일, 인터페이스 추상화가 필요한 지점을 찾아 순서대로 개선해보자.",
	"중간 점검을 하자. 이미 끝난 것과 아직 불완전한 것을 구분하고, 구현은 되었지만 검증이 약한 부분을 찾아 보강하면서 계속 진행해보자.",
}

var builtins = map[string]Policy{
	"default": Static{
		name:        "default",
		description: "현재 watcher의 기본 continue/audit 동작을 유지하는 균형형 정책",
		continuation: ContinuationSpec{
			BasePrompts:     defaultBasePrompts,
			AuditPrompts:    defaultAuditPrompts,
			AuditEvery:      DefaultAuditEvery,
			FallbackMessage: "계속 진행하되 완료까지 이어서 처리해보자.",
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
		description: "최소한의 구조 부담으로 end-to-end 동작 완료를 우선하는 정책",
		continuation: ContinuationSpec{
			BasePrompts: []string{
				"우선 end-to-end로 동작하는 POC를 완성하자. 막히면 가장 짧은 경로로 우회하고 계속 진행해보자.",
				"핵심 흐름이 실제로 동작하도록 필요한 구현만 우선 마무리하고, 나머지 정리는 뒤로 미루자.",
			},
			AuditPrompts: []string{
				"POC 기준으로 아직 막힌 핵심 경로와 남은 필수 연결 작업만 추려서 빠르게 마무리하자.",
			},
			AuditEvery:      DefaultAuditEvery,
			FallbackMessage: "우선 동작하는 POC를 완성하는 데 집중해서 계속 진행해보자.",
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
		description: "구조 개선, 책임 분리, 인터페이스 경계 정리를 강하게 밀어붙이는 정책",
		continuation: ContinuationSpec{
			BasePrompts: []string{
				"clean architecture와 SRP 관점에서 가장 큰 구조 문제부터 정리하고 바로 적용해보자.",
				"지금 코드에서 책임이 과도한 지점을 줄이고 인터페이스 경계를 분명히 하면서 계속 진행해보자.",
			},
			AuditPrompts: []string{
				"중간 점검을 하자. 레이어 경계가 흐린 부분, 테스트하기 어려운 결합부, interface-driven 분리가 필요한 지점을 우선순위대로 개선하자.",
			},
			AuditEvery:      DefaultAuditEvery,
			FallbackMessage: "구조 개선 효과가 큰 지점부터 분리하고 책임을 줄이면서 계속 진행해보자.",
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
		description: "다른 언어 구현과의 동작/출력 parity를 100%에 가깝게 맞추는 정책",
		continuation: ContinuationSpec{
			BasePrompts: []string{
				"reference 구현과의 차이를 우선순위대로 정리하고 parity를 100%에 가깝게 만들기 위해 하나씩 검증하며 진행해보자.",
				"남은 diff와 회귀 가능 지점을 구분하고, baseline과 테스트 결과를 근거로 parity를 계속 끌어올려보자.",
			},
			AuditPrompts: []string{
				"중간 점검을 하자. parity가 아직 100%가 아닌 이유를 diff, 누락 케이스, 테스트 갭, 구조적 제약으로 나눠서 정리하고 큰 차이부터 해소하자.",
			},
			AuditEvery:      DefaultAuditEvery,
			FallbackMessage: "reference 대비 parity 차이를 줄이는 방향으로 계속 진행해보자.",
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
		description: "자유 의지와 신선한 아이디어 구체화를 우선하는 정책",
		continuation: ContinuationSpec{
			BasePrompts: []string{
				"기존 구현에 갇히지 말고 신선한 아이디어를 구체적인 실험으로 바꿔가며 진행해보자.",
				"대안들을 빠르게 비교하면서 가장 흥미롭고 설득력 있는 방향을 실제 코드로 구체화해보자.",
			},
			AuditPrompts: []string{
				"지금까지 나온 아이디어를 정리하고, 더 밀어볼 가치가 있는 실험과 버려야 할 방향을 구분해 다음 시도를 구체화하자.",
			},
			AuditEvery:      DefaultAuditEvery,
			FallbackMessage: "자유롭게 아이디어를 확장하되 실제 코드와 검증으로 구체화하면서 계속 진행해보자.",
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
