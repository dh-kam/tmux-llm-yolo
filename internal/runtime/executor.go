package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dh-kam/yollo/internal/capture"
	"github.com/dh-kam/yollo/internal/i18n"
	"github.com/dh-kam/yollo/internal/tmux"
	"gopkg.in/yaml.v3"
)

const (
	dryRunOutputFormatPlain = "plain"
	dryRunOutputFormatJSON  = "json"
	dryRunOutputFormatYAML  = "yaml"
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
	provider           string
	target             string
	locale             string
	submitKey          string
	fallbackSubmitKey  string
	fallbackDelay      float64
	clearBeforeTyping  bool
	captureLines       int
	cursorProbeDelay   time.Duration
	dryRunOutputFormat string
	footerKeyHints     map[string]string // Dynamic key hints from footer parsing (candidates only)
}

type providerActionExecutor struct {
	client  tmux.API
	profile executorProfile
}

type dryRunActionExecutor struct {
	profile executorProfile
}

func newActionExecutor(client tmux.API, cfg Config, provider string) actionExecutor {
	if cfg.DryRun {
		return newDryRunExecutor(cfg, provider)
	}
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

func newDryRunExecutor(cfg Config, provider string) actionExecutor {
	return dryRunActionExecutor{profile: resolveExecutorProfile(cfg, provider)}
}

func resolveExecutorProfile(cfg Config, provider string) executorProfile {
	profile := executorProfile{
		provider:           normalizeRuntimeProvider(provider),
		target:             cfg.Target,
		locale:             cfg.Locale,
		submitKey:          strings.TrimSpace(cfg.SubmitKey),
		fallbackSubmitKey:  strings.TrimSpace(cfg.SubmitKeyFallback),
		fallbackDelay:      cfg.SubmitFallbackDelay,
		captureLines:       cfg.CaptureLines,
		cursorProbeDelay:   cursorProbeDelay,
		dryRunOutputFormat: normalizeDryRunOutputFormat(cfg.DryRunOutputFormat),
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
		// Copilot uses Enter (C-m) to submit; C-s opens the command palette.
		if strings.EqualFold(profile.submitKey, "C-s") {
			profile.submitKey = "C-m"
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
	beforeANSI, err := e.client.CapturePane(ctx, e.profile.target, e.profile.captureLines, true)
	if err != nil {
		return fmt.Errorf("choice pre-capture failed: %w", err)
	}
	if err := sendChoiceMessage(ctx, e.client, e.profile.target, req.Choice, e.profile.submitKey, e.profile.fallbackSubmitKey, e.profile.fallbackDelay, e.profile.clearBeforeTyping); err != nil {
		return err
	}
	afterANSI, err := e.client.CapturePane(ctx, e.profile.target, e.profile.captureLines, true)
	if err != nil {
		return fmt.Errorf("choice post-capture failed: %w", err)
	}
	// Conservative safety guard:
	// if sending a numeric choice only grows blank prompt lines while meaningful
	// content stays the same, treat it as a misclassified menu interaction.
	if isPromptLineGrowth(beforeANSI, afterANSI) {
		return fmt.Errorf("choice submission detected prompt-line growth without menu transition (provider: %s, choice: %s)", e.profile.provider, promptNumericChoice(req.Choice))
	}
	if e.profile.provider == "codex" && isPromptClearedWithoutMenuTransition(beforeANSI, afterANSI) {
		return fmt.Errorf("choice submission cleared prompt text without menu transition (provider: %s, choice: %s)", e.profile.provider, promptNumericChoice(req.Choice))
	}
	return nil
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
	// Detect prompt-growing: if cursor keys only added blank lines instead of
	// navigating a menu, the provider doesn't support cursor-based selection.
	// Fall through to the continue-message fallback via cursorChoiceFallback.
	if isPromptGrowth(beforeANSI, afterANSI) {
		return fmt.Errorf("cursor probe detected prompt growth instead of menu navigation for choice %d (provider: %s)", targetIndex, e.profile.provider)
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
		if isPromptGrowth(beforeANSI, probeANSI) {
			return fmt.Errorf("cursor probe detected prompt growth instead of menu navigation for choice %d (provider: %s)", targetIndex, e.profile.provider)
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

type dryRunActionPlan struct {
	Mode              string   `json:"mode" yaml:"mode"`
	Provider          string   `json:"provider" yaml:"provider"`
	Target            string   `json:"target" yaml:"target"`
	Action            string   `json:"action" yaml:"action"`
	Intent            string   `json:"intent" yaml:"intent"`
	Choice            string   `json:"choice,omitempty" yaml:"choice,omitempty"`
	Input             string   `json:"input,omitempty" yaml:"input,omitempty"`
	ClearBeforeTyping bool     `json:"clear_before_typing" yaml:"clear_before_typing"`
	Keys              []string `json:"keys" yaml:"keys"`
	SubmitKey         string   `json:"submit_key,omitempty" yaml:"submit_key,omitempty"`
	FallbackSubmitKey string   `json:"fallback_submit_key,omitempty" yaml:"fallback_submit_key,omitempty"`
	Notes             []string `json:"notes,omitempty" yaml:"notes,omitempty"`
}

func (e dryRunActionExecutor) Provider() string {
	return e.profile.provider
}

func (e dryRunActionExecutor) SendContinue(_ context.Context, req continueRequest) error {
	keys := submitKeyPlan(e.profile.submitKey, e.profile.fallbackSubmitKey)
	if e.profile.clearBeforeTyping {
		keys = append([]string{"C-u"}, keys...)
	}
	if strings.TrimSpace(req.Message) != "" {
		keys = append(keys, "type:message")
	}
	return e.printDryRunAction(dryRunActionPlan{
		Mode:              "dry-run",
		Provider:          e.profile.provider,
		Target:            e.profile.target,
		Action:            "continue",
		Intent:            i18n.T(e.profile.locale, "executor.intent_continue"),
		Input:             strings.TrimSpace(req.Message),
		ClearBeforeTyping: e.profile.clearBeforeTyping,
		Keys:              keys,
		SubmitKey:         e.profile.submitKey,
		FallbackSubmitKey: e.profile.fallbackSubmitKey,
	})
}

func (e dryRunActionExecutor) SendChoice(_ context.Context, req choiceRequest) error {
	choice := promptNumericChoice(req.Choice)
	keys := submitKeyPlan(e.profile.submitKey, e.profile.fallbackSubmitKey)
	if e.profile.clearBeforeTyping {
		keys = append([]string{"C-u"}, keys...)
	}
	keys = append(keys, "type:"+choice)
	return e.printDryRunAction(dryRunActionPlan{
		Mode:              "dry-run",
		Provider:          e.profile.provider,
		Target:            e.profile.target,
		Action:            "choice",
		Intent:            i18n.T(e.profile.locale, "executor.intent_choice"),
		Choice:            choice,
		ClearBeforeTyping: e.profile.clearBeforeTyping,
		Keys:              keys,
		SubmitKey:         e.profile.submitKey,
		FallbackSubmitKey: e.profile.fallbackSubmitKey,
	})
}

func (e dryRunActionExecutor) SendCursorChoice(_ context.Context, req cursorChoiceRequest) error {
	targetChoice := promptNumericChoice(req.Choice)
	targetIndex := 1
	if parsed, err := strconv.Atoi(targetChoice); err == nil {
		targetIndex = parsed
	}
	if targetIndex < 1 {
		targetIndex = 1
	}

	keys := make([]string, 0, targetIndex+2)
	if targetIndex == 1 {
		keys = append(keys, "Down", "Up")
	} else {
		for i := 0; i < targetIndex-1; i++ {
			keys = append(keys, "Down")
		}
	}
	keys = append(keys, "submit:"+resolveSubmitKey(e.profile.submitKey))

	notes := []string{}
	if targetIndex == 1 {
		notes = append(notes, i18n.T(e.profile.locale, "executor.cursor_note_probe"))
	} else {
		notes = append(notes, fmt.Sprintf(i18n.T(e.profile.locale, "executor.cursor_move"), targetIndex-1))
	}
	return e.printDryRunAction(dryRunActionPlan{
		Mode:              "dry-run",
		Provider:          e.profile.provider,
		Target:            e.profile.target,
		Action:            "cursor_choice",
		Intent:            i18n.T(e.profile.locale, "executor.intent_cursor"),
		Choice:            targetChoice,
		ClearBeforeTyping: e.profile.clearBeforeTyping,
		Keys:              keys,
		SubmitKey:         e.profile.submitKey,
		FallbackSubmitKey: e.profile.fallbackSubmitKey,
		Notes:             notes,
	})
}

func (e dryRunActionExecutor) SendInput(_ context.Context, req inputRequest) error {
	input := strings.TrimSpace(req.Input)
	if input == "" {
		return fmt.Errorf("empty input")
	}
	keys := submitKeyPlan(e.profile.submitKey, e.profile.fallbackSubmitKey)
	if e.profile.clearBeforeTyping {
		keys = append([]string{"C-u"}, keys...)
	}
	keys = append(keys, "type:input")
	return e.printDryRunAction(dryRunActionPlan{
		Mode:              "dry-run",
		Provider:          e.profile.provider,
		Target:            e.profile.target,
		Action:            "input",
		Intent:            i18n.T(e.profile.locale, "executor.intent_input"),
		Input:             input,
		ClearBeforeTyping: e.profile.clearBeforeTyping,
		Keys:              keys,
		SubmitKey:         e.profile.submitKey,
		FallbackSubmitKey: e.profile.fallbackSubmitKey,
	})
}

func (e dryRunActionExecutor) SubmitPending(_ context.Context, _ submitRequest) error {
	return e.printDryRunAction(dryRunActionPlan{
		Mode:              "dry-run",
		Provider:          e.profile.provider,
		Target:            e.profile.target,
		Action:            "submit_pending",
		Intent:            i18n.T(e.profile.locale, "executor.intent_submit_pending"),
		Keys:              submitKeyPlan(e.profile.submitKey, e.profile.fallbackSubmitKey),
		SubmitKey:         e.profile.submitKey,
		FallbackSubmitKey: e.profile.fallbackSubmitKey,
	})
}

func (e dryRunActionExecutor) ClearPrompt(_ context.Context, _ clearPromptRequest) error {
	return e.printDryRunAction(dryRunActionPlan{
		Mode:     "dry-run",
		Provider: e.profile.provider,
		Target:   e.profile.target,
		Action:   "clear_prompt",
		Intent:   i18n.T(e.profile.locale, "executor.intent_clear_prompt"),
		Keys:     []string{"Escape", "wait:120ms", "C-u"},
	})
}

func (e dryRunActionExecutor) printDryRunAction(plan dryRunActionPlan) error {
	switch e.profile.dryRunOutputFormat {
	case dryRunOutputFormatJSON:
		raw, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			return err
		}
		fmt.Printf("%s\n", string(raw))
	case dryRunOutputFormatYAML:
		raw, err := yaml.Marshal(plan)
		if err != nil {
			return err
		}
		fmt.Printf("%s\n", strings.TrimSpace(string(raw)))
	case dryRunOutputFormatPlain:
		fallthrough
	default:
		e.printDryRunActionPlain(plan)
	}
	return nil
}

func (e dryRunActionExecutor) printDryRunActionPlain(plan dryRunActionPlan) {
	fmt.Printf("[dry-run] mode=%s provider=%s target=%s action=%s\n", plan.Mode, plan.Provider, plan.Target, plan.Action)
	fmt.Printf("[dry-run] intent=%s clear_before_typing=%t submit=%s fallback=%s\n", plan.Intent, plan.ClearBeforeTyping, strings.TrimSpace(plan.SubmitKey), strings.TrimSpace(plan.FallbackSubmitKey))
	if plan.Choice != "" {
		fmt.Printf("[dry-run] choice=%s\n", plan.Choice)
	}
	if plan.Input != "" {
		fmt.Printf("[dry-run] input=%q\n", plan.Input)
	}
	if len(plan.Keys) > 0 {
		fmt.Printf("[dry-run] keys=%s\n", strings.Join(plan.Keys, ", "))
	}
	if len(plan.Notes) > 0 {
		fmt.Printf("[dry-run] notes=%s\n", strings.Join(plan.Notes, "; "))
	}
}

func submitKeyPlan(submitKey, fallbackSubmitKey string) []string {
	steps := make([]string, 0, 2)
	steps = append(steps, "submit:"+resolveSubmitKey(submitKey))
	if fallback := resolveSubmitKey(fallbackSubmitKey); fallback != "" {
		steps = append(steps, "fallback-submit:"+fallback)
	}
	return steps
}

func resolveSubmitKey(raw string) string {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "C-m"
	}
	return key
}

func normalizeDryRunOutputFormat(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case dryRunOutputFormatPlain:
		return dryRunOutputFormatPlain
	case dryRunOutputFormatJSON:
		return dryRunOutputFormatJSON
	case dryRunOutputFormatYAML, "yml":
		return dryRunOutputFormatYAML
	default:
		return dryRunOutputFormatPlain
	}
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

// isPromptGrowth detects when cursor key presses only added blank lines to
// the terminal instead of navigating a menu. This happens with providers like
// codex where arrow keys create newlines in the input area rather than moving
// a selection cursor. Returns true if the only difference between before and
// after is additional blank/whitespace lines.
func isPromptGrowth(beforeANSI, afterANSI string) bool {
	if beforeANSI == afterANSI {
		return false
	}
	beforePlain := capture.StripANSI(beforeANSI)
	afterPlain := capture.StripANSI(afterANSI)
	return compactNonBlank(beforePlain) == compactNonBlank(afterPlain)
}

func isPromptLineGrowth(beforeANSI, afterANSI string) bool {
	if beforeANSI == afterANSI {
		return false
	}
	beforePlain := normalizeChoiceCapture(capture.StripANSI(beforeANSI))
	afterPlain := normalizeChoiceCapture(capture.StripANSI(afterANSI))
	if compactNonBlank(beforePlain) != compactNonBlank(afterPlain) {
		return false
	}
	return lineCount(afterPlain) > lineCount(beforePlain)
}

func normalizeChoiceCapture(value string) string {
	return strings.ReplaceAll(value, "\r\n", "\n")
}

func lineCount(value string) int {
	if value == "" {
		return 0
	}
	return len(strings.Split(value, "\n"))
}

var numberedMenuLinePattern = regexp.MustCompile(`(?m)^[[:space:]]*(?:[›❯>]\s*)?\d+[\).]\s+.+$`)

func isPromptClearedWithoutMenuTransition(beforeANSI, afterANSI string) bool {
	if beforeANSI == afterANSI {
		return false
	}
	beforePlain := normalizeChoiceCapture(capture.StripANSI(beforeANSI))
	afterPlain := normalizeChoiceCapture(capture.StripANSI(afterANSI))
	if hasStrongMenuContext(beforePlain) {
		return false
	}
	beforePrompt, beforeActive := lastPromptLine(beforePlain)
	afterPrompt, afterActive := lastPromptLine(afterPlain)
	if !beforeActive || !afterActive {
		return false
	}
	if !hasTypedPromptText(beforePrompt) {
		return false
	}
	if hasTypedPromptText(afterPrompt) {
		return false
	}
	if compactNonBlank(removePromptLines(beforePlain)) != compactNonBlank(removePromptLines(afterPlain)) {
		return false
	}
	return true
}

func hasStrongMenuContext(text string) bool {
	return len(numberedMenuLinePattern.FindAllString(text, -1)) >= 2
}

func lastPromptLine(text string) (string, bool) {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if isPromptMarkerLine(line) {
			return line, true
		}
	}
	return "", false
}

func isPromptMarkerLine(line string) bool {
	return strings.HasPrefix(line, "›") ||
		strings.HasPrefix(line, "❯") ||
		strings.HasPrefix(line, ">")
}

func hasTypedPromptText(line string) bool {
	line = strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(line, "›"):
		return strings.TrimSpace(strings.TrimPrefix(line, "›")) != ""
	case strings.HasPrefix(line, "❯"):
		return strings.TrimSpace(strings.TrimPrefix(line, "❯")) != ""
	case strings.HasPrefix(line, ">"):
		return strings.TrimSpace(strings.TrimPrefix(line, ">")) != ""
	default:
		return false
	}
}

func removePromptLines(text string) string {
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if isPromptMarkerLine(strings.TrimSpace(line)) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// compactNonBlank removes blank lines and trims whitespace, returning a
// string suitable for comparing whether two captures have the same
// meaningful content.
func compactNonBlank(text string) string {
	var parts []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			parts = append(parts, strings.TrimSpace(line))
		}
	}
	return strings.Join(parts, "\n")
}

// footerKeyToTmux translates a footer key hint (e.g. "ctrl+s") to tmux key
// notation (e.g. "C-s"). Returns empty string if the format is unrecognized.
func footerKeyToTmux(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	parts := strings.SplitN(key, "+", 2)
	if len(parts) != 2 {
		return ""
	}
	modifier, char := parts[0], parts[1]
	switch modifier {
	case "ctrl":
		return "C-" + char
	case "shift":
		// shift+tab -> special, most shift combos not directly usable
		return ""
	case "alt":
		return "M-" + char
	default:
		return ""
	}
}

// ApplyFooterKeyHints applies dynamic key hints from footer parsing to an
// executor profile. This is used as a candidate suggestion only -- the caller
// decides whether to use it based on testing/validation.
func (p *executorProfile) ApplyFooterKeyHints(hints map[string]string) {
	if len(hints) == 0 {
		return
	}
	p.footerKeyHints = hints
}

// FooterSubmitKeyCandidate returns the tmux key notation for the footer's
// "run command" or "enqueue" action, if available. Returns empty string if
// no suitable key hint was found.
func (p *executorProfile) FooterSubmitKeyCandidate() string {
	for key, action := range p.footerKeyHints {
		lower := strings.ToLower(action)
		if strings.Contains(lower, "run command") || strings.Contains(lower, "enqueue") || strings.Contains(lower, "submit") {
			if tmuxKey := footerKeyToTmux(key); tmuxKey != "" {
				return tmuxKey
			}
		}
	}
	return ""
}

func normalizeRuntimeProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "codex", "gemini", "glm", "copilot":
		return strings.ToLower(strings.TrimSpace(provider))
	default:
		return ""
	}
}
