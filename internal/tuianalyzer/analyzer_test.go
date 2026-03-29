package tuianalyzer

import (
	"strings"
	"testing"
)

func TestAnalyze_CodexWelcome(t *testing.T) {
	plain := strings.Join([]string{
		"╭──────────────────────────────────────────────────╮",
		"│ >_ OpenAI Codex (v0.112.0)                      │",
		"│                                                  │",
		"│ model:     gpt-5.4 medium   fast   /model to... │",
		"│ directory: /workspace/tmp                        │",
		"╰──────────────────────────────────────────────────╯",
		"",
		"  Tip: New 2x rate limits until April 2nd.",
		"",
		"› Find and fix a bug in @filename",
		"",
		"  gpt-5.4 medium · 100% left · /workspac…",
	}, "\n")

	result := Analyze("", plain)

	if result.FrontEnd != "codex" {
		t.Errorf("expected frontend codex, got %q", result.FrontEnd)
	}

	// Should detect at least header, prompt, and footer
	hasHeader := false
	hasPrompt := false
	hasFooter := false
	for _, s := range result.Sections {
		switch s.Type {
		case SectionHeader:
			hasHeader = true
		case SectionUserPrompt:
			hasPrompt = true
		case SectionFooter:
			hasFooter = true
		}
	}

	if !hasHeader {
		t.Error("expected SectionHeader to be detected")
	}
	if !hasPrompt {
		t.Error("expected SectionUserPrompt to be detected")
	}
	if !hasFooter {
		t.Error("expected SectionFooter to be detected")
	}
}

func TestAnalyze_GLMWelcome(t *testing.T) {
	plain := strings.Join([]string{
		"╭─ Claude Code ────────────────────────────╮",
		"│                                          │",
		"│              Welcome back!                │",
		"│                                          │",
		"│                 glm-5                     │",
		"│           API Usage Billing               │",
		"│            /workspace/tmp                 │",
		"╰──────────────────────────────────────────╯",
		"",
		"  /model to try Opus 4.6",
		"",
		"❯ 프로젝트 코드 분석해봐.",
		"",
		"──────────────────────────────────────────",
		"  esc to interrupt",
	}, "\n")

	result := Analyze("", plain)

	if result.FrontEnd != "claude-code" {
		t.Errorf("expected frontend claude-code, got %q", result.FrontEnd)
	}

	hasHeader := false
	hasPrompt := false
	for _, s := range result.Sections {
		switch s.Type {
		case SectionHeader:
			hasHeader = true
		case SectionUserPrompt:
			hasPrompt = true
		}
	}
	if !hasHeader {
		t.Error("expected SectionHeader")
	}
	if !hasPrompt {
		t.Error("expected SectionUserPrompt")
	}
}

func TestAnalyze_GeminiWelcome(t *testing.T) {
	plain := strings.Join([]string{
		" █████████",
		"███░░░░░███",
		"  ░░░███   ███     ░░░",
		"    ░░░███░███     █████",
		"   ███░   ░░███  ░░███",
		" ███░      ░░█████████",
		"░░░         ░░░░░░░░░",
		"",
		"Logged in with Google: user@example.com",
		" /auth",
		"Plan: Gemini Code Assist in Google One AI Pro",
		"",
		" ? for shortcuts",
		"─────────────────────────────────────────",
		" YOLO ctrl+y",
		"",
		" - 1 GEMINI.md file",
		"▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀",
		"*   Type your message or @path/to/file",
		"▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄",
		" /workspace/tmp    sandbox    /model Auto",
		" (4aa08788a4*)           (Gemini 3)",
	}, "\n")

	result := Analyze("", plain)

	if result.FrontEnd != "gemini" {
		t.Errorf("expected frontend gemini, got %q", result.FrontEnd)
	}

	hasHeader := false
	hasFooter := false
	hasPrompt := false
	for _, s := range result.Sections {
		switch s.Type {
		case SectionHeader:
			hasHeader = true
		case SectionFooter:
			hasFooter = true
		case SectionUserPrompt:
			hasPrompt = true
		}
	}
	if !hasHeader {
		t.Error("expected SectionHeader")
	}
	if !hasFooter {
		t.Error("expected SectionFooter")
	}
	if !hasPrompt {
		t.Error("expected SectionUserPrompt")
	}
}

func TestAnalyze_CopilotWelcome(t *testing.T) {
	plain := strings.Join([]string{
		"╭──────────────────────────────────────────────────────────────────╮",
		"│  ╭─╮╭─╮                                                        │",
		"│  ╰─╯╰─╯  GitHub Copilot v0.0.414                                │",
		"│  █ ▘▝ █  Describe a task to get started.                         │",
		"│   ▔▔▔▔                                                           │",
		"│  Tip: /resume Switch to a different session                      │",
		"│  Copilot uses AI, so always check for mistakes.                  │",
		"╰──────────────────────────────────────────────────────────────────╯",
		"",
		"❯ Type @ to mention files, / for commands, or ? for shortcuts",
		"",
		" v1.0.2 available · run /update · shift+tab switch mode · ctrl+q enqueue ",
		" Remaining reqs.: 99.6%",
	}, "\n")

	result := Analyze("", plain)

	if result.FrontEnd != "copilot" {
		t.Errorf("expected frontend copilot, got %q", result.FrontEnd)
	}

	hasHeader := false
	hasFooter := false
	for _, s := range result.Sections {
		switch s.Type {
		case SectionHeader:
			hasHeader = true
		case SectionFooter:
			hasFooter = true
		}
	}
	if !hasHeader {
		t.Error("expected SectionHeader")
	}
	if !hasFooter {
		t.Error("expected SectionFooter")
	}
}

