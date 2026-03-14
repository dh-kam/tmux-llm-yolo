package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dh-kam/tmux-llm-yolo/internal/capture"
	"github.com/dh-kam/tmux-llm-yolo/internal/llm"
	"github.com/dh-kam/tmux-llm-yolo/internal/prompt"
	"github.com/dh-kam/tmux-llm-yolo/internal/tmux"
)

type fakeTmuxClient struct {
	ansi     string
	plain    string
	width    int
	sendKeys [][]string
	sendErr  error
	onSend   func(*fakeTmuxClient, []string)
}

func (f *fakeTmuxClient) ListSessions(ctx context.Context) ([]tmux.Session, error) {
	return nil, nil
}

func (f *fakeTmuxClient) HasSession(ctx context.Context, session string) (bool, error) {
	return true, nil
}

func (f *fakeTmuxClient) CapturePane(ctx context.Context, target string, lines int, includeANSI bool) (string, error) {
	if includeANSI {
		return f.ansi, nil
	}
	return f.plain, nil
}

func (f *fakeTmuxClient) PaneSize(ctx context.Context, target string) (int, int, error) {
	if f.width <= 0 {
		f.width = 120
	}
	return f.width, 40, nil
}

func (f *fakeTmuxClient) SendKeys(ctx context.Context, target string, keys ...string) error {
	f.sendKeys = append(f.sendKeys, append([]string(nil), keys...))
	if f.onSend != nil {
		f.onSend(f, append([]string(nil), keys...))
	}
	return f.sendErr
}

func (f *fakeTmuxClient) IsPaneInMode(ctx context.Context, target string) (bool, error) {
	return false, nil
}

func (f *fakeTmuxClient) CreateSession(ctx context.Context, name string) error { return nil }
func (f *fakeTmuxClient) AttachSession(ctx context.Context, name string) error { return nil }
func (f *fakeTmuxClient) KillSession(ctx context.Context, name string) error   { return nil }

type fakeLLMProvider struct {
	prompt   string
	response string
}

func (f *fakeLLMProvider) Name() string                    { return "fake" }
func (f *fakeLLMProvider) Binary() string                  { return "fake" }
func (f *fakeLLMProvider) ValidateBinary() (string, error) { return "fake", nil }
func (f *fakeLLMProvider) CheckUsage(context.Context) (llm.Usage, error) {
	return llm.Usage{}, nil
}
func (f *fakeLLMProvider) RunPrompt(_ context.Context, prompt string) (string, error) {
	f.prompt = prompt
	if strings.TrimSpace(f.response) == "" {
		return "ACTION: INJECT_CONTINUE\nRECOMMENDED_CHOICE: none\nCONTINUE_MESSAGE: none\nREASON: test\n", nil
	}
	return f.response, nil
}
func (f *fakeLLMProvider) IsProgressingCapture(llm.Capture) (bool, string) { return false, "" }

func TestLLMStatusLineIncludesPrimaryFallbackAndActive(t *testing.T) {
	r := &Runner{
		cfg: Config{
			LLMName:          "codex",
			LLMModel:         "gpt-5",
			FallbackLLMName:  "gemini",
			FallbackLLMModel: "gemini-3",
		},
		primaryInitDone:  true,
		fallbackInitDone: true,
		lastLLMProvider:  "fallback:gemini",
	}

	got := r.llmStatusLine()
	wantParts := []string{
		"primary=codex/gpt-5:ready",
		"fallback=gemini/gemini-3:ready",
		"active=fallback:gemini",
	}
	for _, part := range wantParts {
		if !strings.Contains(got, part) {
			t.Fatalf("status %q missing part %q", got, part)
		}
	}
}

func TestLLMStatusLineShowsFailure(t *testing.T) {
	r := &Runner{
		cfg: Config{
			LLMName: "codex",
		},
		primaryInitDone: true,
		primaryInitErr:  errStop,
	}
	got := r.llmStatusLine()
	if !strings.Contains(got, "primary=codex:failed") {
		t.Fatalf("status %q missing failed primary state", got)
	}
}

func TestScopeLineIncludesSessionModeAndCapture(t *testing.T) {
	r := &Runner{
		cfg: Config{
			Target:       "tmp-codex",
			CaptureLines: 40,
			Once:         true,
		},
	}

	got := r.scopeLine()
	wantParts := []string{
		"session=tmp-codex",
		"mode=once",
		"capture=40",
	}
	for _, part := range wantParts {
		if !strings.Contains(got, part) {
			t.Fatalf("scope %q missing part %q", got, part)
		}
	}
}

