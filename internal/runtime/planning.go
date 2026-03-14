package runtime

import (
	"strings"

	"github.com/dh-kam/tmux-llm-yolo/internal/interaction"
	"github.com/dh-kam/tmux-llm-yolo/internal/prompt"
)

func deterministicRequirementFromAnalysis(analysis prompt.Analysis) (interaction.Requirement, bool) {
	requirement := interaction.Requirement{
		Prompt:         strings.TrimSpace(analysis.PromptText),
		SuggestedValue: strings.TrimSpace(analysis.RecommendedChoice),
		Reason:         strings.TrimSpace(analysis.Reason),
	}

	switch analysis.Classification {
	case prompt.ClassContinueAfterDone:
		requirement.Kind = interaction.KindContinue
		requirement.Context = strings.TrimSpace(analysis.OutputBlock)
		return requirement, true
	case prompt.ClassNumberedMultipleChoice:
		requirement.Kind = interaction.KindNumberedChoice
		requirement.Context = strings.TrimSpace(strings.Join([]string{analysis.OutputBlock, analysis.PromptText}, "\n"))
		return requirement, true
	case prompt.ClassCursorBasedChoice:
		requirement.Kind = interaction.KindCursorChoice
		requirement.Context = strings.TrimSpace(strings.Join([]string{analysis.OutputBlock, analysis.PromptText}, "\n"))
		return requirement, true
	case prompt.ClassCompletedNoOp:
		requirement.Kind = interaction.KindContinue
		requirement.Context = strings.TrimSpace(analysis.OutputBlock)
		return requirement, true
	case prompt.ClassFreeTextRequest:
		message := continueMessageFromPlannedItems(analysis)
		if strings.TrimSpace(message) != "" {
			requirement.Kind = interaction.KindPlannedText
			requirement.SuggestedValue = message
			requirement.Context = strings.TrimSpace(analysis.OutputBlock)
			return requirement, true
		}
		return interaction.Requirement{}, false
	default:
		return interaction.Requirement{}, false
	}
}

func deterministicActionPlan(analysis prompt.Analysis, fallbackReason string) (interaction.ActionPlan, bool) {
	requirement, ok := deterministicRequirementFromAnalysis(analysis)
	if !ok {
		return interaction.ActionPlan{}, false
	}

	plan := interaction.ActionPlan{
		Requirement: requirement,
		Reason:      fallbackReason,
	}
	if strings.TrimSpace(requirement.Reason) != "" {
		plan.Reason = requirement.Reason
	}

	switch requirement.Kind {
	case interaction.KindContinue:
		plan.Steps = append(plan.Steps, interaction.ActionStep{
			Kind:   interaction.ActionContinue,
			Reason: plan.Reason,
		})
	case interaction.KindNumberedChoice:
		value := prompt.ParseNumericChoice(requirement.SuggestedValue)
		if value == "" {
			value = "1"
		}
		plan.Steps = append(plan.Steps, interaction.ActionStep{
			Kind:   interaction.ActionChoice,
			Value:  value,
			Reason: plan.Reason,
		})
	case interaction.KindCursorChoice:
		value := prompt.ParseNumericChoice(requirement.SuggestedValue)
		if value == "" {
			value = "1"
		}
		plan.Steps = append(plan.Steps, interaction.ActionStep{
			Kind:   interaction.ActionCursorChoice,
			Value:  value,
			Reason: plan.Reason,
		})
	case interaction.KindPlannedText, interaction.KindFreeText:
		if strings.TrimSpace(requirement.SuggestedValue) != "" {
			plan.Steps = append(plan.Steps, interaction.ActionStep{
				Kind:   interaction.ActionInputText,
				Value:  requirement.SuggestedValue,
				Reason: plan.Reason,
			})
			return plan, true
		}
		plan.Steps = append(plan.Steps, interaction.ActionStep{
			Kind:   interaction.ActionContinue,
			Reason: plan.Reason,
		})
	default:
		return interaction.ActionPlan{}, false
	}

	return plan, len(plan.Steps) > 0
}

func (r *Runner) executeActionPlan(plan interaction.ActionPlan, once bool) error {
	if len(plan.Steps) == 0 {
		return nil
	}
	step := plan.Steps[0]
	switch step.Kind {
	case interaction.ActionContinue:
		if once {
			return r.injectContinueOnce(step.Reason)
		}
		return r.injectContinue(step.Reason)
	case interaction.ActionInputText:
		if strings.TrimSpace(step.Value) != "" {
			r.maybeSetContinueOverride(step.Value, "planned-input")
			if once {
				return r.injectContinueOnce(step.Reason)
			}
			return r.injectContinue(step.Reason)
		}
		if once {
			return r.injectContinueOnce(step.Reason)
		}
		return r.injectContinue(step.Reason)
	case interaction.ActionChoice:
		if once {
			return r.injectChoiceOnce(step.Value, step.Reason)
		}
		return r.injectChoice(step.Value, step.Reason)
	case interaction.ActionCursorChoice:
		if once {
			return r.injectCursorChoiceOnce(step.Value, step.Reason)
		}
		return r.injectCursorChoice(step.Value, step.Reason)
	default:
		if once {
			return r.injectContinueOnce(step.Reason)
		}
		return r.injectContinue(step.Reason)
	}
}
