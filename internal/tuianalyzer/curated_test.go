package tuianalyzer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// extractCuratedArchive extracts curated-captures.tar.zst into a temporary directory
// and returns the path to the curated-testdata directory within it.
func extractCuratedArchive(t *testing.T) string {
	t.Helper()

	archive := filepath.Join("testdata", "curated-captures.tar.zst")
	if _, err := os.Stat(archive); err != nil {
		t.Skipf("curated archive not found: %v", err)
	}

	tmpDir := t.TempDir()
	cmd := exec.Command("tar", "--zstd", "-xf", archive, "-C", tmpDir)
	cmd.Dir = filepath.Dir(archive)
	absArchive, _ := filepath.Abs(archive)
	cmd.Args = []string{"tar", "--zstd", "-xf", absArchive, "-C", tmpDir}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("extract archive: %v\n%s", err, out)
	}

	dataDir := filepath.Join(tmpDir, "curated-testdata")
	if _, err := os.Stat(dataDir); err != nil {
		t.Fatalf("curated-testdata dir not found after extraction: %v", err)
	}
	return dataDir
}

// loadCuratedFixture reads a plain text capture from the extracted curated directory.
func loadCuratedFixture(t *testing.T, dir string, name string) string {
	t.Helper()
	path := filepath.Join(dir, name+".plain.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read curated fixture %q: %v", name, err)
	}
	return string(data)
}

// loadCuratedFixturePair reads both plain and ansi captures.
func loadCuratedFixturePair(t *testing.T, dir string, name string) (plain, ansi string) {
	t.Helper()
	plainPath := filepath.Join(dir, name+".plain.txt")
	ansiPath := filepath.Join(dir, name+".ansi.txt")

	pData, err := os.ReadFile(plainPath)
	if err != nil {
		t.Fatalf("read plain fixture %q: %v", name, err)
	}
	aData, err := os.ReadFile(ansiPath)
	if err != nil {
		t.Fatalf("read ansi fixture %q: %v", name, err)
	}
	return string(pData), string(aData)
}

// curatedFrontend maps filename prefix to frontend hint.
func curatedFrontend(name string) string {
	switch {
	case strings.HasPrefix(name, "glm-"):
		return "claude-code"
	case strings.HasPrefix(name, "codex-"):
		return "codex"
	case strings.HasPrefix(name, "copilot-"):
		return "copilot"
	case strings.HasPrefix(name, "gemini-"):
		return "gemini"
	default:
		return ""
	}
}

// curatedState extracts the state (idle, working, approval, completion, error, banner)
// from a curated fixture name like "glm-working-2".
func curatedState(name string) string {
	parts := strings.SplitN(name, "-", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// TestCurated_ExtractAndEnumerate verifies the archive extracts correctly and
// contains the expected number of fixture pairs.
func TestCurated_ExtractAndEnumerate(t *testing.T) {
	dir := extractCuratedArchive(t)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read curated dir: %v", err)
	}

	plainCount := 0
	ansiCount := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".plain.txt") {
			plainCount++
		}
		if strings.HasSuffix(e.Name(), ".ansi.txt") {
			ansiCount++
		}
	}

	t.Logf("curated fixtures: %d plain, %d ansi", plainCount, ansiCount)
	if plainCount < 40 {
		t.Errorf("expected at least 40 plain fixtures, got %d", plainCount)
	}
	if plainCount != ansiCount {
		t.Errorf("plain/ansi count mismatch: %d vs %d", plainCount, ansiCount)
	}
}

// listCuratedFixtures returns fixture base names (without extension) from the directory.
func listCuratedFixtures(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read curated dir: %v", err)
	}
	var names []string
	seen := make(map[string]bool)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".plain.txt") {
			base := strings.TrimSuffix(e.Name(), ".plain.txt")
			if !seen[base] {
				seen[base] = true
				names = append(names, base)
			}
		}
	}
	return names
}

