package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/dh-kam/tmux-llm-yolo/internal/interaction"
	"github.com/dh-kam/tmux-llm-yolo/internal/prompt"
)

func TestDeterministicRequirementFromAnalysisBuildsCursorChoice(t *testing.T) {
	analysis := prompt.Analysis{
		Classification:    prompt.ClassCursorBasedChoice,
		PromptText:        "❯ 1. Yes\n2. Yes, allow reading during this session",
		OutputBlock:       "Read file\nDo you want to proceed?",
		RecommendedChoice: "2",
		Reason:            "승인 프롬프트의 커서 선택 메뉴가 감지됨",
	}

	requirement, ok := deterministicRequirementFromAnalysis(analysis)
	if !ok {
		t.Fatal("deterministicRequirementFromAnalysis returned ok=false")
	}
	if requirement.Kind != interaction.KindCursorChoice {
		t.Fatalf("Kind=%q want %q", requirement.Kind, interaction.KindCursorChoice)
	}
	if requirement.SuggestedValue != "2" {
		t.Fatalf("SuggestedValue=%q want 2", requirement.SuggestedValue)
	}
}

func TestDeterministicActionPlanBuildsPlannedTextInput(t *testing.T) {
	analysis := prompt.Analysis{
		Classification: prompt.ClassFreeTextRequest,
		PromptText:     "›",
		PromptActive:   true,
		OutputBlock: strings.Join([]string{
			"1. parser.go에서 normalizeKotlinSource 공통화",
			"2. 테스트 보강",
		}, "\n"),
	}

	plan, ok := deterministicActionPlan(analysis, "fallback")
	if !ok {
		t.Fatal("deterministicActionPlan returned ok=false")
	}
	if len(plan.Steps) != 1 {
		t.Fatalf("len(Steps)=%d want 1", len(plan.Steps))
	}
	if plan.Steps[0].Kind != interaction.ActionInputText {
		t.Fatalf("Kind=%q want %q", plan.Steps[0].Kind, interaction.ActionInputText)
	}
	if !strings.Contains(plan.Steps[0].Value, "남은 항목 1번") {
		t.Fatalf("Value=%q missing planned item guidance", plan.Steps[0].Value)
	}
}

func TestExecuteActionPlanUsesContinueOverrideForInputText(t *testing.T) {
	client := &fakeTmuxClient{}
	r := &Runner{
		cfg: Config{
			Target:          "tmp-codex",
			SubmitKey:       "C-m",
			ContinueMessage: "fallback continue",
		},
		client:       client,
		continuePlan: newContinueStrategy("fallback continue"),
		logger:       func(string, ...interface{}) {},
		ctx:          context.Background(),
	}

	err := r.executeActionPlan(interaction.ActionPlan{
		Reason: "planned text action",
		Steps: []interaction.ActionStep{
			{
				Kind:   interaction.ActionInputText,
				Value:  "남은 항목 1번부터 진행해보자.",
				Reason: "planned text action",
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("executeActionPlan error = %v", err)
	}
	if got := client.sendKeys[1][1]; got != "남은 항목 1번부터 진행해보자." {
		t.Fatalf("typed message=%q want override text", got)
	}
}
