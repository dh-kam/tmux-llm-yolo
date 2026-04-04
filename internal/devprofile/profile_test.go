package devprofile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapPreferenceMD(t *testing.T) {
	md := bootstrapPreferenceMD("test-session")
	if !strings.Contains(md, "# Developer Preference Profile") {
		t.Fatal("missing header")
	}
	if !strings.Contains(md, "test-session") {
		t.Fatal("missing target")
	}
	if !strings.Contains(md, "## 코딩 스타일") {
		t.Fatal("missing coding style section")
	}
	if !strings.Contains(md, "## 미확인 영역") {
		t.Fatal("missing uncovered areas section")
	}
}

func TestParseSections(t *testing.T) {
	md := `# Title
> meta

## 코딩 스타일
- 에러 처리: defensive [confidence: 0.9]
- 네이밍: domain-driven [confidence: 0.8]

## 아키텍처 선호
- Clean Architecture [confidence: 0.85]

## 관찰 로그 (compaction 전 raw 데이터)

### 2026-04-03 14:20
- 사용자 입력: "fix parser"
`
	sections := parseSections(md)
	if len(sections) < 3 {
		t.Fatalf("expected ≥3 sections, got %d", len(sections))
	}
	style, ok := sections["코딩 스타일"]
	if !ok {
		t.Fatal("missing 코딩 스타일")
	}
	if !strings.Contains(style, "defensive") {
		t.Fatal("missing defensive in style")
	}
}

func TestProfileManagerLoadAndSave(t *testing.T) {
	dir := t.TempDir()
	pm := &ProfileManager{
		baseDir: dir,
		target:  "test",
		logger:  func(string, ...interface{}) {},
	}

	if err := pm.Load(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "test.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# Developer Preference Profile") {
		t.Fatal("bootstrap failed")
	}

	raw := pm.RawContent()
	if raw == "" {
		t.Fatal("empty raw content")
	}
}

func TestAppendObservation(t *testing.T) {
	dir := t.TempDir()
	pm := &ProfileManager{
		baseDir: dir,
		target:  "test",
		logger:  func(string, ...interface{}) {},
	}
	if err := pm.Load(); err != nil {
		t.Fatal(err)
	}

	if err := pm.AppendObservation("- 사용자 입력: \"fix bug\"\n- 맥락: Go 서버"); err != nil {
		t.Fatal(err)
	}

	raw := pm.RawContent()
	if !strings.Contains(raw, "fix bug") {
		t.Fatal("observation not appended")
	}

	// Read from disk
	data, _ := os.ReadFile(filepath.Join(dir, "test.md"))
	if !strings.Contains(string(data), "fix bug") {
		t.Fatal("observation not persisted")
	}
}

func TestSummarizeForContext(t *testing.T) {
	dir := t.TempDir()
	pm := &ProfileManager{
		baseDir: dir,
		target:  "test",
		logger:  func(string, ...interface{}) {},
	}
	if err := pm.Load(); err != nil {
		t.Fatal(err)
	}

	// Initially empty sections → no summary
	summary := pm.SummarizeForContext("output", "prompt")
	if summary != "" {
		t.Fatalf("expected empty summary for new profile, got: %q", summary)
	}

	// Add some preferences
	content := pm.RawContent()
	content = strings.Replace(content, "## 코딩 스타일\n", "## 코딩 스타일\n- 에러 처리: defensive [confidence: 0.9]\n- 테스트: TDD 선호 [confidence: 0.7]\n", 1)
	if err := pm.ReplaceContent(content); err != nil {
		t.Fatal(err)
	}

	summary = pm.SummarizeForContext("test output", "❯")
	if !strings.Contains(summary, "defensive") {
		t.Fatalf("expected defensive in summary, got: %q", summary)
	}
}

func TestUncoveredAreas(t *testing.T) {
	dir := t.TempDir()
	pm := &ProfileManager{
		baseDir: dir,
		target:  "test",
		logger:  func(string, ...interface{}) {},
	}
	if err := pm.Load(); err != nil {
		t.Fatal(err)
	}

	areas := pm.UncoveredAreas()
	if len(areas) == 0 {
		t.Fatal("expected uncovered areas from bootstrap")
	}
	found := false
	for _, a := range areas {
		if strings.Contains(a, "에러 처리") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected '에러 처리' in uncovered areas")
	}
}

func TestReplaceContent(t *testing.T) {
	dir := t.TempDir()
	pm := &ProfileManager{
		baseDir: dir,
		target:  "test",
		logger:  func(string, ...interface{}) {},
	}
	if err := pm.Load(); err != nil {
		t.Fatal(err)
	}

	newContent := "# Developer Preference Profile\n> Target: test\n\n## 코딩 스타일\n- updated\n"
	if err := pm.ReplaceContent(newContent); err != nil {
		t.Fatal(err)
	}

	if pm.RawContent() != newContent {
		t.Fatal("content not replaced")
	}

	data, _ := os.ReadFile(filepath.Join(dir, "test.md"))
	if string(data) != newContent {
		t.Fatal("content not persisted")
	}
}

func TestSanitizeTarget(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"llm-glm", "llm-glm"},
		{"foo/bar", "foo_bar"},
		{"", "default"},
		{"  spaces  ", "__spaces__"},
	}
	for _, tc := range tests {
		got := sanitizeTarget(tc.input)
		if got != tc.want {
			t.Errorf("sanitizeTarget(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