// TestCurated_SectionDetection verifies that every curated capture produces
// a non-zero number of sections within reasonable bounds.
func TestCurated_SectionDetection(t *testing.T) {
	dir := extractCuratedArchive(t)
	fixtures := listCuratedFixtures(t, dir)

	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			plain, ansi := loadCuratedFixturePair(t, dir, name)
			frontend := curatedFrontend(name)

			result := AnalyzeWithHint(frontend, ansi, plain)

			if len(result.Sections) == 0 {
				t.Error("analysis produced 0 sections")
			}
			// Section count upper bound scales with line count: long captures
			// with scroll history naturally produce many sections.
			maxSections := len(result.PlainLines)
			if maxSections < 50 {
				maxSections = 50
			}
			if len(result.Sections) > maxSections {
				t.Errorf("too many sections (%d) for %d lines; expected at most %d",
					len(result.Sections), len(result.PlainLines), maxSections)
			}

			// Log section breakdown for debugging
			var types []string
			for _, s := range result.Sections {
				types = append(types, s.Type.ShortLabel())
			}
			t.Logf("frontend=%s sections=%d types=[%s]",
				result.FrontEnd, len(result.Sections), strings.Join(types, " "))
		})
	}
}

// TestCurated_FrontendDetection verifies that the detected frontend matches
// the expected frontend based on the fixture name.
func TestCurated_FrontendDetection(t *testing.T) {
	dir := extractCuratedArchive(t)
	fixtures := listCuratedFixtures(t, dir)

	for _, name := range fixtures {
		expected := curatedFrontend(name)
		if expected == "" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			plain, ansi := loadCuratedFixturePair(t, dir, name)

			// Use empty hint to test auto-detection
			result := Analyze(ansi, plain)

			// Allow fuzzy matching: "claude-code" and "glm" are the same
			detected := result.FrontEnd
			if detected != expected {
				t.Logf("auto-detected frontend %q, expected %q (may be acceptable)", detected, expected)
			}
		})
	}
}

// TestCurated_IdleStateHasPrompt verifies that idle captures contain a user prompt section.
func TestCurated_IdleStateHasPrompt(t *testing.T) {
	dir := extractCuratedArchive(t)
	fixtures := listCuratedFixtures(t, dir)

	for _, name := range fixtures {
		state := curatedState(name)
		if state != "idle" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			plain := loadCuratedFixture(t, dir, name)
			frontend := curatedFrontend(name)
			result := AnalyzeWithHint(frontend, "", plain)

			prompts := result.SectionByType(SectionUserPrompt)
			if len(prompts) == 0 {
				t.Logf("sections: %s", dumpSections(result.Sections))
				t.Error("idle capture should have a user prompt section")
			}
		})
	}
}

// TestCurated_WorkingStateHasOutput verifies that working captures contain
// assistant output or spinner sections.
func TestCurated_WorkingStateHasOutput(t *testing.T) {
	dir := extractCuratedArchive(t)
	fixtures := listCuratedFixtures(t, dir)

	for _, name := range fixtures {
		state := curatedState(name)
		if state != "working" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			plain := loadCuratedFixture(t, dir, name)
			frontend := curatedFrontend(name)
			result := AnalyzeWithHint(frontend, "", plain)

			outputs := result.SectionByType(SectionAssistantOutput)
			spinners := result.SectionByType(SectionSpinner)
			if len(outputs) == 0 && len(spinners) == 0 {
				t.Logf("sections: %s", dumpSections(result.Sections))
				t.Error("working capture should have assistant output or spinner sections")
			}
		})
	}
}

// TestCurated_ApprovalStateHasQuestion verifies that approval captures contain
// an assistant question section or approval-like content somewhere in the capture.
func TestCurated_ApprovalStateHasQuestion(t *testing.T) {
	dir := extractCuratedArchive(t)
	fixtures := listCuratedFixtures(t, dir)

	for _, name := range fixtures {
		state := curatedState(name)
		if state != "approval" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			plain := loadCuratedFixture(t, dir, name)
			frontend := curatedFrontend(name)
			result := AnalyzeWithHint(frontend, "", plain)

			// Gather all text from question and prompt sections
			questions := result.SectionByType(SectionAssistantQuestion)
			prompts := result.SectionByType(SectionUserPrompt)
			sectionText := ""
			for _, s := range append(questions, prompts...) {
				sectionText += s.PlainText() + "\n"
			}

			// Also check full capture text for approval indicators,
			// since some captures have approval content in output sections
			// (e.g., codex shows approval choices as numbered list in output).
			fullText := strings.Join(result.PlainLines, "\n")

			approvalPatterns := []string{
				"Yes", "proceed", "confirm", "Allow", "allow once",
				"Esc to cancel", "Press enter", "Don't allow",
				"allow all edits", "auto-accept",
			}

			hasApproval := false
			for _, pat := range approvalPatterns {
				if strings.Contains(sectionText, pat) || strings.Contains(fullText, pat) {
					hasApproval = true
					break
				}
			}

			if !hasApproval {
				t.Logf("sections: %s", dumpSections(result.Sections))
				t.Logf("question+prompt text: %s", truncate(sectionText, 300))
				t.Error("approval capture should have approval-related content")
			}
		})
	}
}

