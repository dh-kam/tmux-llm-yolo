package runtime

import (
	"fmt"
	"strings"
	"time"

	"github.com/dh-kam/yollo/internal/policy"
	"github.com/dh-kam/yollo/internal/tui"
)

func (r *Runner) nextTaskName() string {
	if len(r.queue) == 0 {
		return ""
	}
	return r.queue[0].Name() + " - " + r.queue[0].Description()
}

func (r *Runner) updateUI() {
	if r.ui == nil {
		return
	}
	currentTask := ""
	currentDesc := ""
	if r.currentTask != nil {
		currentTask = r.currentTask.Name()
		currentDesc = r.currentTask.Description()
	}
	r.ui.Update(tui.Snapshot{
		Target:      strings.TrimSpace(r.cfg.Target),
		State:       r.state,
		Mode:        r.modeName(),
		Capture:     fmt.Sprintf("%d lines", r.cfg.CaptureLines),
		WaitPlan:    r.waitPlanLine(),
		Continue:    r.continueLine(),
		Policy:      r.policyName(),
		CurrentTask: currentTask,
		CurrentDesc: currentDesc,
		NextTask:    r.nextTaskShortName(),
		NextDesc:    r.nextTaskDescription(),
		LLMPrimary:  r.providerState(r.cfg.LLMName, r.cfg.LLMModel, r.primaryInitDone, r.primaryInitErr),
		LLMFallback: r.fallbackProviderLine(),
		LLMActive:   r.activeLLMLine(),
		SleepReason: r.sleepReason,
		SleepStart:  r.sleepStarted,
		SleepUntil:  r.sleepUntil,
		Deadline:    r.deadline,
		LastEvent:   r.lastEvent,
		LastUpdated: time.Now(),
		LogLines:    r.cfg.LogBuffer.Lines(),
	})
}

func (r *Runner) scopeLine() string {
	mode := "watch"
	if r.cfg.Once {
		mode = "once"
	}
	parts := []string{
		"session=" + displayValue(strings.TrimSpace(r.cfg.Target)),
		"mode=" + mode,
		fmt.Sprintf("capture=%d", r.cfg.CaptureLines),
	}
	return strings.Join(parts, " | ")
}

func (r *Runner) modeName() string {
	if r.cfg.Once {
		return "once"
	}
	return "watch"
}

func (r *Runner) nextTaskShortName() string {
	if len(r.queue) == 0 {
		return ""
	}
	return r.queue[0].Name()
}

func (r *Runner) nextTaskDescription() string {
	if len(r.queue) == 0 {
		return ""
	}
	return r.queue[0].Description()
}

func (r *Runner) waitPlanLine() string {
	return fmt.Sprintf("%s>%s>%s>%s", r.baseInterval().Round(time.Second), r.suspectWait1().Round(time.Second), r.suspectWait2().Round(time.Second), r.suspectWait3().Round(time.Second))
}

func (r *Runner) continueLine() string {
	return fmt.Sprintf("%d sent / audit %d", r.continueSentCount, r.continuePlan.nextAuditIn(r.continueSentCount))
}

func (r *Runner) policyLine() string {
	parts := []string{
		"wait=" + r.waitPlanLine(),
		fmt.Sprintf("continue=%d sent,next-audit=%d", r.continueSentCount, r.continuePlan.nextAuditIn(r.continueSentCount)),
		"policy=" + r.policyName(),
	}
	llmPlan := "llm=primary"
	if strings.TrimSpace(r.cfg.FallbackLLMName) != "" {
		llmPlan = "llm=primary->fallback"
	}
	parts = append(parts, llmPlan)
	return strings.Join(parts, " | ")
}

func (r *Runner) policyName() string {
	if r.activePolicy != nil && strings.TrimSpace(r.activePolicy.Name()) != "" {
		return r.activePolicy.Name()
	}
	return policy.Default().Name()
}

func displayValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func (r *Runner) llmStatusLine() string {
	parts := []string{
		fmt.Sprintf("primary=%s", r.providerState(r.cfg.LLMName, r.cfg.LLMModel, r.primaryInitDone, r.primaryInitErr)),
	}
	if fallback := r.fallbackProviderLine(); fallback != "" {
		parts = append(parts, "fallback="+fallback)
	}
	if active := r.activeLLMLine(); active != "" {
		parts = append(parts, "active="+active)
	}
	return strings.Join(parts, " | ")
}

func (r *Runner) fallbackProviderLine() string {
	if strings.TrimSpace(r.cfg.FallbackLLMName) == "" {
		return ""
	}
	return r.providerState(r.cfg.FallbackLLMName, r.cfg.FallbackLLMModel, r.fallbackInitDone, r.fallbackInitErr)
}

func (r *Runner) activeLLMLine() string {
	value := strings.TrimSpace(r.lastLLMProvider)
	if value == "" {
		return ""
	}
	parts := strings.Split(value, ":")
	if len(parts) == 0 {
		return value
	}
	if len(parts) >= 2 {
		return parts[0] + ":" + parts[1]
	}
	return parts[0]
}

func (r *Runner) providerState(name string, model string, initDone bool, initErr error) string {
	label := strings.TrimSpace(name)
	if model = strings.TrimSpace(model); model != "" {
		label += "/" + model
	}
	if label == "" {
		label = "-"
	}
	switch {
	case !initDone:
		return label + ":pending"
	case initErr != nil:
		return label + ":failed"
	default:
		return label + ":ready"
	}
}