func TestAnalyze_CodexWithOutput(t *testing.T) {
	plain := strings.Join([]string{
		"› 남은 작업을 우선순위대로 하나씩 처리하자.",
		"",
		"• 남은 항목 1개를 우선 처리하겠습니다.",
		"• Explored",
		"  └ Read PRESUBMIT.py",
		"────────────────────────────────────────────────",
		"• 현재 업로드 단계는 경고만 출력하고 실행은 안 하고 있습니다.",
		"• Edited PRESUBMIT.py (+27 -5)",
		"• Ran python3 -m py_compile",
		"  └ (no output)",
		"",
		"─ Worked for 1m 07s ───────────────────────────",
		"",
		"• 끝난 항목",
		"  - classfile_kotlin_backport_support 잔여 분기 테스트",
		"  - PRESUBMIT 업로드 단계에서 스모크 게이트를 실제 실행",
		"",
		"1. presubmit upload에서 스모크 실행 시간을 줄이기 위한 선택적 경량 모드",
		"2. completion gate와 smoke gate의 중복 단계를 정리해 총 수행 시간 최적화",
		"",
		"›",
		"",
		"  gpt-5.3-codex high · 36% left · /workspace/r8",
	}, "\n")

	result := AnalyzeWithHint("codex", "", plain)

	if result.FrontEnd != "codex" {
		t.Errorf("expected frontend codex, got %q", result.FrontEnd)
	}

	// Should detect: assistant output, spinner, user prompt, footer
	sections := result.Sections
	if len(sections) < 3 {
		t.Errorf("expected at least 3 sections, got %d", len(sections))
	}

	// Last section should be footer or prompt
	last := sections[len(sections)-1]
	if last.Type != SectionFooter && last.Type != SectionUserPrompt {
		t.Errorf("expected last section to be footer or prompt, got %s", last.Type)
	}
}

func TestAnalyze_EmptyCapture(t *testing.T) {
	result := Analyze("", "")
	if len(result.Sections) != 0 {
		t.Errorf("expected 0 sections for empty capture, got %d", len(result.Sections))
	}
	if result.FrontEnd != "" {
		t.Errorf("expected empty frontend for empty capture, got %q", result.FrontEnd)
	}
}

func TestAnalyze_SpinnerDetection(t *testing.T) {
	tests := []struct {
		line     string
		expected SectionType
	}{
		{"✻ Fermenting…", SectionSpinner},
		{"✻ Brewed for 2m 20s", SectionSpinner},
		{"◦ Working (19m 08s · esc to interrupt)", SectionSpinner},
		{"─ Worked for 1m 07s ─────────────────────", SectionSpinner},
		{"Running…", SectionSpinner},
		{"Thinking", SectionSpinner},
	}

	for _, tc := range tests {
		result := Analyze("", tc.line)
		if len(result.Sections) == 0 {
			t.Errorf("no sections detected for %q", tc.line)
			continue
		}
		if result.Sections[0].Type != tc.expected {
			t.Errorf("for %q: expected %s, got %s", tc.line, tc.expected, result.Sections[0].Type)
		}
	}
}

func TestAnalyze_FooterDetection(t *testing.T) {
	tests := []struct {
		line     string
		expected SectionType
	}{
		{"  gpt-5.4 medium · 100% left · /workspac…", SectionFooter},
		{"  esc to interrupt", SectionFooter},
		{"  ? for shortcuts", SectionFooter},
		{" shift+tab switch mode · ctrl+q enqueue ", SectionFooter},
		{" Remaining reqs.: 99.6%", SectionFooter},
	}

	for _, tc := range tests {
		result := Analyze("", tc.line)
		if len(result.Sections) == 0 {
			t.Errorf("no sections detected for %q", tc.line)
			continue
		}
		if result.Sections[0].Type != tc.expected {
			t.Errorf("for %q: expected %s, got %s", tc.line, tc.expected, result.Sections[0].Type)
		}
	}
}

func TestSectionByType(t *testing.T) {
	result := AnalysisResult{
		Sections: []Section{
			{Type: SectionHeader, StartLine: 0, EndLine: 3},
			{Type: SectionAssistantOutput, StartLine: 4, EndLine: 10},
			{Type: SectionFooter, StartLine: 11, EndLine: 12},
		},
	}

	headers := result.SectionByType(SectionHeader)
	if len(headers) != 1 {
		t.Errorf("expected 1 header, got %d", len(headers))
	}

	footers := result.SectionByType(SectionFooter)
	if len(footers) != 1 {
		t.Errorf("expected 1 footer, got %d", len(footers))
	}

	spinner := result.FirstSectionByType(SectionSpinner)
	if spinner != nil {
		t.Errorf("expected nil for spinner, got %+v", spinner)
	}

	lastFooter := result.LastSectionByType(SectionFooter)
	if lastFooter == nil || lastFooter.StartLine != 11 {
		t.Errorf("expected last footer at line 11, got %+v", lastFooter)
	}
}

func TestSectionLineCount(t *testing.T) {
	s := Section{StartLine: 3, EndLine: 7}
	if s.LineCount() != 5 {
		t.Errorf("expected LineCount=5, got %d", s.LineCount())
	}
}

func TestSectionPlainText(t *testing.T) {
	s := Section{PlainLines: []string{"hello", "world"}}
	if s.PlainText() != "hello\nworld" {
		t.Errorf("expected 'hello\\nworld', got %q", s.PlainText())
	}
}
