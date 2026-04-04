package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dh-kam/yollo/internal/llm"
)

type sessionTaskPlanner struct {
	mu          sync.Mutex
	path        string
	locale      string
	llmName     string
	llmModel    string
	logger      func(string, ...interface{})
	data        sessionTaskFile
	lastRefresh time.Time
	guidance    string
}

type sessionTaskFile struct {
	Target      string            `json:"target"`
	UpdatedAt   string            `json:"updated_at"`
	UserPrompts []string          `json:"user-prompts"`
	TaskTurns   []sessionTaskTurn `json:"task-turns"`
}

type sessionTaskTurn struct {
	CreatedAt         string            `json:"created_at"`
	SourcePromptCount int               `json:"source_prompt_count"`
	Tasks             []sessionTaskItem `json:"tasks"`
}

type sessionTaskItem struct {
	Role        string `json:"role"`
	Title       string `json:"title"`
	Instruction string `json:"instruction"`
	Completed   bool   `json:"completed"`
	CompletedAt string `json:"completed_at,omitempty"`
}

type llmProviderGetter func(context.Context) (llm.Provider, error)

func newSessionTaskPlanner(target string, locale string, llmName string, llmModel string, logger func(string, ...interface{})) *sessionTaskPlanner {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	baseDir := filepath.Join(home, ".yollo", "sessions")
	path := filepath.Join(baseDir, sanitizeSessionFilename(target)+".json")
	p := &sessionTaskPlanner{
		path:     path,
		locale:   locale,
		llmName:  llmName,
		llmModel: llmModel,
		logger:   logger,
		data: sessionTaskFile{
			Target:      target,
			UserPrompts: make([]string, 0, 32),
			TaskTurns:   make([]sessionTaskTurn, 0, 16),
		},
	}
	_ = p.load()
	return p
}

func sanitizeSessionFilename(target string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", " ", "_", ":", "_")
	value := strings.TrimSpace(replacer.Replace(target))
	if value == "" {
		return "session"
	}
	return value
}

func (p *sessionTaskPlanner) AddUserPrompt(ctx context.Context, promptText string, getProvider llmProviderGetter) error {
	if p == nil {
		return nil
	}
	promptText = strings.TrimSpace(promptText)
	if promptText == "" {
		return nil
	}
	p.mu.Lock()
	if len(p.data.UserPrompts) > 0 && strings.TrimSpace(p.data.UserPrompts[len(p.data.UserPrompts)-1]) == promptText {
		p.mu.Unlock()
		return nil
	}
	p.data.UserPrompts = append(p.data.UserPrompts, promptText)
	p.data.UpdatedAt = time.Now().Format(time.RFC3339)
	saveErr := p.saveLocked()
	shouldRefresh := p.lastRefresh.IsZero() || time.Since(p.lastRefresh) >= 10*time.Second
	p.mu.Unlock()
	if saveErr != nil {
		return saveErr
	}
	if !shouldRefresh {
		return nil
	}
	if err := p.refreshTaskTurn(ctx, getProvider); err != nil {
		if p.logger != nil {
			p.logger("task-turn refresh failed: %v", err)
		}
	}
	return nil
}

func (p *sessionTaskPlanner) NextTaskMessage(ctx context.Context, getProvider llmProviderGetter) (string, bool) {
	if p == nil {
		return "", false
	}
	p.mu.Lock()
	turnIdx, taskIdx := findPendingTaskLocked(p.data.TaskTurns)
	p.mu.Unlock()

	if turnIdx < 0 || taskIdx < 0 {
		_ = p.refreshTaskTurn(ctx, getProvider)
		p.mu.Lock()
		turnIdx, taskIdx = findPendingTaskLocked(p.data.TaskTurns)
		p.mu.Unlock()
	}
	if turnIdx < 0 || taskIdx < 0 {
		return "", false
	}

	p.mu.Lock()
	task := p.data.TaskTurns[turnIdx].Tasks[taskIdx]
	p.data.TaskTurns[turnIdx].Tasks[taskIdx].Completed = true
	p.data.TaskTurns[turnIdx].Tasks[taskIdx].CompletedAt = time.Now().Format(time.RFC3339)
	p.data.UpdatedAt = time.Now().Format(time.RFC3339)
	_ = p.saveLocked()
	p.mu.Unlock()

	message := strings.TrimSpace(task.Instruction)
	if message == "" {
		return "", false
	}
	return message, true
}

func findPendingTaskLocked(turns []sessionTaskTurn) (int, int) {
	for ti := len(turns) - 1; ti >= 0; ti-- {
		for i := range turns[ti].Tasks {
			if !turns[ti].Tasks[i].Completed && strings.TrimSpace(turns[ti].Tasks[i].Instruction) != "" {
				return ti, i
			}
		}
	}
	return -1, -1
}