func TestPolicyLineIncludesWaitContinueAndFallbackPlan(t *testing.T) {
	r := &Runner{
		cfg: Config{
			BaseInterval:     60 * time.Second,
			FallbackLLMName:  "gemini",
			FallbackLLMModel: "gemini-3",
		},
		continuePlan:      newContinueStrategy("fallback"),
		continueSentCount: 7,
	}

	got := r.policyLine()
	wantParts := []string{
		"wait=1m0s>4s>4s>4s",
		"continue=7 sent,next-audit=13",
		"policy=default",
		"llm=primary->fallback",
	}
	for _, part := range wantParts {
		if !strings.Contains(got, part) {
			t.Fatalf("policy %q missing part %q", got, part)
		}
	}
}

func TestNewUsesConfiguredPolicy(t *testing.T) {
	r := New(Config{
		PolicyName:      "parity-porting",
		ContinueMessage: "",
	}, nil, func(string, ...interface{}) {})

	if got := r.policyName(); got != "parity-porting" {
		t.Fatalf("policyName=%q want parity-porting", got)
	}
}

func TestActionProviderHintMatchesCopilotTarget(t *testing.T) {
	cfg := Config{
		Target:            "tmp-copilot",
		SubmitKey:         "C-m",
		SubmitKeyFallback: "C-m",
	}

	if got := actionProviderHint(cfg); got != "copilot" {
		t.Fatalf("actionProviderHint=%q want copilot", got)
	}
}

func TestPromptProviderHintPrefersConfiguredLLMOverTargetName(t *testing.T) {
	r := &Runner{
		cfg: Config{
			Target:  "jkdeps-codex",
			LLMName: "glm",
		},
	}

	if got := r.promptProviderHint(); got != "glm" {
		t.Fatalf("promptProviderHint=%q want glm", got)
	}
}

func TestCopilotSlashPendingInputRequiresReplace(t *testing.T) {
	r := &Runner{}
	analysis := prompt.Analysis{
		Provider:     "copilot",
		PromptText:   "❯ /계속 진행하되 중간에 막히는 지점이 있으면 스스로 가설을 세우고 검증까지 이어서 진행해보자.ss",
		PromptActive: true,
	}

	if !r.hasPendingPromptInput(analysis) {
		t.Fatalf("hasPendingPromptInput=false want true")
	}
	if !r.shouldReplacePendingPromptInput(analysis) {
		t.Fatalf("shouldReplacePendingPromptInput=false want true")
	}
}

func TestCopilotSlashCommandStateDetectedFromPromptAndMenu(t *testing.T) {
	r := &Runner{}

	if !r.hasCopilotSlashCommandState(prompt.Analysis{
		Provider:     "copilot",
		PromptText:   "❯ /add-dir",
		PromptActive: true,
	}) {
		t.Fatalf("slash command prompt should be detected")
	}

	if !r.hasCopilotSlashCommandState(prompt.Analysis{
		Provider:    "copilot",
		OutputBlock: "▋ /add-dir <directory>\n▋ /agent",
	}) {
		t.Fatalf("slash command menu should be detected")
	}
}

func TestCodexPlaceholderPromptIsNotPendingInput(t *testing.T) {
	r := &Runner{}
	analysis := prompt.Analysis{
		Provider:          "codex",
		PromptText:        "› Find and fix a bug in @filename",
		PromptActive:      true,
		PromptPlaceholder: true,
	}

	if r.hasPendingPromptInput(analysis) {
		t.Fatalf("hasPendingPromptInput=true want false")
	}
}

func TestHasLongStableANSIReturnsTrueAfterOneMinute(t *testing.T) {
	now := time.Now()
	r := &Runner{
		ansiHistory: []ansiSnapshot{
			{ANSI: "older-other", TakenAt: now.Add(-4 * time.Minute)},
			{ANSI: "same-screen", TakenAt: now.Add(-70 * time.Second)},
			{ANSI: "same-screen", TakenAt: now.Add(-30 * time.Second)},
		},
	}

	if !r.hasLongStableANSI("same-screen", now) {
		t.Fatalf("hasLongStableANSI=false want true")
	}
}

func TestWatchDurationDefaultsTo24Hours(t *testing.T) {
	r := &Runner{}
	if got := r.watchDuration(); got != 24*time.Hour {
		t.Fatalf("watchDuration=%s, want %s", got, 24*time.Hour)
	}
}

func TestWatchDurationUsesConfiguredValue(t *testing.T) {
	r := &Runner{cfg: Config{Duration: 2 * time.Hour}}
	if got := r.watchDuration(); got != 2*time.Hour {
		t.Fatalf("watchDuration=%s, want %s", got, 2*time.Hour)
	}
}

