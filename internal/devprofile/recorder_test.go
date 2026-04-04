package devprofile

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestRecordUserInputAndResponse(t *testing.T) {
	dir := t.TempDir()
	pm := &ProfileManager{
		baseDir: dir,
		target:  "test",
		logger:  func(string, ...interface{}) {},
	}
	if err := pm.Load(); err != nil {
		t.Fatal(err)
	}

	cr := &ConversationRecorder{
		path:    dir + "/test.jsonl",
		profile: pm,
	}

	cr.RecordUserInput("에러 핸들링을 구체적으로 해줘", "Go rate limiter")
	cr.RecordAssistantResponse("에러 메시지에 위치 정보를 추가했습니다.")

	data, err := os.ReadFile(cr.Path())
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "에러 핸들링") {
		t.Fatal("missing user input in JSONL")
	}
	if !strings.Contains(content, "위치 정보") {
		t.Fatal("missing response in JSONL")
	}

	// Verify observation was appended to profile
	raw := pm.RawContent()
	if !strings.Contains(raw, "에러 핸들링") {
		t.Fatal("observation not appended to preference.md")
	}
}

func TestRecordUserAction(t *testing.T) {
	dir := t.TempDir()
	pm := &ProfileManager{
		baseDir: dir,
		target:  "test",
		logger:  func(string, ...interface{}) {},
	}
	_ = pm.Load()

	cr := &ConversationRecorder{
		path:    dir + "/test.jsonl",
		profile: pm,
	}

	cr.RecordUserAction("choice:2")

	data, _ := os.ReadFile(cr.Path())
	if !strings.Contains(string(data), "choice:2") {
		t.Fatal("missing action in JSONL")
	}
}

func TestGetSince(t *testing.T) {
	dir := t.TempDir()
	cr := &ConversationRecorder{
		path:    dir + "/test.jsonl",
		profile: nil, // no profile — skip observation
	}

	before := time.Now().Add(-1 * time.Second)
	cr.RecordUserInput("first", "")
	cr.RecordAssistantResponse("response1")

	time.Sleep(10 * time.Millisecond)
	after := time.Now()

	cr.RecordUserInput("second", "")
	cr.RecordAssistantResponse("response2")

	all := cr.GetSince(before)
	if len(all) < 2 {
		t.Fatalf("expected ≥2 turns since before, got %d", len(all))
	}

	recent := cr.GetSince(after)
	if len(recent) < 1 {
		t.Fatalf("expected ≥1 turn since after, got %d", len(recent))
	}
}

func TestTrimResponse(t *testing.T) {
	short := "hello"
	if trimResponse(short) != "hello" {
		t.Fatal("short string changed")
	}

	long := strings.Repeat("x", 600)
	trimmed := trimResponse(long)
	if len(trimmed) > 504 { // 500 + "..."
		t.Fatalf("expected ≤504 chars, got %d", len(trimmed))
	}
	if !strings.HasSuffix(trimmed, "...") {
		t.Fatal("expected ... suffix")
	}
}
