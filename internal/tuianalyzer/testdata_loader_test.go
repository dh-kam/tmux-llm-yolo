package tuianalyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadFixture reads a plain text capture from testdata/live/<name>.plain.txt.
// Name format: "claude-code-idle", "codex-approval", "copilot-prompt", "gemini-working", etc.
func loadFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", "live", name+".plain.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	return string(data)
}

// loadFixtureLines returns the capture split into lines.
func loadFixtureLines(t *testing.T, name string) []string {
	t.Helper()
	return strings.Split(loadFixture(t, name), "\n")
}

func TestFixtureLoaderSmokeTest(t *testing.T) {
	fixtures := []string{
		"claude-code-idle",
		"claude-code-working",
		"codex-approval",
		"codex-working",
		"copilot-prompt",
		"gemini-idle",
		"gemini-working",
		"shell-plain",
	}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			content := loadFixture(t, name)
			if len(content) == 0 {
				t.Fatalf("empty fixture: %s", name)
			}
			lines := strings.Split(content, "\n")
			t.Logf("%s: %d lines, %d bytes", name, len(lines), len(content))
		})
	}
}
