package prompt

import (
	"strings"
	"testing"
)

func TestAnalyzeCodexPlaceholderPromptIsNotTypedInput(t *testing.T) {
	ansiLines := []string{"\x1b[1m›\x1b[0m \x1b[2mFind and fix a bug in @filename\x1b[0m", "\x1b[2m  gpt-5.4 medium · 29% left · /workspace/tmp\x1b[0m"}
	plainLines := []string{"› Find and fix a bug in @filename", "  gpt-5.4 medium · 29% left · /workspace/tmp"}
	if !isCodexPlaceholderPromptLine(0, ansiLines, plainLines) {
		t.Fatalf("isCodexPlaceholderPromptLine=false want true")
	}
	analysis := Analysis{
		PromptText:        "› Find and fix a bug in @filename",
		PromptActive:      true,
		PromptPlaceholder: true,
		AssistantUI:       true,
		OutputBlock:       "다음 우선순위:\n1. TestingState 에서 stacktrace processing 분리\n다음 턴에서는 TestingState 의 남은 stacktrace/status policy 분리를 이어가겠습니다.",
	}
	classification, _, _ := classify(analysis)
	if classification != ClassContinueAfterDone {
		t.Fatalf("Classification=%q want %q", classification, ClassContinueAfterDone)
	}
}

func TestAnalyzeGLMGrayPromptPlaceholderAndHandoff(t *testing.T) {
	ansiLines := []string{
		"\x1b[38;5;239m\x1b[48;5;237m❯ \x1b[38;5;231m계속해서 Task #1 진행해\x1b[39m",
		"\x1b[38;5;246m❯ \x1b[7m\x1b[39m \x1b[0m",
	}
	plainLines := []string{
		"❯ 계속해서 Task #1 진행해",
		"❯ ",
	}
	if !isGLMPlaceholderPromptLine(1, ansiLines, plainLines) {
		t.Fatalf("isGLMPlaceholderPromptLine=false want true")
	}
	analysis := Analysis{
		PromptText:        "❯",
		PromptActive:      true,
		PromptPlaceholder: true,
		AssistantUI:       true,
		OutputBlock:       "📈 다음 단계\n\n다음 우선순위 작업인 Task #1 (AppView 인터페이스 분리) 를 진행할까요?\n\n✻ Cogitated for 15m 42s\n\n4 tasks (1 done, 3 open)",
	}
	classification, _, _ := classify(analysis)
	if classification != ClassContinueAfterDone {
		t.Fatalf("Classification=%q want %q", classification, ClassContinueAfterDone)
	}
}

func TestClassifyApprovalPromptPrefersPersistentAllowChoice(t *testing.T) {
	analysis := Analysis{
		PromptText:   "❯",
		PromptActive: true,
		AssistantUI:  true,
		OutputBlock: strings.Join([]string{
			"Bash command",
			"Do you want to proceed?",
			"❯ 1. Yes",
			"  2. Yes, and don't ask again for: go test:*",
			"  3. No",
			"Esc to cancel · Tab to amend",
		}, "\n"),
	}

	classification, choice, _ := classify(analysis)
	if classification != ClassCursorBasedChoice {
		t.Fatalf("classification=%q want %q", classification, ClassCursorBasedChoice)
	}
	if choice != "2" {
		t.Fatalf("choice=%q want 2", choice)
	}
}

func TestAnalyzeGLMReadPermissionPromptOverridesBottomInputPrompt(t *testing.T) {
	plain := strings.Join([]string{
		"● Reading 1 file… (ctrl+o to expand)",
		"  ⎿  /tmp/jkdeps-samples/channels/BufferedChannel.kt",
		"",
		"────────────────────────────────────────────────────────────────",
		" Read file",
		"",
		"  Read(/tmp/jkdeps-samples/channels/BufferedChannel.kt)",
		"",
		" Do you want to proceed?",
		" ❯ 1. Yes",
		"   2. Yes, allow reading from channels/ during this session",
		"   3. No",
		"",
		" Esc to cancel · Tab to amend",
		"",
		"────────────────────────────────────────────────────────────────",
		"❯ ",
		"  ⏵⏵ accept edits on (shift+tab to cycle) · esc to interrupt",
	}, "\n")
	ansi := strings.Join([]string{
		"● Reading 1 file… (ctrl+o to expand)",
		"  ⎿  /tmp/jkdeps-samples/channels/BufferedChannel.kt",
		"",
		"────────────────────────────────────────────────────────────────",
		" Read file",
		"",
		"  Read(/tmp/jkdeps-samples/channels/BufferedChannel.kt)",
		"",
		" Do you want to proceed?",
		" \x1b[38;5;153m❯\x1b[0m 1. Yes",
		"   2. Yes, allow reading from channels/ during this session",
		"   3. No",
		"",
		" Esc to cancel · Tab to amend",
		"",
		"────────────────────────────────────────────────────────────────",
		"\x1b[38;5;246m❯ \x1b[7m\x1b[39m \x1b[0m",
		"  ⏵⏵ accept edits on (shift+tab to cycle) · esc to interrupt",
	}, "\n")

	analysis := AnalyzeWithHint("glm", ansi, plain)
	if !analysis.PromptDetected {
		t.Fatalf("PromptDetected=false want true")
	}
	if analysis.Classification != ClassCursorBasedChoice {
		t.Fatalf("classification=%q want %q", analysis.Classification, ClassCursorBasedChoice)
	}
	if analysis.RecommendedChoice != "2" {
		t.Fatalf("recommended choice=%q want 2", analysis.RecommendedChoice)
	}
	if !analysis.InteractivePrompt {
		t.Fatalf("interactivePrompt=false want true")
	}
}
