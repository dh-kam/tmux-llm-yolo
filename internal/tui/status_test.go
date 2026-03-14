package tui

import (
	"strings"
	"testing"

	"github.com/dh-kam/tmux-llm-yolo/internal/buildinfo"
)

func TestWaitProgressIndex(t *testing.T) {
	cases := map[string]int{
		"monitoring":              0,
		"suspect_waiting_stage_1": 1,
		"suspect_waiting_stage_2": 2,
		"suspect_waiting_stage_3": 3,
		"confident_waiting":       4,
		"interpreting":            4,
		"acting":                  4,
		"completed":               4,
		"stopped":                 4,
		"unknown":                 -1,
	}

	for state, want := range cases {
		if got := waitProgressIndex(state); got != want {
			t.Fatalf("state %q index=%d want %d", state, got, want)
		}
	}
}

func TestGridColumnCountIsResponsive(t *testing.T) {
	if got := gridColumnCount(40, 4, 26, 1); got != 1 {
		t.Fatalf("width 40 columns=%d want 1", got)
	}
	if got := gridColumnCount(90, 4, 26, 1); got != 2 {
		t.Fatalf("width 90 columns=%d want 2", got)
	}
	if got := gridColumnCount(25, 4, 26, 1); got != 1 {
		t.Fatalf("width 25 columns=%d want 1", got)
	}
}

func TestViewFillsTerminalHeightAndShowsLatestLogs(t *testing.T) {
	m := model{
		width:  40,
		height: 18,
		snapshot: Snapshot{
			Target:      "tmp",
			State:       "monitoring",
			Scope:       "session=tmp | mode=watch",
			Policy:      "wait=4s->4s->4s->4s",
			CurrentTask: "SleepTask",
			CurrentDesc: "waiting",
			NextTask:    "CaptureTask",
			LLMStatus:   "primary=glm:ready",
			LastEvent:   "event text",
			LogLines:    []string{"log 1", "log 2", "log 3", "log 4", "log 5"},
		},
	}

	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 18 {
		t.Fatalf("line count=%d want 18", len(lines))
	}
	if !strings.Contains(view, "log 5") {
		t.Fatalf("view does not contain latest log: %q", view)
	}
	if !strings.Contains(view, buildinfo.AppName) {
		t.Fatalf("view does not contain app name: %q", view)
	}
	if !strings.Contains(view, buildinfo.Version) {
		t.Fatalf("view does not contain version: %q", view)
	}
	if !strings.Contains(view, "tmp") {
		t.Fatalf("view does not contain target: %q", view)
	}
	if strings.Contains(view, "target tmp") {
		t.Fatalf("view still contains old target line: %q", view)
	}
	if !strings.Contains(view, "updated 00:00:00") {
		t.Fatalf("view does not contain updated time in header: %q", view)
	}
	if !strings.Contains(view, "Activity Log") {
		t.Fatalf("view does not contain log panel title: %q", view)
	}
	if strings.Contains(view, "Session") || strings.Contains(view, "Flow") || strings.Contains(view, "Control") || strings.Contains(view, "Signals") {
		t.Fatalf("single-column view should omit card titles: %q", view)
	}
}

func TestViewPadsBlankLogLinesWhenLogsAreShort(t *testing.T) {
	m := model{
		width:  30,
		height: 20,
		snapshot: Snapshot{
			State:    "monitoring",
			LogLines: []string{"only one"},
		},
	}

	lines := strings.Split(m.View(), "\n")
	if len(lines) != 20 {
		t.Fatalf("line count=%d want 20", len(lines))
	}
	if strings.TrimSpace(lines[len(lines)-1]) != "" {
		t.Fatalf("last line=%q want blank padded line", lines[len(lines)-1])
	}
}