func TestContinueMessageFromPlannedItemsBuildsPrompt(t *testing.T) {
	analysis := prompt.Analysis{
		Classification: prompt.ClassFreeTextRequest,
		PromptActive:   true,
		OutputBlock: strings.Join([]string{
			"남은 항목",
			"1. presubmit upload에서 스모크 실행 시간을 줄이기 위한 선택적 경량 모드 설계/적용",
			"2. completion gate와 smoke gate의 중복 단계를 정리해 총 수행 시간 최적화",
		}, "\n"),
	}

	got := continueMessageFromPlannedItems(analysis, "")
	if !strings.Contains(got, "남은 항목 1번") {
		t.Fatalf("message=%q missing first item index", got)
	}
	if !strings.Contains(got, "presubmit upload") {
		t.Fatalf("message=%q missing first item text", got)
	}
}

func TestBaseCaptureTaskBypassesANSIStabilityForInteractivePrompt(t *testing.T) {
	client := &fakeTmuxClient{
		ansi: strings.Join([]string{
			"● Bash(go test ./pkg/... -run TestParse)",
			"● Reading 1 file… (ctrl+o to expand)",
			"",
			"Bash command",
			"  go test ./pkg/... -run TestParse",
			"Do you want to proceed?",
			"❯ 1. Yes",
			"  2. Yes, and don't ask again for: go test:*",
			"  3. No",
			"Esc to cancel · Tab to amend",
		}, "\n"),
		plain: strings.Join([]string{
			"● Bash(go test ./pkg/... -run TestParse)",
			"● Reading 1 file… (ctrl+o to expand)",
			"",
			"Bash command",
			"  go test ./pkg/... -run TestParse",
			"Do you want to proceed?",
			"❯ 1. Yes",
			"  2. Yes, and don't ask again for: go test:*",
			"  3. No",
			"Esc to cancel · Tab to amend",
		}, "\n"),
	}
	prev := capture.Snapshot{
		ANSI:    strings.ReplaceAll(client.ansi, "● Reading", "  Reading"),
		Plain:   client.plain,
		TakenAt: time.Now().Add(-4 * time.Second),
	}
	r := &Runner{
		cfg: Config{
			Target:       "tmp-codex",
			SubmitKey:    "C-m",
			CaptureLines: 40,
		},
		client:       client,
		fetcher:      capture.Fetcher{Client: client},
		continuePlan: newContinueStrategy("continue"),
		logger:       func(string, ...interface{}) {},
		ctx:          context.Background(),
		prevBase:     prev,
		screenHistory: []screenSnapshot{
			{
				ChangedLineRatio:      0.03,
				PromptZoneFingerprint: prompt.PromptZoneFingerprint(client.plain),
				InteractivePrompt:     true,
				TakenAt:               time.Now().Add(-2 * time.Second),
			},
		},
	}

	if err := (baseCaptureTask{}).Run(r); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if r.state != stateConfidentWaiting {
		t.Fatalf("state=%q want %q", r.state, stateConfidentWaiting)
	}
	if len(r.queue) != 2 {
		t.Fatalf("queue length=%d want 2", len(r.queue))
	}
	next, ok := r.queue[1].(analyzeWaitingTask)
	if !ok {
		t.Fatalf("second queued task type=%T want analyzeWaitingTask", r.queue[1])
	}
	if !next.allowVolatileANSI {
		t.Fatalf("allowVolatileANSI=false want true")
	}
}

func TestAnalyzeWaitingTaskAllowsVolatileANSIWhenPromptZoneMatches(t *testing.T) {
	client := &fakeTmuxClient{
		ansi: strings.Join([]string{
			"● Bash(go test ./pkg/... -run TestParse)",
			"● Reading 1 file… (ctrl+o to expand)",
			"",
			"Bash command",
			"  go test ./pkg/... -run TestParse",
			"Do you want to proceed?",
			"❯ 1. Yes",
			"  2. Yes, and don't ask again for: go test:*",
			"  3. No",
			"Esc to cancel · Tab to amend",
		}, "\n"),
		plain: strings.Join([]string{
			"● Bash(go test ./pkg/... -run TestParse)",
			"● Reading 1 file… (ctrl+o to expand)",
			"",
			"Bash command",
			"  go test ./pkg/... -run TestParse",
			"Do you want to proceed?",
			"❯ 1. Yes",
			"  2. Yes, and don't ask again for: go test:*",
			"  3. No",
			"Esc to cancel · Tab to amend",
		}, "\n"),
		onSend: func(f *fakeTmuxClient, keys []string) {
			if len(keys) == 1 && keys[0] == "Down" {
				f.ansi = strings.Replace(f.ansi, "❯ 1. Yes", "  1. Yes", 1)
				f.ansi = strings.Replace(f.ansi, "  2. Yes, and don't ask again for: go test:*", "❯ 2. Yes, and don't ask again for: go test:*", 1)
			}
		},
	}
	referenceANSI := strings.ReplaceAll(client.ansi, "● Reading", "  Reading")
	r := &Runner{
		cfg: Config{
			Target:       "tmp-codex",
			SubmitKey:    "C-m",
			CaptureLines: 40,
		},
		client:  client,
		fetcher: capture.Fetcher{Client: client},
		logger:  func(string, ...interface{}) {},
		ctx:     context.Background(),
	}

	err := (analyzeWaitingTask{
		referenceANSI:       referenceANSI,
		referencePromptZone: prompt.PromptZoneFingerprint(client.plain),
		allowVolatileANSI:   true,
	}).Run(r)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(client.sendKeys) != 2 {
		t.Fatalf("sendKeys calls = %d, want 2", len(client.sendKeys))
	}
	if got := client.sendKeys[0]; len(got) != 1 || got[0] != "Down" {
		t.Fatalf("first sendKeys = %v, want [Down]", got)
	}
	if got := client.sendKeys[1]; len(got) != 1 || got[0] != "C-m" {
		t.Fatalf("second sendKeys = %v, want [C-m]", got)
	}
}

