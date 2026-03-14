package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dh-kam/tmux-llm-yolo/internal/i18n"
)

type promptBuilder func(string, string) [][]string

type promptDriver interface {
	validateBinary() (string, error)
	runPrompt(context.Context, string, string, promptBuilder) (string, error)
}

type provider struct {
	name          string
	binary        string
	quotaEnv      []string
	model         string
	progressRegex []*regexp.Regexp
	promptBuilder promptBuilder
	driver        promptDriver
}

func (p provider) Name() string {
	return p.name
}

func (p provider) Binary() string {
	return p.binary
}

func (p provider) ValidateBinary() (string, error) {
	if p.driver == nil {
		return "", fmt.Errorf("no llm driver configured for %s", p.name)
	}
	return p.driver.validateBinary()
}

func (p provider) IsProgressingCapture(capture Capture) (bool, string) {
	return isProgressCaptureForLocale(capture, p.progressRegex, i18n.DefaultAppLocale)
}

func (p provider) CheckUsage(ctx context.Context) (Usage, error) {
	_ = ctx
	for _, envKey := range p.quotaEnv {
		raw, exists := os.LookupEnv(envKey)
		if !exists {
			continue
		}
		remaining, err := parseRemaining(raw)
		if err != nil {
			return Usage{
				HasKnownLimit: false,
				Remaining:     0,
				Source:        envKey,
			}, nil
		}
		return Usage{
			HasKnownLimit: true,
			Remaining:     remaining,
			Source:        envKey,
		}, nil
	}

	return Usage{
		HasKnownLimit: false,
		Remaining:     0,
		Source:        "not configured",
	}, nil
}

func (p provider) RunPrompt(ctx context.Context, prompt string) (string, error) {
	if p.driver == nil {
		return "", fmt.Errorf("no llm driver configured for %s", p.name)
	}
	return p.driver.runPrompt(ctx, prompt, p.model, p.promptBuilder)
}

type commandPromptDriver struct {
	binaries []string
	timeout  time.Duration
}

func newCommandPromptDriver(timeout time.Duration, binaries ...string) promptDriver {
	return commandPromptDriver{
		binaries: binaries,
		timeout:  timeout,
	}
}

func (d commandPromptDriver) validateBinary() (string, error) {
	path, err := d.resolveBinary()
	if err != nil {
		return "", err
	}
	return path, nil
}

