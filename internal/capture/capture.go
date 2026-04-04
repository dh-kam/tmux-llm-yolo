package capture

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/dh-kam/yollo/internal/tmux"
)

var ansiEscapePattern = regexp.MustCompile(`\x1b(?:\[[0-9;?]*[ -/]*[@-~]|][^\a]*(?:\a|\x1b\\))`)

type Snapshot struct {
	ANSI    string
	Plain   string
	TakenAt time.Time
}

type Fetcher struct {
	Client tmux.API
}

func (f Fetcher) CaptureDual(ctx context.Context, target string, lines int) (Snapshot, error) {
	ansi, err := f.Client.CapturePane(ctx, target, lines, true)
	if err != nil {
		return Snapshot{}, err
	}
	plain, err := f.Client.CapturePane(ctx, target, lines, false)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		ANSI:    normalize(ansi),
		Plain:   normalize(plain),
		TakenAt: time.Now(),
	}, nil
}

func (f Fetcher) CaptureANSI(ctx context.Context, target string, lines int) (string, error) {
	ansi, err := f.Client.CapturePane(ctx, target, lines, true)
	if err != nil {
		return "", err
	}
	return normalize(ansi), nil
}

func StripANSI(value string) string {
	return ansiEscapePattern.ReplaceAllString(value, "")
}

func normalize(value string) string {
	return strings.TrimRight(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
}