func TestAnalyzeWaitingTaskSelectsPersistentAllowForLiveJkdepsApprovalPrompt(t *testing.T) {
	base := filepath.Join("..", "..", "testdata", "live-captures-codex", "20260314-012835", "jkdeps-codex")
	ansiRaw, err := os.ReadFile(filepath.Join(base, "ansi", "001.ansi.txt"))
	if err != nil {
		t.Fatalf("read ansi failed: %v", err)
	}
	plainRaw, err := os.ReadFile(filepath.Join(base, "plain", "001.plain.txt"))
	if err != nil {
		t.Fatalf("read plain failed: %v", err)
	}

	client := &fakeTmuxClient{
		ansi:  string(ansiRaw),
		plain: string(plainRaw),
		onSend: func(f *fakeTmuxClient, keys []string) {
			if len(keys) == 1 && keys[0] == "Down" {
				f.ansi = f.ansi + "\n[moved-to-choice-2]"
			}
		},
	}
	r := &Runner{
		cfg: Config{
			Target:       "jkdeps-codex",
			SubmitKey:    "C-m",
			CaptureLines: 120,
		},
		client:  client,
		fetcher: capture.Fetcher{Client: client},
		logger:  func(string, ...interface{}) {},
		ctx:     context.Background(),
	}

	err = (analyzeWaitingTask{
		referenceANSI:       string(ansiRaw),
		referencePromptZone: prompt.PromptZoneFingerprint(string(plainRaw)),
		allowVolatileANSI:   true,
	}).Run(r)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(client.sendKeys) != 2 {
		t.Fatalf("sendKeys calls = %d, want 2: %v", len(client.sendKeys), client.sendKeys)
	}
	if got := client.sendKeys[0]; len(got) != 1 || got[0] != "Down" {
		t.Fatalf("first sendKeys = %v, want [Down]", got)
	}
	if got := client.sendKeys[1]; len(got) != 1 || got[0] != "C-m" {
		t.Fatalf("second sendKeys = %v, want [C-m]", got)
	}
}

func TestAnalyzeWaitingTaskInjectsContinueForNumberedPlanWithPrompt(t *testing.T) {
	base := filepath.Join("..", "..", "testdata", "live-captures-codex", "20260314-010538", "r8-codex")
	ansiRaw, err := os.ReadFile(filepath.Join(base, "ansi", "001.ansi.txt"))
	if err != nil {
		t.Fatalf("read ansi failed: %v", err)
	}
	plainRaw, err := os.ReadFile(filepath.Join(base, "plain", "001.plain.txt"))
	if err != nil {
		t.Fatalf("read plain failed: %v", err)
	}

	client := &fakeTmuxClient{
		ansi:  string(ansiRaw),
		plain: string(plainRaw),
	}
	r := &Runner{
		cfg: Config{
			Target:          "r8-codex",
			SubmitKey:       "C-m",
			CaptureLines:    120,
			ContinueMessage: "fallback continue",
		},
		client:       client,
		fetcher:      capture.Fetcher{Client: client},
		continuePlan: newContinueStrategy("fallback continue"),
		logger:       func(string, ...interface{}) {},
		ctx:          context.Background(),
	}

	err = (analyzeWaitingTask{referenceANSI: strings.TrimRight(client.ansi, "\n")}).Run(r)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if r.continueSentCount != 1 {
		t.Fatalf("continueSentCount = %d, want 1", r.continueSentCount)
	}
	if len(client.sendKeys) < 3 {
		t.Fatalf("sendKeys calls = %d, want at least 3", len(client.sendKeys))
	}
	if got := client.sendKeys[0]; len(got) != 1 || got[0] != "C-u" {
		t.Fatalf("first sendKeys = %v, want [C-u]", got)
	}
	if got := client.sendKeys[1]; len(got) != 2 || got[0] != "-l" {
		t.Fatalf("second sendKeys = %v, want typed continue message", got)
	}
	if !strings.Contains(client.sendKeys[1][1], "남은 항목 1번") {
		t.Fatalf("typed continue message=%q missing first item reference", client.sendKeys[1][1])
	}
	if strings.TrimSpace(client.sendKeys[1][1]) == "1" {
		t.Fatalf("typed continue message=%q should not be raw numeric choice", client.sendKeys[1][1])
	}
	if got := client.sendKeys[2]; len(got) != 1 || got[0] != "C-m" {
		t.Fatalf("third sendKeys = %v, want [C-m]", got)
	}
}

