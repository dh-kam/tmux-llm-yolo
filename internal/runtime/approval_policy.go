package runtime

import (
	"strings"
	"sync"
)

// ApprovalPolicy tracks how many times approval prompts have been answered
// and decides whether to use one-time approval (option 1) or session-wide
// approval (option 2) based on a configurable threshold.
//
// Policy: first N approvals use "Allow once" (option 1), then switch to
// "Allow for this session" (option 2) permanently.
type ApprovalPolicy struct {
	mu        sync.Mutex
	counts    map[string]int // command prefix → approval count
	threshold int            // after this many one-time approvals, switch to session-wide
}

// NewApprovalPolicy creates a policy that switches to session-wide approval
// after the given threshold of one-time approvals per command prefix.
func NewApprovalPolicy(threshold int) *ApprovalPolicy {
	if threshold <= 0 {
		threshold = 5
	}
	return &ApprovalPolicy{
		counts:    make(map[string]int),
		threshold: threshold,
	}
}

// ApprovalChoice represents the decision for an approval prompt.
type ApprovalChoice struct {
	Option int    // 1 = allow once, 2 = allow for session
	Reason string
}

// Decide returns which approval option to select for the given context.
// The context is typically the visible output block or command description
// from the approval prompt.
func (p *ApprovalPolicy) Decide(context string) ApprovalChoice {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := extractApprovalKey(context)
	p.counts[key]++
	count := p.counts[key]

	if count <= p.threshold {
		return ApprovalChoice{
			Option: 1,
			Reason: reasonOneTime(count, p.threshold),
		}
	}
	return ApprovalChoice{
		Option: 2,
		Reason: reasonSession(count, p.threshold),
	}
}

// Count returns how many times approvals have been given for the key.
func (p *ApprovalPolicy) Count(context string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.counts[extractApprovalKey(context)]
}

// TotalCount returns the total number of approvals across all keys.
func (p *ApprovalPolicy) TotalCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	total := 0
	for _, c := range p.counts {
		total += c
	}
	return total
}

// extractApprovalKey normalises the approval context into a tracking key.
// It extracts the command name/prefix from typical approval text like:
//   "go test ./internal/cache -count=1"  →  "go test"
//   "gofmt -w internal/cache/cache.go"   →  "gofmt"
//   "Shell go test -v ."                 →  "go test"
func extractApprovalKey(context string) string {
	context = strings.TrimSpace(context)
	if context == "" {
		return "_empty"
	}

	// Strip common prefixes from different frontends
	for _, prefix := range []string{"$ ", "Shell ", "Reason: "} {
		if idx := strings.Index(context, prefix); idx >= 0 {
			context = strings.TrimSpace(context[idx+len(prefix):])
		}
	}

	// Extract first two words as the command key (e.g., "go test", "gofmt")
	fields := strings.Fields(context)
	if len(fields) == 0 {
		return "_empty"
	}
	if len(fields) == 1 {
		return strings.ToLower(fields[0])
	}

	// For commands like "go test", "go build", "npm run" — use first two words
	first := strings.ToLower(fields[0])
	if first == "go" || first == "npm" || first == "cargo" || first == "make" || first == "python" {
		return first + " " + strings.ToLower(fields[1])
	}
	return first
}

func reasonOneTime(count int, threshold int) string {
	return strings.TrimSpace(
		strings.Join([]string{
			"approval policy: allow once",
			"(" + itoa(count) + "/" + itoa(threshold) + " before session-wide)",
		}, " "))
}

func reasonSession(count int, threshold int) string {
	return strings.TrimSpace(
		strings.Join([]string{
			"approval policy: allow for session",
			"(threshold " + itoa(threshold) + " reached, count=" + itoa(count) + ")",
		}, " "))
}

func itoa(n int) string {
	if n < 0 {
		return "-" + itoa(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