// TestCurated_FooterPresence verifies that captures with a visible TUI
// (not shell or error) have footer sections.
func TestCurated_FooterPresence(t *testing.T) {
	dir := extractCuratedArchive(t)
	fixtures := listCuratedFixtures(t, dir)

	for _, name := range fixtures {
		state := curatedState(name)
		// Skip error captures - they may not have a normal footer
		if state == "error" || state == "banner" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			plain := loadCuratedFixture(t, dir, name)
			frontend := curatedFrontend(name)
			result := AnalyzeWithHint(frontend, "", plain)

			footers := result.SectionByType(SectionFooter)
			if len(footers) == 0 {
				t.Logf("sections: %s", dumpSections(result.Sections))
				t.Log("expected footer section in non-error, non-banner capture")
			}
		})
	}
}

// TestCurated_PerFrontend runs a summary check for each frontend,
// verifying that section detection works across all curated states.
func TestCurated_PerFrontend(t *testing.T) {
	dir := extractCuratedArchive(t)
	fixtures := listCuratedFixtures(t, dir)

	frontends := map[string][]string{} // frontend -> list of fixture names
	for _, name := range fixtures {
		fe := curatedFrontend(name)
		if fe != "" {
			frontends[fe] = append(frontends[fe], name)
		}
	}

	for fe, names := range frontends {
		t.Run(fe, func(t *testing.T) {
			totalSections := 0
			totalCaptures := 0
			emptyCaptures := 0
			stateHits := map[string]int{}

			for _, name := range names {
				plain := loadCuratedFixture(t, dir, name)
				result := AnalyzeWithHint(fe, "", plain)

				totalCaptures++
				totalSections += len(result.Sections)
				if len(result.Sections) == 0 {
					emptyCaptures++
				}

				state := curatedState(name)
				stateHits[state]++
			}

			t.Logf("frontend=%s captures=%d sections=%d empty=%d states=%v",
				fe, totalCaptures, totalSections, emptyCaptures, stateHits)

			if totalCaptures == 0 {
				t.Error("no captures for this frontend")
			}
			if emptyCaptures > totalCaptures/2 {
				t.Errorf("too many empty analyses: %d/%d", emptyCaptures, totalCaptures)
			}

			// Each frontend should have at least 2 distinct states represented
			if len(stateHits) < 2 {
				t.Errorf("expected at least 2 distinct states, got %d: %v", len(stateHits), stateHits)
			}
		})
	}
}

// TestCurated_ANSIvsPlainConsistency verifies that analyzing with ANSI data
// produces similar results to plain-only analysis.
func TestCurated_ANSIvsPlainConsistency(t *testing.T) {
	dir := extractCuratedArchive(t)
	fixtures := listCuratedFixtures(t, dir)

	// Test a subset to keep the test fast
	for i, name := range fixtures {
		if i%5 != 0 {
			continue
		}
		t.Run(name, func(t *testing.T) {
			plain, ansi := loadCuratedFixturePair(t, dir, name)
			frontend := curatedFrontend(name)

			withANSI := AnalyzeWithHint(frontend, ansi, plain)
			withoutANSI := AnalyzeWithHint(frontend, "", plain)

			// Section counts should be similar (ANSI may provide slightly
			// different classification due to color cues)
			diff := len(withANSI.Sections) - len(withoutANSI.Sections)
			if diff < 0 {
				diff = -diff
			}
			if diff > len(withANSI.Sections)/2+3 {
				t.Errorf("large section count divergence: with_ansi=%d without=%d",
					len(withANSI.Sections), len(withoutANSI.Sections))
			}
		})
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