func TestAnalyzeWaitingTaskForceInputInjectsContinueOnCompletedNoOp(t *testing.T) {
	client := &fakeTmuxClient{
		ansi: strings.Join([]string{
			"작업 완료",
			"추가 작업 없음",
			"\x1b[1m›\x1b[0m ",
			"\x1b[2mgpt-5.4 medium · 29% left · /workspace/tmp\x1b[0m",
		}, "\n"),
		plain: strings.Join([]string{
			"작업 완료",
			"추가 작업 없음",
			"› ",
			"gpt-5.4 medium · 29% left · /workspace/tmp",
		}, "\n"),
	}
	r := &Runner{
		cfg: Config{
			Target:          "tmp-codex",
			SubmitKey:       "C-m",
			ContinueMessage: "fallback continue",
		},
		client:       client,
		fetcher:      capture.Fetcher{Client: client},
		continuePlan: newContinueStrategy("fallback continue"),
		logger:       func(string, ...interface{}) {},
		ctx:          context.Background(),
	}

	err := (analyzeWaitingTask{referenceANSI: strings.TrimRight(client.ansi, "\n"), forceInput: true}).Run(r)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(client.sendKeys) < 3 {
		t.Fatalf("sendKeys calls = %d, want at least 3: %v", len(client.sendKeys), client.sendKeys)
	}
	if got := client.sendKeys[0]; len(got) != 1 || got[0] != "C-u" {
		t.Fatalf("first sendKeys = %v, want [C-u]", got)
	}
	if got := client.sendKeys[1]; len(got) != 2 || got[0] != "-l" || strings.TrimSpace(got[1]) == "" {
		t.Fatalf("second sendKeys = %v, want typed continue message", got)
	}
	if got := client.sendKeys[2]; len(got) != 1 || got[0] != "C-m" {
		t.Fatalf("third sendKeys = %v, want [C-m]", got)
	}
}

func TestAnalyzeWaitingTaskFallsBackToContinueOnCompletedNoOp(t *testing.T) {
	client := &fakeTmuxClient{
		ansi: strings.Join([]string{
			"작업 완료",
			"추가 작업 없음",
			"\x1b[1m›\x1b[0m ",
			"\x1b[2mgpt-5.4 medium · 29% left · /workspace/tmp\x1b[0m",
		}, "\n"),
		plain: strings.Join([]string{
			"작업 완료",
			"추가 작업 없음",
			"› ",
			"gpt-5.4 medium · 29% left · /workspace/tmp",
		}, "\n"),
	}
	r := &Runner{
		cfg: Config{
			Target:          "tmp-codex",
			SubmitKey:       "C-m",
			CaptureLines:    40,
			ContinueMessage: "fallback continue",
		},
		client:       client,
		fetcher:      capture.Fetcher{Client: client},
		continuePlan: newContinueStrategy("fallback continue"),
		logger:       func(string, ...interface{}) {},
		ctx:          context.Background(),
	}

	err := (analyzeWaitingTask{referenceANSI: strings.TrimRight(client.ansi, "\n")}).Run(r)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if r.continueSentCount != 1 {
		t.Fatalf("continueSentCount = %d, want 1", r.continueSentCount)
	}
	if len(r.queue) != 3 {
		t.Fatalf("queue length = %d, want 3", len(r.queue))
	}
	if len(client.sendKeys) < 3 {
		t.Fatalf("sendKeys calls = %d, want at least 3: %v", len(client.sendKeys), client.sendKeys)
	}
	if got := client.sendKeys[0]; len(got) != 1 || got[0] != "C-u" {
		t.Fatalf("first sendKeys = %v, want [C-u]", got)
	}
	if got := client.sendKeys[1]; len(got) != 2 || got[0] != "-l" || strings.TrimSpace(got[1]) == "" {
		t.Fatalf("second sendKeys = %v, want typed continue message", got)
	}
	if got := client.sendKeys[2]; len(got) != 1 || got[0] != "C-m" {
		t.Fatalf("third sendKeys = %v, want [C-m]", got)
	}
}