func (d commandPromptDriver) runPrompt(ctx context.Context, prompt string, model string, build promptBuilder) (string, error) {
	if build == nil {
		build = func(rawPrompt, _ string) [][]string {
			return [][]string{{rawPrompt}}
		}
	}

	var lastErr error
	argsVariants := build(prompt, model)
	for _, args := range argsVariants {
		output, err := d.runCommand(ctx, args...)
		if err == nil {
			return output, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		return "", fmt.Errorf("llm command failed with no output")
	}
	return "", lastErr
}

func (d commandPromptDriver) runCommand(ctx context.Context, args ...string) (string, error) {
	binary, err := d.resolveBinary()
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func (d commandPromptDriver) resolveBinary() (string, error) {
	var attempts []string
	for _, binary := range d.binaries {
		if strings.TrimSpace(binary) == "" {
			continue
		}
		attempts = append(attempts, strings.TrimSpace(binary))
		if path, err := exec.LookPath(binary); err == nil {
			return path, nil
		}
	}
	if len(attempts) == 0 {
		return "", fmt.Errorf("llm binary not configured")
	}
	return "", fmt.Errorf("llm binary not found: %s", strings.Join(attempts, ", "))
}

type ollamaRESTDriver struct {
	endpoint string
	timeout  time.Duration
	client   *http.Client
}

func newOllamaRESTDriver() promptDriver {
	const ollamaTimeout = 10 * time.Minute
	return ollamaRESTDriver{
		endpoint: defaultOllamaEndpoint(),
		timeout:  ollamaTimeout,
		client: &http.Client{
			Timeout: ollamaTimeout,
		},
	}
}

func (d ollamaRESTDriver) validateBinary() (string, error) {
	reqCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, d.endpoint+"/api/version", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "tmux-yolo-llm")

	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama api is not reachable at %s: %w", d.endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("ollama api returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return d.endpoint, nil
}

func (d ollamaRESTDriver) runPrompt(ctx context.Context, prompt string, model string, _ promptBuilder) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", fmt.Errorf("ollama model is required: use --llm ollama/<model> or set --llm-model")
	}

	reqBody, err := json.Marshal(map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"stream": false,
	})
	if err != nil {
		return "", err
	}

	runCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(runCtx, http.MethodPost, d.endpoint+"/api/generate", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "tmux-yolo-llm")

	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama api request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ollama api error (status=%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		Response string `json:"response"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("ollama api response parse failed: %w", err)
	}
	if parsed.Error != "" {
		return "", fmt.Errorf("ollama api returned error: %s", parsed.Error)
	}
	if strings.TrimSpace(parsed.Response) == "" {
		return "", fmt.Errorf("ollama api returned empty response")
	}

	return strings.TrimSpace(parsed.Response), nil
}

func defaultOllamaEndpoint() string {
	if raw := strings.TrimSpace(os.Getenv("OLLAMA_BASE_URL")); raw != "" {
		return strings.TrimRight(raw, "/")
	}
	if raw := strings.TrimSpace(os.Getenv("OLLAMA_HOST")); raw != "" {
		raw = strings.TrimPrefix(raw, "http://")
		raw = strings.TrimPrefix(raw, "https://")
		return "http://" + strings.TrimRight(raw, "/")
	}
	return "http://127.0.0.1:11434"
}

func canonicalLLMName(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if strings.Contains(normalized, "gemini") {
		return "gemini"
	}
	if strings.HasPrefix(normalized, "ollama/") {
		return "ollama"
	}
	return normalized
}

var (
	sgrPattern               = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	promptLinePattern        = regexp.MustCompile(`^[[:space:]]*([›❯>]|[#$%])\s`)
	promptShellPromptPattern = regexp.MustCompile(`^[^[:space:]]+@[^[:space:]]+.*`)
	promptPlaceholderRegexp  = regexp.MustCompile(`(?i)^[[:space:]]*([›❯>]|[#$%])\s*(type|type here|enter|press|input|입력|입력하세요|message|답변|선택|select|질문|명령|command|placeholder|press enter|다음|continue).*`)
)

func defaultProgressPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`(?i)esc to interrupt`),
		regexp.MustCompile(`(?i)esc to cancel`),
		regexp.MustCompile(`(?i)press ctrl`),
		regexp.MustCompile(`(?i)to interrupt`),
	}
}

