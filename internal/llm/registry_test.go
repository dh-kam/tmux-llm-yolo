package llm

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIsProgressCaptureUsingTestdata(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	baseDir := filepath.Dir(filename)
	testdataDir := filepath.Join(baseDir, "..", "..", "testdata")

	type captureFixture struct {
		file       string
		working    bool
		needReason bool
		byProvider map[string]bool
	}

	expectWorking := func(provider string, fixture captureFixture) bool {
		if fixture.byProvider != nil {
			if expected, ok := fixture.byProvider[provider]; ok {
				return expected
			}
		}
		return fixture.working
	}

	captureCases := []captureFixture{
		{file: "r8-codex.working.capture", working: true, needReason: true},
		{file: "r8-codex.working.ansi.capture", working: true, needReason: true},
		{file: "location-glm.vibing.capture", working: true, needReason: true},
		{file: "location-glm.vibing.ansi.capture", working: true, needReason: true},
		{file: "r8-codex.completed.capture", working: false, needReason: false},
		{file: "r8-codex.completed.ansi.capture", working: false, needReason: false},
		{file: "location-glm.completed.capture", working: false, needReason: false},
		{file: "location-glm.completed.ansi.capture", working: false, needReason: false},
		{file: "location-gemini.working.capture", working: true, needReason: true},
		{file: "location-gemini.working.ansi.capture", working: true, needReason: true},
		{file: "location-gemini.working2.capture", working: true, needReason: true},
		{file: "location-gemini.working2.ansi.capture", working: true, needReason: true},
		{file: "location-gemini.working3.capture", working: true, needReason: true},
		{file: "location-gemini.working3.ansi.capture", working: true, needReason: true},
		{file: "location-gemini.completed.capture", working: false, needReason: false},
		{file: "location-gemini.completed.ansi.capture", working: false, needReason: false},
	}
	cases := []struct {
		name  string
		model string
	}{
		{name: "gemini", model: ""},
		{name: "codex", model: ""},
		{name: "copilot", model: ""},
		{name: "glm", model: ""},
		{name: "ollama", model: "glm4:9b"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider, err := New(tc.name, tc.model)
			if err != nil {
				t.Fatalf("provider init failed: %v", err)
			}
			for _, fixture := range captureCases {
				expected := expectWorking(tc.name, fixture)
				raw, err := os.ReadFile(filepath.Join(testdataDir, fixture.file))
				if err != nil {
					cleanPath := filepath.Clean(filepath.Join("testdata", fixture.file))
					t.Fatalf("read testdata failed: %s %v", cleanPath, err)
				}
				isWorking, reason := provider.IsProgressingCapture(NewCaptureFromBytes(raw))
				if isWorking != expected {
					t.Fatalf("expected working=%t for %s with %s", expected, tc.name, fixture.file)
				}
				if expected && fixture.needReason && strings.TrimSpace(reason) == "" {
					t.Fatalf("expected non-empty reason for %s with %s", tc.name, fixture.file)
				}
				captureFromReader, err := NewCaptureFromReader(bytes.NewReader(raw))
				if err != nil {
					t.Fatalf("reader capture failed: %s %v", fixture.file, err)
				}
				workingFromReader, reasonFromReader := provider.IsProgressingCapture(captureFromReader)
				if workingFromReader != expected {
					t.Fatalf("expected working=%t for %s with %s using reader", expected, tc.name, fixture.file)
				}
				if expected && fixture.needReason && strings.TrimSpace(reasonFromReader) == "" {
					t.Fatalf("expected non-empty reason for %s with %s using reader", tc.name, fixture.file)
				}
			}
		})
	}
}
