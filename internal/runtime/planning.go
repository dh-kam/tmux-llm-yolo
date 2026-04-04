package runtime

import (
	"strings"

	"github.com/dh-kam/yollo/internal/interaction"
	"github.com/dh-kam/yollo/internal/prompt"
)

func deterministicRequirementFromAnalysis(analysis prompt.Analysis, locale string) (interaction.Requirement, bool) {
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
		message := continueMessageFromPlannedItems(analysis, locale)
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

func deterministicActionPlan(analysis prompt.Analysis, fallbackReason string, locale string) (interaction.ActionPlan, bool) {
	requirement, ok := deterministicRequirementFromAnalysis(analysis, locale)
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

// applyApprovalPolicy checks if the choice is for an approval prompt and
// adjusts the option based on the approval policy (first N → option 1, then → option 2).
func (r *Runner) applyApprovalPolicy(value string, context string) string {
	if r.approvalPolicy == nil {
		return value
	}
	// Only apply to approval-like contexts (contain "approve", "allow", "proceed", etc.)
	if !isApprovalContext(context) {
		return value
	}
	choice := r.approvalPolicy.Decide(context)
	r.logger("approval policy: option %d for %q (%s)", choice.Option, extractApprovalKey(context), choice.Reason)
	return itoa(choice.Option)
}

// isApprovalContext detects if the choice context is an approval/permission prompt.
func isApprovalContext(context string) bool {
	lower := strings.ToLower(context)
	markers := []string{
		"approve", "allow", "proceed", "confirm",
		"yes, and don't ask", "yes, proceed",
		"allow once", "allow for this session",
		"press enter to confirm",
		"would you like to run",
	}
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
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
		value := r.applyApprovalPolicy(step.Value, plan.Requirement.Context)
		if once {
			return r.injectChoiceOnce(value, step.Reason)
		}
		return r.injectChoice(value, step.Reason)
	case interaction.ActionCursorChoice:
		value := r.applyApprovalPolicy(step.Value, plan.Requirement.Context)
		if once {
			return r.injectCursorChoiceOnce(value, step.Reason)
		}
		return r.injectCursorChoice(value, step.Reason)
	default:
		if once {
			return r.injectContinueOnce(step.Reason)
		}
		return r.injectContinue(step.Reason)
	}
}