var registry = map[string]provider{
	"glm": {
		name:     "glm",
		binary:   "glm",
		quotaEnv: []string{"TMUX_YOLO_GLM_QUOTA_REMAINING", "GLM_RATE_LIMIT_REMAINING", "GLM_QUOTA_REMAINING"},
		progressRegex: []*regexp.Regexp{
			regexp.MustCompile(`(?i)esc to interrupt`),
			regexp.MustCompile(`(?i)esc to cancel`),
			regexp.MustCompile(`(?i)continue.*interrupt`),
		},
		promptBuilder: glmPromptArgs,
		driver:        newCommandPromptDriver(90*time.Second, "glm"),
	},
	"codex": {
		name:     "codex",
		binary:   "codex",
		quotaEnv: []string{"TMUX_YOLO_CODEX_QUOTA_REMAINING", "CODEX_RATE_LIMIT_REMAINING", "CODEX_QUOTA_REMAINING"},
		progressRegex: []*regexp.Regexp{
			regexp.MustCompile(`(?i)esc to interrupt`),
			regexp.MustCompile(`(?i)esc to cancel`),
			regexp.MustCompile(`(?i)to interrupt`),
		},
		promptBuilder: codexPromptArgs,
		driver:        newCommandPromptDriver(90*time.Second, "codex"),
	},
	"copilot": {
		name:     "copilot",
		binary:   "copilot",
		quotaEnv: []string{"TMUX_YOLO_COPILOT_QUOTA_REMAINING", "COPILOT_RATE_LIMIT_REMAINING", "COPILOT_QUOTA_REMAINING"},
		progressRegex: []*regexp.Regexp{
			regexp.MustCompile(`(?i)esc to stop`),
			regexp.MustCompile(`(?i)to stop`),
			regexp.MustCompile(`(?i)esc to interrupt`),
			regexp.MustCompile(`(?i)esc to cancel`),
		},
		promptBuilder: copilotPromptArgs,
		driver:        newCommandPromptDriver(90*time.Second, "copilot"),
	},
	"gemini": {
		name:     "gemini",
		binary:   "gemini",
		quotaEnv: []string{"TMUX_YOLO_GEMINI_QUOTA_REMAINING", "GEMINI_RATE_LIMIT_REMAINING", "GEMINI_QUOTA_REMAINING"},
		progressRegex: []*regexp.Regexp{
			regexp.MustCompile(`(?i)esc to interrupt`),
			regexp.MustCompile(`(?i)esc to cancel`),
		},
		promptBuilder: geminiPromptArgs,
		driver:        newCommandPromptDriver(90*time.Second, "gemini", "gemini-cli"),
	},
	"ollama": {
		name:          "ollama",
		binary:        "ollama",
		quotaEnv:      []string{},
		progressRegex: defaultProgressPatterns(),
		driver:        newOllamaRESTDriver(),
	},
}

func New(name string, model string) (Provider, error) {
	normalized := canonicalLLMName(name)
	p, ok := registry[normalized]
	if !ok {
		return nil, fmt.Errorf("unsupported llm: %s", name)
	}
	p = p.withModel(model)
	if p.driver == nil {
		return nil, fmt.Errorf("unsupported llm driver: %s", normalized)
	}
	if len(p.progressRegex) == 0 {
		p.progressRegex = defaultProgressPatterns()
	}
	if p.promptBuilder == nil && normalized != "ollama" {
		return nil, fmt.Errorf("provider %s missing prompt builder", normalized)
	}
	return p, nil
}

func (p provider) withModel(model string) provider {
	p.model = model
	return p
}

func appendModelToArgs(model string, args []string) []string {
	if model == "" {
		return args
	}
	p := make([]string, 0, len(args)+2)
	p = append(p, args...)
	p = append(p, "--model", model)
	return p
}

func glmPromptArgs(prompt string, model string) [][]string {
	return [][]string{
		appendModelToArgs(model, []string{"--print", prompt}),
		appendModelToArgs(model, []string{"--yolo", "--print", prompt}),
		appendModelToArgs(model, []string{"-p", prompt}),
		appendModelToArgs(model, []string{prompt}),
	}
}

func codexPromptArgs(prompt string, model string) [][]string {
	return [][]string{
		appendModelToArgs(model, []string{"exec", prompt, "--dangerously-bypass-approvals-and-sandbox"}),
		appendModelToArgs(model, []string{"exec", "-p", prompt}),
		appendModelToArgs(model, []string{"exec", prompt}),
		appendModelToArgs(model, []string{"-p", prompt}),
		appendModelToArgs(model, []string{prompt}),
	}
}

func copilotPromptArgs(prompt string, model string) [][]string {
	return [][]string{
		appendModelToArgs(model, []string{"--prompt", prompt, "--silent", "--allow-all-tools"}),
		appendModelToArgs(model, []string{"--prompt", prompt, "--allow-all-tools"}),
		appendModelToArgs(model, []string{"-p", prompt, "--allow-all-tools"}),
		appendModelToArgs(model, []string{"prompt", prompt}),
		appendModelToArgs(model, []string{prompt}),
	}
}

