package tui

import "sync"

const defaultLogBufferLimit = 2000

type LogBuffer struct {
	mu    sync.Mutex
	limit int
	lines []string
}

func NewLogBuffer(limit int) *LogBuffer {
	if limit <= 0 {
		limit = defaultLogBufferLimit
	}
	return &LogBuffer{limit: limit}
}

func (b *LogBuffer) Append(lines ...string) {
	if b == nil || len(lines) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, line := range lines {
		b.lines = append(b.lines, line)
	}
	if overflow := len(b.lines) - b.limit; overflow > 0 {
		b.lines = append([]string(nil), b.lines[overflow:]...)
	}
}

func (b *LogBuffer) Lines() []string {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.lines...)
}
