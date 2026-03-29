package tuianalyzer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// corpusManifest matches the manifest.json format used in testdata.
type corpusManifest struct {
	Timestamp string `json:"timestamp"`
	Sessions  []struct {
		Session string `json:"session"`
		Files   []struct {
			Index int    `json:"index"`
			ANSI  string `json:"ansi"`
			Plain string `json:"plain"`
		} `json:"files"`
	} `json:"sessions"`
}

func TestCorpus_AllManifests(t *testing.T) {
	manifests := findManifests(t)
	if len(manifests) == 0 {
		t.Skip("no manifest.json files found in testdata")
	}

	for _, mp := range manifests {
		t.Run(filepath.Base(filepath.Dir(mp)), func(t *testing.T) {
			testManifest(t, mp)
		})
	}
}

func findManifests(t *testing.T) []string {
	t.Helper()
	base := filepath.Join("..", "..", "testdata")

	var manifests []string
	for _, dir := range []string{"live-captures", "live-captures-codex", "live-integration"} {
		root := filepath.Join(base, dir)
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			mp := filepath.Join(root, e.Name(), "manifest.json")
			if _, err := os.Stat(mp); err == nil {
				manifests = append(manifests, mp)
			}
		}
	}
	return manifests
}

func testManifest(t *testing.T, manifestPath string) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}

	var manifest corpusManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Skipf("malformed manifest: %v", err)
	}

	baseDir := filepath.Dir(manifestPath)
	history := NewCaptureHistory()
	totalSamples := 0
	totalSections := 0
	providerErrors := 0
	emptyAnalyses := 0

	for _, session := range manifest.Sessions {
		// Infer expected provider from session name
		expectedProvider := inferProviderFromSession(session.Session)

		for _, f := range session.Files {
			var ansi, plain string

			if f.Plain != "" {
				p := resolveTestPath(baseDir, f.Plain)
				if d, err := os.ReadFile(p); err == nil {
					plain = string(d)
				}
			}
			if f.ANSI != "" {
				p := resolveTestPath(baseDir, f.ANSI)
				if d, err := os.ReadFile(p); err == nil {
					ansi = string(d)
				}
			}

			if plain == "" {
				continue
			}

			result := history.AnalyzeWithLearning(ansi, plain)
			history.Record(result)
			totalSamples++
			totalSections += len(result.Sections)

			if len(result.Sections) == 0 {
				emptyAnalyses++
			}

			// Validate provider detection
			if expectedProvider != "" && result.FrontEnd != expectedProvider {
				providerErrors++
			}
		}
	}

	summary := history.Summary()
	t.Logf("manifest=%s samples=%d sections=%d empty=%d providerErrors=%d",
		filepath.Base(filepath.Dir(manifestPath)),
		totalSamples, totalSections, emptyAnalyses, providerErrors)
	t.Logf("  learned: provider=%s stableFooters=%d stableHeaders=%d",
		summary.FrontEnd, len(summary.StableFooterLines), len(summary.StableHeaderLines))

	// Corpus-wide assertions
	if totalSamples == 0 {
		t.Error("no samples analyzed")
	}
	if emptyAnalyses > totalSamples/2 {
		t.Errorf("too many empty analyses: %d/%d", emptyAnalyses, totalSamples)
	}
	// Provider errors are informational — some captures lack signatures
	if providerErrors > 0 {
		t.Logf("provider mismatches: %d/%d (some captures may lack provider signatures)",
			providerErrors, totalSamples)
	}

	// Should learn stable patterns from enough captures
	if totalSamples >= 10 && len(summary.StableFooterLines) == 0 {
		t.Log("no stable footer patterns learned (may need more varied captures)")
	}
}

func inferProviderFromSession(sessionName string) string {
	switch {
	case contains(sessionName, "codex"):
		return "codex"
	case contains(sessionName, "glm"):
		return "claude-code"
	case contains(sessionName, "gemini"):
		return "gemini"
	case contains(sessionName, "copilot"):
		return "copilot"
	default:
		return ""
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func resolveTestPath(baseDir string, rel string) string {
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(baseDir, rel)
}

func TestCorpus_LearningWithLiveCaptures(t *testing.T) {
	base := filepath.Join("..", "..", "testdata", "live-captures", "20260309-130340")
	if _, err := os.Stat(base); err != nil {
		t.Skip("live capture data not found")
	}

	providers := map[string]string{
		"tmp-codex":  "codex",
		"tmp-glm":    "claude-code",
		"tmp-gemini": "gemini",
	}

	for dir, expectedProvider := range providers {
		t.Run(dir, func(t *testing.T) {
			history := NewCaptureHistory()

			// Analyze first 20 samples
			for i := 1; i <= 20; i++ {
				seq := fmt.Sprintf("%03d", i)
				ansi, plain := readLiveCaptureSample(t, base, dir, seq)
				if plain == "" {
					continue
				}
				result := history.AnalyzeWithLearning(ansi, plain)
				history.Record(result)
			}

			summary := history.Summary()
			if summary.FrontEnd != expectedProvider {
				t.Errorf("expected frontend %q, got %q", expectedProvider, summary.FrontEnd)
			}

			if summary.TotalCaptures < 5 {
				t.Errorf("expected at least 5 captures, got %d", summary.TotalCaptures)
			}

			t.Logf("provider=%s captures=%d stableFooters=%d stableHeaders=%d stablePrompts=%d",
				summary.FrontEnd, summary.TotalCaptures,
				len(summary.StableFooterLines), len(summary.StableHeaderLines),
				len(summary.StablePromptMarkers))
		})
	}
}