func (p *sessionTaskPlanner) refreshTaskTurn(ctx context.Context, getProvider llmProviderGetter) error {
	p.mu.Lock()
	prompts := append([]string(nil), p.data.UserPrompts...)
	guidance := strings.TrimSpace(p.guidance)
	promptCount := len(prompts)
	p.mu.Unlock()
	if promptCount == 0 {
		return nil
	}

	tasks := p.synthesizeTasks(ctx, prompts, guidance, getProvider)
	if len(tasks) == 0 {
		return nil
	}

	turn := sessionTaskTurn{
		CreatedAt:         time.Now().Format(time.RFC3339),
		SourcePromptCount: promptCount,
		Tasks:             tasks,
	}

	p.mu.Lock()
	p.data.TaskTurns = append(p.data.TaskTurns, turn)
	p.data.UpdatedAt = time.Now().Format(time.RFC3339)
	p.lastRefresh = time.Now()
	err := p.saveLocked()
	p.mu.Unlock()
	return err
}

func (p *sessionTaskPlanner) synthesizeTasks(ctx context.Context, prompts []string, guidance string, getProvider llmProviderGetter) []sessionTaskItem {
	fallback := heuristicTasks(prompts)
	if getProvider == nil {
		return fallback
	}
	provider, err := getProvider(ctx)
	if err != nil || provider == nil {
		return fallback
	}
	llmPrompt := buildTaskSynthesisPrompt(prompts, guidance, p.locale)
	raw, err := provider.RunPrompt(ctx, llmPrompt)
	if err != nil {
		return fallback
	}
	items := parseTaskItems(raw)
	if len(items) == 0 {
		return fallback
	}
	if len(items) > 12 {
		items = items[:12]
	}
	return items
}

func buildTaskSynthesisPrompt(prompts []string, guidance string, locale string) string {
	body := strings.Join(prompts, "\n---\n")
	guidance = strings.TrimSpace(guidance)
	guidanceSection := ""
	if guidance != "" {
		guidanceSection = fmt.Sprintf("AGENTS policy context:\n%s\n\n", guidance)
	}
	return fmt.Sprintf(`You are a senior software delivery strategist.
Infer the user's long-term intent from cumulative prompts.
Generate a detailed action list that fits the user's style.
Include perspectives across: software architect, product design, marketing, debugging, tester, developer, designer.

%sReturn JSON only with this schema:
{
  "tasks": [
    {"role":"architect|product|marketing|debugger|tester|developer|designer","title":"short title","instruction":"single actionable instruction in %s"}
  ]
}

Rules:
- instruction must be imperative and immediately executable.
- avoid vague advice.
- produce 6-10 tasks.
- tailor to user preference signals from prompts.

Cumulative user prompts:
%s
`, guidanceSection, localeLabel(locale), body)
}

func (p *sessionTaskPlanner) SetGuidance(text string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.guidance = strings.TrimSpace(text)
	p.mu.Unlock()
}

func localeLabel(locale string) string {
	switch strings.ToLower(strings.TrimSpace(locale)) {
	case "ko":
		return "Korean"
	case "ja":
		return "Japanese"
	case "zh":
		return "Chinese"
	default:
		return "English"
	}
}

func parseTaskItems(raw string) []sessionTaskItem {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	type response struct {
		Tasks []sessionTaskItem `json:"tasks"`
	}
	var parsed response
	if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
		return normalizeTaskItems(parsed.Tasks)
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err == nil {
			return normalizeTaskItems(parsed.Tasks)
		}
	}
	return nil
}

func normalizeTaskItems(items []sessionTaskItem) []sessionTaskItem {
	out := make([]sessionTaskItem, 0, len(items))
	for _, item := range items {
		item.Role = strings.TrimSpace(item.Role)
		item.Title = strings.TrimSpace(item.Title)
		item.Instruction = strings.TrimSpace(item.Instruction)
		if item.Instruction == "" {
			continue
		}
		item.Completed = false
		item.CompletedAt = ""
		out = append(out, item)
	}
	return out
}

func heuristicTasks(prompts []string) []sessionTaskItem {
	last := ""
	if len(prompts) > 0 {
		last = strings.TrimSpace(prompts[len(prompts)-1])
	}
	if last == "" {
		last = "현재 목표를 달성하기 위해 남은 작업을 구조화하고 우선순위대로 실행한다."
	}
	return []sessionTaskItem{
		{Role: "architect", Title: "Architecture checkpoint", Instruction: "요구사항을 모듈 경계와 책임 기준으로 재정리하고 현재 코드와의 차이를 우선순위로 정리한다."},
		{Role: "developer", Title: "Execution", Instruction: last},
		{Role: "tester", Title: "Validation", Instruction: "변경 사항에 대해 재현 가능한 테스트 시나리오를 만들고 실패/성공 기준을 명시해 검증한다."},
	}
}

func (p *sessionTaskPlanner) load() error {
	if p == nil {
		return nil
	}
	data, err := os.ReadFile(p.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var parsed sessionTaskFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	if strings.TrimSpace(parsed.Target) == "" {
		parsed.Target = p.data.Target
	}
	if parsed.UserPrompts == nil {
		parsed.UserPrompts = make([]string, 0, 32)
	}
	if parsed.TaskTurns == nil {
		parsed.TaskTurns = make([]sessionTaskTurn, 0, 16)
	}
	p.mu.Lock()
	p.data = parsed
	p.mu.Unlock()
	return nil
}

func (p *sessionTaskPlanner) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(p.path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(p.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.path, body, 0o644)
}
