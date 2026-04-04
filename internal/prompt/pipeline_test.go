package prompt

import "testing"

func TestDefaultAnalysisExpertsOrder(t *testing.T) {
	experts := defaultAnalysisExperts()
	var names []string
	for _, expert := range experts {
		names = append(names, expert.Name())
	}
	want := []string{
		"provider",
		"prompt_line",
		"prompt_state",
		"assistant_ui",
		"output_block",
		"processing",
		"classification",
		"interactive_prompt",
		"footer_key_hints",
	}
	if len(names) != len(want) {
		t.Fatalf("expert count=%d want %d (%v)", len(names), len(want), names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("expert[%d]=%q want %q (all=%v)", i, names[i], want[i], names)
		}
	}
}

func TestExtractFooterKeyHints(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  map[string]string
	}{
		{
			name:  "copilot_footer_basic",
			lines: []string{"shift+tab switch mode · ctrl+s run command"},
			want:  map[string]string{"ctrl+s": "run command", "shift+tab": "switch mode"},
		},
		{
			name:  "copilot_footer_with_version",
			lines: []string{"v1.0.17 available · run /update · shift+tab switch mode · ctrl+q enqueue"},
			want:  map[string]string{"shift+tab": "switch mode", "ctrl+q": "enqueue"},
		},
		{
			name:  "no_hints",
			lines: []string{"plain text without any key hints"},
			want:  nil,
		},
		{
			name:  "empty_lines",
			lines: []string{"", "  "},
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFooterKeyHints(tt.lines)
			if tt.want == nil {
				if got != nil {
					t.Fatalf("got %v, want nil", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d hints %v, want %d %v", len(got), got, len(tt.want), tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("hint[%q]=%q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestAnalyzePipelineFallsBackToHintedProvider(t *testing.T) {
	analysis := AnalyzeWithHintAndWidth("glm", "plain ansi-free text", "some output\n>", 0)
	if analysis.Provider != "glm" {
		t.Fatalf("Provider=%q want glm", analysis.Provider)
	}
	if !analysis.PromptDetected {
		t.Fatal("PromptDetected=false want true")
	}
}
