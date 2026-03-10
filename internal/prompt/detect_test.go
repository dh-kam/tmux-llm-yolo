package prompt

import "testing"

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