func geminiPromptArgs(prompt string, model string) [][]string {
	return [][]string{
		appendModelToArgs(model, []string{"-p", prompt}),
		appendModelToArgs(model, []string{"prompt", prompt}),
		appendModelToArgs(model, []string{prompt}),
	}
}

func parseRemaining(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("empty value")
	}
	re := regexp.MustCompile(`[-+]?\d+`)
	for _, found := range re.FindAllString(raw, -1) {
		parsed, err := strconv.Atoi(strings.TrimPrefix(found, "+"))
		if err == nil {
			return parsed, nil
		}
	}
	return 0, fmt.Errorf("no numeric quota found in %q", raw)
}

func IsProgressingCaptureForLocale(p Provider, capture Capture, locale string) (bool, string) {
	if concrete, ok := p.(provider); ok {
		return isProgressCaptureForLocale(capture, concrete.progressRegex, locale)
	}
	return p.IsProgressingCapture(capture)
}

func isProgressCapture(capture Capture, progressPatterns []*regexp.Regexp) (bool, string) {
	return isProgressCaptureForLocale(capture, progressPatterns, i18n.DefaultAppLocale)
}

func isProgressCaptureForLocale(capture Capture, progressPatterns []*regexp.Regexp, locale string) (bool, string) {
	if len(progressPatterns) == 0 {
		progressPatterns = defaultProgressPatterns()
	}

	lines := strings.Split(strings.ReplaceAll(capture.Text, "\r\n", "\n"), "\n")
	promptLine := findPromptLine(lines)
	if promptLine < 0 {
		return false, i18n.T(locale, "llm.progress.reason_prompt_line_missing")
	}
	if isPromptPlaceholderLine(lines[promptLine]) {
		if replacement := findPromptLineBefore(lines, promptLine); replacement >= 0 {
			promptLine = replacement
		} else if fallback := findContentLineBefore(lines, promptLine); fallback >= 0 {
			promptLine = fallback
		} else {
			return false, i18n.T(locale, "llm.progress.reason_prompt_placeholder", strings.TrimSpace(stripANSI(lines[promptLine])))
		}
	}

	lastLLMLine := strings.TrimSpace(stripANSI(lastVisibleLineBefore(lines, promptLine)))
	if lastLLMLine == "" {
		return false, i18n.T(locale, "llm.progress.reason_no_output_before_prompt")
	}

	for _, linePattern := range promptWorkingPatterns() {
		if linePattern.MatchString(lastLLMLine) {
			return true, i18n.T(locale, "llm.progress.reason_working_above_prompt", lastLLMLine, linePattern.String())
		}
	}

	windowStart := 0
	if promptLine > 24 {
		windowStart = promptLine - 24
	}
	windowEnd := len(lines)
	if promptLine+24 < len(lines) {
		windowEnd = promptLine + 24
	}

	promptWindow := lines[windowStart:windowEnd]
	if matchLine, linePattern := firstMatchingLine(promptWindow, progressPatterns); matchLine != "" {
		return true, i18n.T(locale, "llm.progress.reason_progress_text", matchLine, linePattern.String(), promptLineHint(promptLine))
	}

	if matchLine, linePattern := firstMatchingLine(promptWindow, promptWorkingPatterns()); matchLine != "" {
		return true, i18n.T(locale, "llm.progress.reason_working_near_prompt", matchLine, linePattern.String(), promptLineHint(promptLine))
	}

	return false, ""
}

func findPromptLineBefore(lines []string, before int) int {
	for i := before - 1; i >= 0; i-- {
		plainLine := strings.TrimSpace(stripANSI(lines[i]))
		if plainLine == "" {
			continue
		}
		if !isPromptLine(plainLine) || isPromptPlaceholderLine(lines[i]) {
			continue
		}
		return i
	}
	return -1
}