func TestApplyLLMDecisionSkipFallsBackToContinue(t *testing.T) {
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

	if err := r.applyLLMDecision(llmDecision{
		Action:          "SKIP",
		Reason:          "nothing left to do",
		ContinueMessage: "parity를 100%에 가깝게 만들 차이를 우선순위대로 정리하고 하나씩 수정하면서 계속 진행해보자.",
	}); err != nil {
		t.Fatalf("applyLLMDecision error = %v", err)
	}
	if r.continueSentCount != 1 {
		t.Fatalf("continueSentCount = %d, want 1", r.continueSentCount)
	}
	if r.state != stateActing {
		t.Fatalf("state = %q, want %q", r.state, stateActing)
	}
	if len(r.queue) != 3 {
		t.Fatalf("queue length = %d, want 3", len(r.queue))
	}
	if len(client.sendKeys) < 3 {
		t.Fatalf("sendKeys calls = %d, want at least 3: %v", len(client.sendKeys), client.sendKeys)
	}
	if got := client.sendKeys[0]; len(got) != 1 || got[0] != "C-u" {
		t.Fatalf("first sendKeys = %v, want [C-u]", got)
	}
	if got := client.sendKeys[1]; len(got) != 2 || got[0] != "-l" || strings.TrimSpace(got[1]) == "" {
		t.Fatalf("second sendKeys = %v, want typed continue message", got)
	}
	if got := client.sendKeys[1][1]; got != "parity를 100%에 가깝게 만들 차이를 우선순위대로 정리하고 하나씩 수정하면서 계속 진행해보자." {
		t.Fatalf("typed continue message = %q, want llm override", got)
	}
	if got := client.sendKeys[2]; len(got) != 1 || got[0] != "C-m" {
		t.Fatalf("third sendKeys = %v, want [C-m]", got)
	}
}

func TestInjectContinueConsumesOverrideOnce(t *testing.T) {
	client := &fakeTmuxClient{}
	r := &Runner{
		cfg: Config{
			Target:          "tmp-codex",
			SubmitKey:       "C-m",
			ContinueMessage: "fallback continue",
		},
		client:           client,
		continuePlan:     newContinueStrategy("fallback continue"),
		logger:           func(string, ...interface{}) {},
		ctx:              context.Background(),
		continueOverride: "llm override continue",
	}

	if err := r.injectContinue("send continue with override"); err != nil {
		t.Fatalf("first injectContinue error = %v", err)
	}
	if err := r.injectContinue("send continue after override"); err != nil {
		t.Fatalf("second injectContinue error = %v", err)
	}
	if len(client.sendKeys) < 6 {
		t.Fatalf("sendKeys calls = %d, want at least 6", len(client.sendKeys))
	}
	if got := client.sendKeys[1][1]; got != "llm override continue" {
		t.Fatalf("first typed continue message = %q, want override", got)
	}
	if got := client.sendKeys[4][1]; got == "llm override continue" {
		t.Fatalf("second typed continue message reused override: %q", got)
	}
}

func TestParseLLMDecisionSupportsContinueMessage(t *testing.T) {
	decision := parseLLMDecision(strings.Join([]string{
		"ACTION: INJECT_CONTINUE",
		"RECOMMENDED_CHOICE: none",
		"CONTINUE_MESSAGE: parity 100%까지 맞추기 위해 남은 차이를 분류하고 하나씩 검증하며 진행하자",
		"REASON: parity 개선 지시가 유용함",
	}, "\n"))

	if decision.Action != "INJECT_CONTINUE" {
		t.Fatalf("Action=%q want INJECT_CONTINUE", decision.Action)
	}
	if decision.ContinueMessage == "" {
		t.Fatalf("ContinueMessage is empty")
	}
	if decision.RecommendedChoice != "" {
		t.Fatalf("RecommendedChoice=%q want empty", decision.RecommendedChoice)
	}
}

func TestClassifyWithLLMFallbackPromptIncludesCompletionAuditGuidance(t *testing.T) {
	provider := &fakeLLMProvider{}
	analysis := prompt.Analysis{
		PromptText:  "›",
		OutputBlock: "모든 작업 완료. parity 98%%.",
	}

	if _, err := classifyWithLLMFallback(context.Background(), provider, "glm", "", analysis, analysis.OutputBlock); err != nil {
		t.Fatalf("classifyWithLLMFallback error = %v", err)
	}
	for _, needle := range []string{
		"run the relevant build/test/unit/integration checks",
		"scan the code for TODO, FIXME, not implemented, stub, placeholder, or missing branches",
		"profile CPU and memory usage",
		"reduce memory footprint where practical",
		"Clean Architecture boundaries",
		"Single Responsibility Principle",
		"interface-driven design",
		"modularity, readability, maintainability, performance, and testability",
	} {
		if !strings.Contains(provider.prompt, needle) {
			t.Fatalf("prompt missing %q\n%s", needle, provider.prompt)
		}
	}
}

