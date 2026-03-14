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

func TestAnalyzePipelineFallsBackToHintedProvider(t *testing.T) {
	analysis := AnalyzeWithHintAndWidth("glm", "plain ansi-free text", "some output\n>", 0)
	if analysis.Provider != "glm" {
		t.Fatalf("Provider=%q want glm", analysis.Provider)
	}
	if !analysis.PromptDetected {
		t.Fatal("PromptDetected=false want true")
	}
}
