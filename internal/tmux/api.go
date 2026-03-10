package tmux

import "context"

type Session struct {
	Name string
}

type API interface {
	ListSessions(ctx context.Context) ([]Session, error)
	HasSession(ctx context.Context, session string) (bool, error)
	CapturePane(ctx context.Context, target string, lines int, includeANSI bool) (string, error)
	PaneSize(ctx context.Context, target string) (width int, height int, err error)
	SendKeys(ctx context.Context, target string, keys ...string) error
	IsPaneInMode(ctx context.Context, target string) (bool, error)
	CreateSession(ctx context.Context, name string) error
	AttachSession(ctx context.Context, name string) error
	KillSession(ctx context.Context, name string) error
}
