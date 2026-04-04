package devprofile

import "github.com/dh-kam/yollo/internal/tui"

// QuestionBank returns the default interview questions ordered by priority.
func QuestionBank() []tui.Question {
	return []tui.Question{
		// Level 1: Basic preferences
		{
			ID:       "error_handling",
			Category: "코딩 스타일",
			Text:     "에러가 발생했을 때 선호하는 처리 방식은?",
			Type:     tui.QuestionMultipleChoice,
			Options: []string{
				"가능한 모든 에러를 명시적으로 처리 (defensive)",
				"치명적이면 바로 패닉/크래시 (let-it-crash)",
				"Result/Either 타입으로 전파 (result-type)",
			},
		},
		{
			ID:       "testing_approach",
			Category: "코딩 스타일",
			Text:     "테스트는 보통 언제 작성하시나요?",
			Type:     tui.QuestionMultipleChoice,
			Options: []string{
				"코드 전에 (TDD)",
				"기능 완성 후",
				"통합 테스트 위주로",
			},
		},
		{
			ID:       "naming_style",
			Category: "코딩 스타일",
			Text:     "변수/함수 네이밍 스타일 선호는?",
			Type:     tui.QuestionMultipleChoice,
			Options: []string{
				"길고 설명적인 이름 (verbose)",
				"짧고 간결한 이름 (concise)",
				"도메인 용어 기반 (domain-driven)",
			},
		},
		{
			ID:       "risk_tolerance",
			Category: "의사결정 패턴",
			Text:     "새 기능 구현 시 리스크 허용도는?",
			Type:     tui.QuestionMultipleChoice,
			Options: []string{
				"보수적 — 검증 후 진행 (conservative)",
				"적당히 — 핵심만 검증 (moderate)",
				"과감하게 — 빠른 반복 (aggressive)",
			},
		},
		{
			ID:       "refactor_timing",
			Category: "워크플로우",
			Text:     "리팩토링은 언제 하시나요?",
			Type:     tui.QuestionMultipleChoice,
			Options: []string{
				"코딩하면서 계속 (continuous)",
				"기능 완성 후 따로 (after-feature)",
				"정기적으로 시간 잡아서 (dedicated)",
			},
		},
		{
			ID:       "feature_scope",
			Category: "의사결정 패턴",
			Text:     "새 기능의 초기 구현 범위는?",
			Type:     tui.QuestionMultipleChoice,
			Options: []string{
				"최소 동작부터 (MVP-first)",
				"전체 기능 한번에 (complete)",
				"점진적으로 확장 (iterative)",
			},
		},
		{
			ID:       "arch_style",
			Category: "아키텍처 선호",
			Text:     "선호하는 아키텍처 스타일은?",
			Type:     tui.QuestionMultipleChoice,
			Options: []string{
				"Clean Architecture (계층 분리)",
				"실용적 (pragmatic — 필요할 때만)",
				"DDD (도메인 주도 설계)",
			},
		},
		{
			ID:       "dep_policy",
			Category: "아키텍처 선호",
			Text:     "외부 의존성(라이브러리) 정책은?",
			Type:     tui.QuestionMultipleChoice,
			Options: []string{
				"최소화 — 직접 구현 선호 (minimal)",
				"표준 라이브러리 우선 (standard-lib-prefer)",
				"최적의 도구 사용 (best-tool)",
			},
		},
		{
			ID:       "commit_style",
			Category: "워크플로우",
			Text:     "커밋 단위는?",
			Type:     tui.QuestionMultipleChoice,
			Options: []string{
				"작은 원자적 단위 (atomic)",
				"기능 단위 (feature)",
				"작업 중 임시 (WIP)",
			},
		},
		{
			ID:       "pr_style",
			Category: "워크플로우",
			Text:     "PR 스타일 선호는?",
			Type:     tui.QuestionMultipleChoice,
			Options: []string{
				"작고 집중된 PR (small-focused)",
				"관련 변경 묶어서 (bundled)",
				"trunk-based (직접 머지)",
			},
		},
		// Level 2: Free-text deeper questions
		{
			ID:       "perf_vs_readability",
			Category: "의사결정 패턴",
			Text:     "성능과 가독성이 충돌할 때 어떤 기준으로 결정하시나요?",
			Type:     tui.QuestionFreeText,
		},
		{
			ID:       "code_quality_priority",
			Category: "코딩 스타일",
			Text:     "코드 품질에서 가장 중요하게 생각하는 것은? (자유 답변)",
			Type:     tui.QuestionFreeText,
		},
		{
			ID:       "concurrency_pattern",
			Category: "아키텍처 선호",
			Text:     "동시성 처리에서 선호하는 패턴은? (goroutine/channel/mutex 등)",
			Type:     tui.QuestionFreeText,
		},
	}
}

// InterviewScheduler picks the next unanswered question.
type InterviewScheduler struct {
	questions []tui.Question
	answered  map[string]bool
}

// NewInterviewScheduler creates a scheduler with the default question bank.
func NewInterviewScheduler() *InterviewScheduler {
	return &InterviewScheduler{
		questions: QuestionBank(),
		answered:  make(map[string]bool),
	}
}

// MarkAnswered records that a question has been answered.
func (s *InterviewScheduler) MarkAnswered(questionID string) {
	s.answered[questionID] = true
}

// NextQuestion returns the next unanswered question, or nil if all are done.
func (s *InterviewScheduler) NextQuestion() *tui.Question {
	for i := range s.questions {
		if !s.answered[s.questions[i].ID] {
			q := s.questions[i]
			return &q
		}
	}
	return nil
}

// RemainingCount returns the number of unanswered questions.
func (s *InterviewScheduler) RemainingCount() int {
	count := 0
	for _, q := range s.questions {
		if !s.answered[q.ID] {
			count++
		}
	}
	return count
}
