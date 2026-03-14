package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dh-kam/tmux-llm-yolo/internal/tmux"
)

type actionExecutor interface {
	Provider() string
	SendContinue(context.Context, continueRequest) error
	SendChoice(context.Context, choiceRequest) error
	SendCursorChoice(context.Context, cursorChoiceRequest) error
	SendInput(context.Context, inputRequest) error
	SubmitPending(context.Context, submitRequest) error
	ClearPrompt(context.Context, clearPromptRequest) error
}

type continueRequest struct {
	Message string
}

type choiceRequest struct {
	Choice string
}

type cursorChoiceRequest struct {
	Choice string
}

type inputRequest struct {
	Input string
}

type submitRequest struct{}

type clearPromptRequest struct{}

type executorProfile struct {
	provider          string
	target            string
	submitKey         string
	fallbackSubmitKey string
	fallbackDelay     float64
	clearBeforeTyping bool
	captureLines      int
	cursorProbeDelay  time.Duration
}

type providerActionExecutor struct {
	client  tmux.API
	profile executorProfile
}

func newActionExecutor(client tmux.API, cfg Config, provider string) actionExecutor {
	switch normalizeRuntimeProvider(provider) {
	case "copilot":
		return newCopilotExecutor(client, cfg)
	case "codex":
		return newCodexExecutor(client, cfg)
	case "gemini":
		return newGeminiExecutor(client, cfg)
	case "glm":
		return newGLMExecutor(client, cfg)
	default:
		return newDefaultExecutor(client, cfg, provider)
	}
}

func newDefaultExecutor(client tmux.API, cfg Config, provider string) actionExecutor {
	return providerActionExecutor{client: client, profile: resolveExecutorProfile(cfg, provider)}
}

func newCodexExecutor(client tmux.API, cfg Config) actionExecutor {
	return providerActionExecutor{client: client, profile: resolveExecutorProfile(cfg, "codex")}
}

func newCopilotExecutor(client tmux.API, cfg Config) actionExecutor {
	return providerActionExecutor{client: client, profile: resolveExecutorProfile(cfg, "copilot")}
}

func newGeminiExecutor(client tmux.API, cfg Config) actionExecutor {
	return providerActionExecutor{client: client, profile: resolveExecutorProfile(cfg, "gemini")}
}

func newGLMExecutor(client tmux.API, cfg Config) actionExecutor {
	return providerActionExecutor{client: client, profile: resolveExecutorProfile(cfg, "glm")}
}

func resolveExecutorProfile(cfg Config, provider string) executorProfile {
	profile := executorProfile{
		provider:          normalizeRuntimeProvider(provider),
		target:            cfg.Target,
		submitKey:         strings.TrimSpace(cfg.SubmitKey),
		fallbackSubmitKey: strings.TrimSpace(cfg.SubmitKeyFallback),
		fallbackDelay:     cfg.SubmitFallbackDelay,
		captureLines:      cfg.CaptureLines,
		cursorProbeDelay:  cursorProbeDelay,
	}
	if profile.captureLines <= 0 {
		profile.captureLines = 40
	}
	if profile.cursorProbeDelay <= 0 {
		profile.cursorProbeDelay = cursorProbeDelay
	}
	switch profile.provider {
	case "codex":
		profile.clearBeforeTyping = true
	case "copilot":
		profile.clearBeforeTyping = true
		if strings.EqualFold(profile.submitKey, "C-m") {
			profile.submitKey = "C-s"
		}
		profile.fallbackSubmitKey = ""
	}
	if profile.submitKey == "" {
		profile.submitKey = "C-m"
	}
	return profile
}

func (e providerActionExecutor) Provider() string {
	return e.profile.provider
}

func (e providerActionExecutor) SendContinue(ctx context.Context, req continueRequest) error {
	return sendContinueMessage(ctx, e.client, e.profile.target, req.Message, e.profile.submitKey, e.profile.fallbackSubmitKey, e.profile.fallbackDelay, e.profile.clearBeforeTyping)
}

func (e providerActionExecutor) SendChoice(ctx context.Context, req choiceRequest) error {
	return sendChoiceMessage(ctx, e.client, e.profile.target, req.Choice, e.profile.submitKey, e.profile.fallbackSubmitKey, e.profile.fallbackDelay, e.profile.clearBeforeTyping)
}

func (e providerActionExecutor) SendCursorChoice(ctx context.Context, req cursorChoiceRequest) error {
	targetChoice := promptNumericChoice(req.Choice)
	targetIndex := 1
	if parsed, err := strconv.Atoi(targetChoice); err == nil {
		targetIndex = parsed
	}
	if targetIndex < 1 {
		targetIndex = 1
	}
	beforeANSI, err := e.client.CapturePane(ctx, e.profile.target, e.profile.captureLines, true)
	if err != nil {
		return fmt.Errorf("cursor probe capture before move failed: %w", err)
	}
	steps := targetIndex - 1
	for i := 0; i < steps; i++ {
		if err := e.client.SendKeys(ctx, e.profile.target, "Down"); err != nil {
			return err
		}
		if err := waitForDuration(ctx, e.profile.cursorProbeDelay); err != nil {
			return err
		}
	}
	afterANSI, err := e.client.CapturePane(ctx, e.profile.target, e.profile.captureLines, true)
	if err != nil {
		return fmt.Errorf("cursor probe capture after move failed: %w", err)
	}
	if targetIndex > 1 && afterANSI == beforeANSI {
		return fmt.Errorf("cursor probe produced no ANSI change for choice %d", targetIndex)
	}
	if targetIndex == 1 {
		if err := e.client.SendKeys(ctx, e.profile.target, "Down"); err != nil {
			return err
		}
		if err := waitForDuration(ctx, e.profile.cursorProbeDelay); err != nil {
			return err
		}
		probeANSI, err := e.client.CapturePane(ctx, e.profile.target, e.profile.captureLines, true)
		if err != nil {
			return fmt.Errorf("cursor probe capture after move failed: %w", err)
		}
		if probeANSI == beforeANSI {
			return fmt.Errorf("cursor probe produced no ANSI change for choice %d", targetIndex)
		}
		if err := e.client.SendKeys(ctx, e.profile.target, "Up"); err != nil {
			return err
		}
		if err := waitForDuration(ctx, e.profile.cursorProbeDelay); err != nil {
			return err
		}
	}
	return sendChoiceMessage(ctx, e.client, e.profile.target, "Enter", e.profile.submitKey, e.profile.fallbackSubmitKey, e.profile.fallbackDelay, e.profile.clearBeforeTyping)
}

func (e providerActionExecutor) SendInput(ctx context.Context, req inputRequest) error {
	return sendInputMessage(ctx, e.client, e.profile.target, req.Input, e.profile.submitKey, e.profile.fallbackSubmitKey, e.profile.fallbackDelay, e.profile.clearBeforeTyping)
}

func (e providerActionExecutor) SubmitPending(ctx context.Context, req submitRequest) error {
	return sendSubmitOnly(ctx, e.client, e.profile.target, e.profile.submitKey, e.profile.fallbackSubmitKey, e.profile.fallbackDelay)
}

func (e providerActionExecutor) ClearPrompt(ctx context.Context, req clearPromptRequest) error {
	return clearPromptState(ctx, e.client, e.profile.target)
}

func promptNumericChoice(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "1"
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return "1"
		}
	}
	return value
}

func normalizeRuntimeProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "codex", "gemini", "glm", "copilot":
		return strings.ToLower(strings.TrimSpace(provider))
	default:
		return ""
	}
}