func TestInjectContinueDoesNotAdvanceCountOnSendFailure(t *testing.T) {
	client := &fakeTmuxClient{sendErr: errors.New("send failed")}
	r := &Runner{
		cfg: Config{
			Target:          "tmp-codex",
			SubmitKey:       "C-m",
			ContinueMessage: "continue",
		},
		client:       client,
		continuePlan: newContinueStrategy("continue"),
		logger:       func(string, ...interface{}) {},
		ctx:          context.Background(),
	}

	err := r.injectContinue("retry later")
	if err == nil {
		t.Fatal("injectContinue error = nil, want failure")
	}
	if r.continueSentCount != 0 {
		t.Fatalf("continueSentCount = %d, want 0", r.continueSentCount)
	}
}

func TestInjectContinueAdvancesCountAfterSuccessfulSend(t *testing.T) {
	client := &fakeTmuxClient{}
	r := &Runner{
		cfg: Config{
			Target:          "tmp-codex",
			SubmitKey:       "C-m",
			ContinueMessage: "continue",
		},
		client:       client,
		continuePlan: newContinueStrategy("continue"),
		logger:       func(string, ...interface{}) {},
		ctx:          context.Background(),
	}

	if err := r.injectContinue("send continue"); err != nil {
		t.Fatalf("injectContinue error = %v", err)
	}
	if r.continueSentCount != 1 {
		t.Fatalf("continueSentCount = %d, want 1", r.continueSentCount)
	}
}

func TestApplyLLMDecisionInjectInputWithEmptyChoiceFallsBackToContinue(t *testing.T) {
	client := &fakeTmuxClient{}
	r := &Runner{
		cfg: Config{
			Target:          "tmp-codex",
			SubmitKey:       "C-m",
			ContinueMessage: "continue",
		},
		client:       client,
		continuePlan: newContinueStrategy("continue"),
		logger:       func(string, ...interface{}) {},
		ctx:          context.Background(),
	}

	if err := r.applyLLMDecision(llmDecision{
		Action:            "INJECT_INPUT",
		RecommendedChoice: "   ",
		Reason:            "filename input requested",
	}); err != nil {
		t.Fatalf("applyLLMDecision error = %v", err)
	}
	if r.continueSentCount != 1 {
		t.Fatalf("continueSentCount = %d, want 1", r.continueSentCount)
	}
	if len(client.sendKeys) < 3 {
		t.Fatalf("sendKeys calls = %d, want at least 3", len(client.sendKeys))
	}
	if got := client.sendKeys[0]; len(got) != 1 || got[0] != "C-u" {
		t.Fatalf("first sendKeys = %v, want [C-u]", got)
	}
	if got := client.sendKeys[1]; len(got) != 2 || got[0] != "-l" || strings.TrimSpace(got[1]) == "" {
		t.Fatalf("second sendKeys = %v, want typed continue message", got)
	}
	if got := client.sendKeys[2]; len(got) != 1 || got[0] != "C-m" {
		t.Fatalf("third sendKeys = %v, want [C-m]", got)
	}
	if len(r.queue) != 3 {
		t.Fatalf("queue length = %d, want 3", len(r.queue))
	}
}

func TestInjectInputOnceWithEmptyChoiceFallsBackToContinue(t *testing.T) {
	client := &fakeTmuxClient{}
	r := &Runner{
		cfg: Config{
			Target:          "tmp-codex",
			SubmitKey:       "C-m",
			ContinueMessage: "continue",
		},
		client:       client,
		continuePlan: newContinueStrategy("continue"),
		logger:       func(string, ...interface{}) {},
		ctx:          context.Background(),
	}

	if err := r.injectInputOnce("   ", "filename input requested"); err != nil {
		t.Fatalf("injectInputOnce error = %v", err)
	}
	if r.continueSentCount != 1 {
		t.Fatalf("continueSentCount = %d, want 1", r.continueSentCount)
	}
	if r.state != stateStopped {
		t.Fatalf("state = %q, want %q", r.state, stateStopped)
	}
	if len(client.sendKeys) < 3 {
		t.Fatalf("sendKeys calls = %d, want at least 3", len(client.sendKeys))
	}
	if got := client.sendKeys[0]; len(got) != 1 || got[0] != "C-u" {
		t.Fatalf("first sendKeys = %v, want [C-u]", got)
	}
	if got := client.sendKeys[1]; len(got) != 2 || got[0] != "-l" || strings.TrimSpace(got[1]) == "" {
		t.Fatalf("second sendKeys = %v, want typed continue message", got)
	}
	if got := client.sendKeys[2]; len(got) != 1 || got[0] != "C-m" {
		t.Fatalf("third sendKeys = %v, want [C-m]", got)
	}
}

