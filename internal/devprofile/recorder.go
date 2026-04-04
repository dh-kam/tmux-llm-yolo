package devprofile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ConversationTurn represents one user–assistant exchange.
type ConversationTurn struct {
	Timestamp  time.Time `json:"ts"`
	UserInput  string    `json:"user_input,omitempty"`
	Response   string    `json:"response,omitempty"`
	Context    string    `json:"context,omitempty"`
	UserAction string    `json:"user_action,omitempty"` // accept, reject, modify, choice:N
}

// ConversationRecorder appends conversation turns to a JSONL file.
type ConversationRecorder struct {
	path           string
	profile        *ProfileManager
	mu             sync.Mutex
	pendingTurn    *ConversationTurn
	lastCompaction time.Time
}

// NewConversationRecorder creates a recorder for the given target session.
func NewConversationRecorder(target string, profile *ProfileManager) *ConversationRecorder {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	dir := filepath.Join(home, defaultBaseDir, "conversations")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, sanitizeTarget(target)+".jsonl")
	return &ConversationRecorder{
		path:    path,
		profile: profile,
	}
}

// RecordUserInput starts a new conversation turn with the user's input.
func (r *ConversationRecorder) RecordUserInput(input string, context string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Flush any pending turn that never got a response
	if r.pendingTurn != nil {
		r.flushLocked()
	}

	r.pendingTurn = &ConversationTurn{
		Timestamp: time.Now(),
		UserInput: strings.TrimSpace(input),
		Context:   strings.TrimSpace(context),
	}
}

// RecordAssistantResponse completes the pending turn with the assistant's response.
func (r *ConversationRecorder) RecordAssistantResponse(response string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.pendingTurn == nil {
		// No matching user input; record response-only turn
		r.pendingTurn = &ConversationTurn{
			Timestamp: time.Now(),
		}
	}
	r.pendingTurn.Response = trimResponse(response)
	r.flushLocked()
}

// RecordUserAction records how the user reacted to the last assistant output.
// Actions: "accept", "reject", "modify", "choice:N", "skip".
func (r *ConversationRecorder) RecordUserAction(action string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	turn := &ConversationTurn{
		Timestamp:  time.Now(),
		UserAction: strings.TrimSpace(action),
	}
	r.appendLocked(turn)
	r.maybeObserveLocked(turn)
}

// GetSince returns all turns recorded after the given time.
func (r *ConversationRecorder) GetSince(since time.Time) []ConversationTurn {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := os.ReadFile(r.path)
	if err != nil {
		return nil
	}

	var result []ConversationTurn
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var turn ConversationTurn
		if err := json.Unmarshal([]byte(line), &turn); err != nil {
			continue
		}
		if turn.Timestamp.After(since) {
			result = append(result, turn)
		}
	}
	return result
}

// Path returns the JSONL file path.
func (r *ConversationRecorder) Path() string {
	return r.path
}

// --- internal ---

func (r *ConversationRecorder) flushLocked() {
	if r.pendingTurn == nil {
		return
	}
	r.appendLocked(r.pendingTurn)
	r.maybeObserveLocked(r.pendingTurn)
	r.pendingTurn = nil
}

func (r *ConversationRecorder) appendLocked(turn *ConversationTurn) {
	data, err := json.Marshal(turn)
	if err != nil {
		return
	}
	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}

// maybeObserveLocked derives a preference observation from a turn and appends it to the profile.
func (r *ConversationRecorder) maybeObserveLocked(turn *ConversationTurn) {
	if r.profile == nil {
		return
	}

	var observation string
	switch {
	case turn.UserInput != "" && turn.Response != "":
		observation = fmt.Sprintf("- 사용자 입력: %q\n- assistant 응답 요약: %s",
			truncate(turn.UserInput, 200),
			truncate(turn.Response, 300))
		if turn.Context != "" {
			observation += fmt.Sprintf("\n- 맥락: %s", truncate(turn.Context, 100))
		}
	case turn.UserAction != "":
		observation = fmt.Sprintf("- 사용자 행동: %s", turn.UserAction)
		if turn.Context != "" {
			observation += fmt.Sprintf("\n- 맥락: %s", truncate(turn.Context, 100))
		}
	default:
		return
	}

	_ = r.profile.AppendObservation(observation)
}

func trimResponse(s string) string {
	s = strings.TrimSpace(s)
	// Keep at most 500 chars for JSONL compactness
	if len(s) > 500 {
		return s[:500] + "..."
	}
	return s
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