func findContentLineBefore(lines []string, before int) int {
	for i := before - 1; i >= 0; i-- {
		line := strings.TrimSpace(stripANSI(lines[i]))
		if line == "" || isPromptLine(line) || isPromptPlaceholderLine(lines[i]) {
			continue
		}
		return i
	}
	return -1
}

func firstMatchingLine(lines []string, patterns []*regexp.Regexp) (string, *regexp.Regexp) {
	for i := len(lines) - 1; i >= 0; i-- {
		lineText := strings.TrimSpace(stripANSI(lines[i]))
		if lineText == "" {
			continue
		}
		if isPromptLine(lineText) || isPromptPlaceholderLine(lines[i]) {
			continue
		}
		for _, pattern := range patterns {
			if pattern.MatchString(lineText) {
				return lineText, pattern
			}
		}
	}
	return "", nil
}

func promptLineHint(promptLine int) string {
	return fmt.Sprintf("promptLine=%d", promptLine)
}

func findPromptLine(lines []string) int {
	promptLine := -1
	styledPromptLine := -1
	placeholderPromptLine := -1

	for i, line := range lines {
		if !isPromptLine(strings.TrimSpace(stripANSI(line))) {
			continue
		}
		if lineHasPromptBackground(line) {
			styledPromptLine = i
		}
		if isPromptPlaceholderLine(line) {
			if placeholderPromptLine == -1 {
				placeholderPromptLine = i
			}
			continue
		}
		promptLine = i
	}

	if styledPromptLine != -1 {
		return styledPromptLine
	}
	if promptLine != -1 {
		return promptLine
	}
	return placeholderPromptLine
}

func lastVisibleLineBefore(lines []string, index int) string {
	if index <= 0 || index > len(lines) {
		return ""
	}
	for i := index - 1; i >= 0; i-- {
		line := strings.TrimSpace(stripANSI(lines[i]))
		if line == "" {
			continue
		}
		if isPromptLine(line) {
			continue
		}
		return line
	}
	return ""
}

func isPromptLine(rawLine string) bool {
	line := strings.TrimSpace(rawLine)
	if line == "" {
		return false
	}
	if promptLinePattern.MatchString(line) || promptShellPromptPattern.MatchString(line) {
		return true
	}
	return false
}

func promptWorkingPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(working|processing|building|compiling|running|loading|wait|waiting|in progress)\b`),
		regexp.MustCompile(`(?i)(작업.?중|처리.?중|빌드.?중|컴파일.?중|실행.?중|대기.?중|로딩.?중|작업중|진행.?중)`),
	}
}

func stripANSI(line string) string {
	return sgrPattern.ReplaceAllString(line, "")
}

func isPromptPlaceholderLine(line string) bool {
	plainLine := strings.TrimSpace(stripANSI(line))
	return promptLinePattern.MatchString(plainLine) && promptPlaceholderRegexp.MatchString(plainLine)
}

func lineHasPromptBackground(line string) bool {
	for _, match := range sgrPattern.FindAllString(line, -1) {
		if sgrHasBackground(match) {
			return true
		}
	}
	return false
}

func sgrHasBackground(sgr string) bool {
	params := strings.TrimSuffix(strings.TrimPrefix(sgr, "\x1b["), "m")
	if params == "" {
		return false
	}
	fields := strings.Split(params, ";")
	for i := 0; i < len(fields); i++ {
		value := strings.TrimSpace(fields[i])
		if value == "" {
			continue
		}
		parsed, err := strconv.Atoi(value)
		if err != nil {
			continue
		}
		if (parsed >= 40 && parsed <= 47) || (parsed >= 100 && parsed <= 107) {
			return true
		}
		if parsed == 48 {
			if i+1 >= len(fields) {
				continue
			}
			next := strings.TrimSpace(fields[i+1])
			if next == "5" || next == "2" {
				return true
			}
		}
	}
	return false
}