func TestInjectCursorConfirmFallsBackToContinueWhenANSIUnchanged(t *testing.T) {
	client := &fakeTmuxClient{
		ansi:  "same ansi",
		plain: "same plain",
	}
	r := &Runner{
		cfg: Config{
			Target:          "tmp-codex",
			SubmitKey:       "C-m",
			CaptureLines:    40,
			ContinueMessage: "continue",
		},
		client:       client,
		fetcher:      capture.Fetcher{Client: client},
		continuePlan: newContinueStrategy("continue"),
		logger:       func(string, ...interface{}) {},
		ctx:          context.Background(),
	}

	if err := r.injectCursorConfirm("cursor menu detected"); err != nil {
		t.Fatalf("injectCursorConfirm error = %v", err)
	}
	if r.continueSentCount != 1 {
		t.Fatalf("continueSentCount = %d, want 1", r.continueSentCount)
	}
	if len(client.sendKeys) < 4 {
		t.Fatalf("sendKeys calls = %d, want at least 4", len(client.sendKeys))
	}
	if got := client.sendKeys[0]; len(got) != 1 || got[0] != "Down" {
		t.Fatalf("first sendKeys = %v, want [Down]", got)
	}
	if got := client.sendKeys[1]; len(got) != 1 || got[0] != "C-u" {
		t.Fatalf("second sendKeys = %v, want [C-u]", got)
	}
	if got := client.sendKeys[2]; len(got) != 2 || got[0] != "-l" || strings.TrimSpace(got[1]) == "" {
		t.Fatalf("third sendKeys = %v, want typed continue message", got)
	}
	if got := client.sendKeys[3]; len(got) != 1 || got[0] != "C-m" {
		t.Fatalf("fourth sendKeys = %v, want [C-m]", got)
	}
}

func TestInjectCursorConfirmRestoresSelectionAndSubmitsWhenANSIChanges(t *testing.T) {
	client := &fakeTmuxClient{
		ansi:  "before",
		plain: "plain",
		onSend: func(f *fakeTmuxClient, keys []string) {
			if len(keys) == 1 && keys[0] == "Down" {
				f.ansi = "after down"
			}
			if len(keys) == 1 && keys[0] == "Up" {
				f.ansi = "before"
			}
		},
	}
	r := &Runner{
		cfg: Config{
			Target:       "tmp-codex",
			SubmitKey:    "C-m",
			CaptureLines: 40,
		},
		client:  client,
		fetcher: capture.Fetcher{Client: client},
		logger:  func(string, ...interface{}) {},
		ctx:     context.Background(),
	}

	if err := r.injectCursorConfirm("cursor menu detected"); err != nil {
		t.Fatalf("injectCursorConfirm error = %v", err)
	}
	if r.continueSentCount != 0 {
		t.Fatalf("continueSentCount = %d, want 0", r.continueSentCount)
	}
	if len(client.sendKeys) != 3 {
		t.Fatalf("sendKeys calls = %d, want 3", len(client.sendKeys))
	}
	if got := client.sendKeys[0]; len(got) != 1 || got[0] != "Down" {
		t.Fatalf("first sendKeys = %v, want [Down]", got)
	}
	if got := client.sendKeys[1]; len(got) != 1 || got[0] != "Up" {
		t.Fatalf("second sendKeys = %v, want [Up]", got)
	}
	if got := client.sendKeys[2]; len(got) != 1 || got[0] != "C-m" {
		t.Fatalf("third sendKeys = %v, want [C-m]", got)
	}
}

func TestSendContinueMessageStopsBeforeFallbackWhenContextCanceled(t *testing.T) {
	client := &fakeTmuxClient{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sendContinueMessage(ctx, client, "tmp", "continue", "C-m", "C-s", 0.5, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context canceled", err)
	}
	if len(client.sendKeys) != 2 {
		t.Fatalf("sendKeys calls = %d, want 2", len(client.sendKeys))
	}
	if got := client.sendKeys[0]; len(got) != 2 || got[0] != "-l" || got[1] != "continue" {
		t.Fatalf("first sendKeys = %v, want typed message", got)
	}
	if got := client.sendKeys[1]; len(got) != 1 || got[0] != "C-m" {
		t.Fatalf("second sendKeys = %v, want primary submit only", got)
	}
}

func TestClearPromptStateStopsBeforeCtrlUWhenContextCanceled(t *testing.T) {
	client := &fakeTmuxClient{}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := clearPromptState(ctx, client, "tmp")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context canceled", err)
	}
	if len(client.sendKeys) != 1 {
		t.Fatalf("sendKeys calls = %d, want 1", len(client.sendKeys))
	}
	if got := client.sendKeys[0]; len(got) != 1 || got[0] != "Escape" {
		t.Fatalf("first sendKeys = %v, want [Escape]", got)
	}
}
