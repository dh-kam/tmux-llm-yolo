package prompt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type corpusManifest struct {
	Sessions []struct {
		Session string `json:"session"`
		Files   []struct {
			Index int    `json:"index"`
			ANSI  string `json:"ansi"`
			Plain string `json:"plain"`
		} `json:"files"`
	} `json:"sessions"`
}

func TestAnalyzeLiveCaptureCorpus(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	baseDir := filepath.Dir(filename)
	foundManifest := false
	for _, corpusRoot := range []string{"live-captures", "live-captures-codex"} {
		liveDir := filepath.Join(baseDir, "..", "..", "testdata", corpusRoot)
		entries, err := os.ReadDir(liveDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("read live capture dir failed: %v", err)
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			manifestPath := filepath.Join(liveDir, entry.Name(), "manifest.json")
			rawManifest, err := os.ReadFile(manifestPath)
			if err != nil {
				continue
			}
			foundManifest = true

			var manifest corpusManifest
			if err := json.Unmarshal(rawManifest, &manifest); err != nil {
				t.Fatalf("parse manifest failed (%s): %v", manifestPath, err)
			}

			for _, session := range manifest.Sessions {
				for _, sample := range session.Files {
					ansiPath := filepath.Join(liveDir, entry.Name(), sample.ANSI)
					plainPath := filepath.Join(liveDir, entry.Name(), sample.Plain)
					ansiRaw, err := os.ReadFile(ansiPath)
					if err != nil {
						t.Fatalf("read ansi failed (%s): %v", ansiPath, err)
					}
					plainRaw, err := os.ReadFile(plainPath)
					if err != nil {
						t.Fatalf("read plain failed (%s): %v", plainPath, err)
					}

					analysis := Analyze(string(ansiRaw), string(plainRaw))
					if strings.TrimSpace(analysis.OutputBlock) == "" && !analysis.PromptDetected {
						t.Fatalf("%s sample %d: expected prompt or output block, got neither", session.Session, sample.Index)
					}
					if analysis.Classification == "" {
						t.Fatalf("%s sample %d: classification should not be empty", session.Session, sample.Index)
					}
				}
			}
		}
	}

	if !foundManifest {
		t.Skip("no live capture manifest found")
	}
}

func TestAnalyzeProviderSpecificFixtures(t *testing.T) {
	base := filepath.Join("..", "..", "testdata", "live-captures", "20260309-130340")
	tests := []struct {
		name            string
		ansi            string
		plain           string
		wantProvider    string
		wantProcessing  bool
		wantAssistantUI bool
		wantClass       Classification
	}{
		{
			name:            "codex live processing",
			ansi:            filepath.Join("..", "..", "testdata", "live-captures-codex", "20260309-131605", "tmp-codex", "ansi", "010.ansi.txt"),
			plain:           filepath.Join("..", "..", "testdata", "live-captures-codex", "20260309-131605", "tmp-codex", "plain", "010.plain.txt"),
			wantProvider:    "codex",
			wantProcessing:  true,
			wantAssistantUI: true,
			wantClass:       ClassUnknownWaiting,
		},
		{
			name:            "codex startup prompt",
			ansi:            filepath.Join(base, "tmp-codex", "ansi", "030.ansi.txt"),
			plain:           filepath.Join(base, "tmp-codex", "plain", "030.plain.txt"),
			wantProvider:    "codex",
			wantProcessing:  false,
			wantAssistantUI: true,
			wantClass:       ClassUnknownWaiting,
		},
		{
			name:            "gemini processing",
			ansi:            filepath.Join(base, "tmp-gemini", "ansi", "030.ansi.txt"),
			plain:           filepath.Join(base, "tmp-gemini", "plain", "030.plain.txt"),
			wantProvider:    "gemini",
			wantProcessing:  true,
			wantAssistantUI: true,
			wantClass:       ClassUnknownWaiting,
		},
		{
			name:            "gemini idle prompt after tool output",
			ansi:            filepath.Join("..", "..", "testdata", "live-integration", "20260309-133824", "tmp-gemini", "ansi", "017.ansi.txt"),
			plain:           filepath.Join("..", "..", "testdata", "live-integration", "20260309-133824", "tmp-gemini", "plain", "017.plain.txt"),
			wantProvider:    "gemini",
			wantProcessing:  false,
			wantAssistantUI: true,
			wantClass:       ClassFreeTextRequest,
		},
		{
			name:            "glm processing",
			ansi:            filepath.Join(base, "tmp-glm", "ansi", "060.ansi.txt"),
			plain:           filepath.Join(base, "tmp-glm", "plain", "060.plain.txt"),
			wantProvider:    "glm",
			wantProcessing:  true,
			wantAssistantUI: true,
			wantClass:       ClassUnknownWaiting,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ansiRaw, err := os.ReadFile(tc.ansi)
			if err != nil {
				t.Fatalf("read ansi failed: %v", err)
			}
			plainRaw, err := os.ReadFile(tc.plain)
			if err != nil {
				t.Fatalf("read plain failed: %v", err)
			}

			analysis := Analyze(string(ansiRaw), string(plainRaw))
			if analysis.Provider != tc.wantProvider {
				t.Fatalf("provider=%q want %q", analysis.Provider, tc.wantProvider)
			}
			if !analysis.PromptDetected {
				t.Fatalf("PromptDetected=false want true")
			}
			if tc.wantClass == ClassFreeTextRequest && !analysis.PromptActive {
				t.Fatalf("PromptActive=false want true")
			}
			if analysis.Processing != tc.wantProcessing {
				t.Fatalf("processing=%v want %v", analysis.Processing, tc.wantProcessing)
			}
			if analysis.AssistantUI != tc.wantAssistantUI {
				t.Fatalf("assistantUI=%v want %v", analysis.AssistantUI, tc.wantAssistantUI)
			}
			if analysis.Classification != tc.wantClass {
				t.Fatalf("classification=%q want %q", analysis.Classification, tc.wantClass)
			}
		})
	}
}

func TestAnalyzeDoesNotTreatBareShellPromptAsAssistantContinue(t *testing.T) {
	analysis := Analyze("build smoke test\n> continue\n", "build smoke test\n> continue\n")
	if analysis.AssistantUI {
		t.Fatalf("assistantUI=true want false")
	}
	if analysis.Classification == ClassContinueAfterDone {
		t.Fatalf("bare shell prompt should not be classified as continue_after_completion")
	}
}

func TestAnalyzeCodexWrappedPromptIsNotProcessingKeywordFalsePositive(t *testing.T) {
	ansiPath := filepath.Join("..", "..", "testdata", "live-integration", "20260309-234051", "tmp-codex", "ansi", "001.ansi.txt")
	plainPath := filepath.Join("..", "..", "testdata", "live-integration", "20260309-234051", "tmp-codex", "plain", "001.plain.txt")

	ansiRaw, err := os.ReadFile(ansiPath)
	if err != nil {
		t.Fatalf("read ansi failed: %v", err)
	}
	plainRaw, err := os.ReadFile(plainPath)
	if err != nil {
		t.Fatalf("read plain failed: %v", err)
	}

	analysis := Analyze(string(ansiRaw), string(plainRaw))
	if analysis.Processing {
		t.Fatalf("processing=true want false")
	}
	if !analysis.PromptActive {
		t.Fatalf("promptActive=false want true")
	}
}

func TestAnalyzeWithHintRecoversGLMConfirmationUI(t *testing.T) {
	ansiPath := filepath.Join("..", "..", "testdata", "live-integration", "20260309-134218", "tmp-glm", "ansi", "190.ansi.txt")
	plainPath := filepath.Join("..", "..", "testdata", "live-integration", "20260309-134218", "tmp-glm", "plain", "190.plain.txt")

	ansiRaw, err := os.ReadFile(ansiPath)
	if err != nil {
		t.Fatalf("read ansi failed: %v", err)
	}
	plainRaw, err := os.ReadFile(plainPath)
	if err != nil {
		t.Fatalf("read plain failed: %v", err)
	}

	analysis := AnalyzeWithHint("glm", string(ansiRaw), string(plainRaw))
	if !analysis.AssistantUI {
		t.Fatalf("assistantUI=false want true")
	}
	if analysis.Classification != ClassNumberedMultipleChoice {
		t.Fatalf("classification=%q want %q", analysis.Classification, ClassNumberedMultipleChoice)
	}
	if analysis.RecommendedChoice != "1" {
		t.Fatalf("recommended choice=%q want 1", analysis.RecommendedChoice)
	}
}
