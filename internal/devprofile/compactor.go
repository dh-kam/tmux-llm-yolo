package devprofile

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dh-kam/yollo/internal/llm"
)

// ProviderGetter returns an LLM provider on demand.
type ProviderGetter func(context.Context) (llm.Provider, error)

// Compactor periodically refines the preference.md through LLM-based analysis
// of raw observation logs, increasing confidence and promoting patterns.
type Compactor struct {
	profile     *ProfileManager
	recorder    *ConversationRecorder
	getProvider ProviderGetter
	interval    time.Duration
	logger      func(string, ...interface{})

	mu              sync.Mutex
	lastCompaction  time.Time
	observationSeen int
}

// NewCompactor creates a compactor that refines preference.md every interval
// or after observationThreshold new observations, whichever comes first.
func NewCompactor(
	profile *ProfileManager,
	recorder *ConversationRecorder,
	getProvider ProviderGetter,
	interval time.Duration,
	logger func(string, ...interface{}),
) *Compactor {
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	return &Compactor{
		profile:     profile,
		recorder:    recorder,
		getProvider: getProvider,
		interval:    interval,
		logger:      logger,
	}
}

// Run starts the periodic compaction loop. Blocks until ctx is cancelled.
func (c *Compactor) Run(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := c.Compact(ctx); err != nil {
				c.log("preference compaction failed: %v", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

// Compact performs a single compaction cycle.
func (c *Compactor) Compact(ctx context.Context) error {
	c.mu.Lock()
	lastTime := c.lastCompaction
	c.mu.Unlock()

	// Gather new observations since last compaction
	turns := c.recorder.GetSince(lastTime)
	if len(turns) == 0 {
		return nil
	}

	currentMD := c.profile.RawContent()
	if currentMD == "" {
		return fmt.Errorf("empty profile")
	}

	provider, err := c.getProvider(ctx)
	if err != nil || provider == nil {
		return fmt.Errorf("no LLM provider: %w", err)
	}

	observations := formatTurnsForCompaction(turns)

	prompt := buildCompactionPrompt(currentMD, observations)

	c.log("compaction: sending %d observations to LLM", len(turns))
	result, err := provider.RunPrompt(ctx, prompt)
	if err != nil {
		return fmt.Errorf("LLM compaction: %w", err)
	}

	result = strings.TrimSpace(result)
	if !strings.HasPrefix(result, "# Developer Preference Profile") {
		// LLM might have wrapped in code fence
		result = extractMarkdownBlock(result)
		if result == "" {
			return fmt.Errorf("LLM returned unexpected format")
		}
	}

	if err := c.profile.ReplaceContent(result); err != nil {
		return fmt.Errorf("save compacted profile: %w", err)
	}

	c.mu.Lock()
	c.lastCompaction = time.Now()
	c.observationSeen = 0
	c.mu.Unlock()

	c.log("compaction: preference.md updated (%d bytes)", len(result))
	return nil
}

// NotifyObservation tracks observation count for threshold-based compaction.
func (c *Compactor) NotifyObservation() {
	c.mu.Lock()
	c.observationSeen++
	c.mu.Unlock()
}

// NeedsCompaction returns true if enough observations have been collected.
func (c *Compactor) NeedsCompaction(threshold int) bool {
	if threshold <= 0 {
		threshold = 10
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.observationSeen >= threshold
}

func (c *Compactor) log(format string, args ...interface{}) {
	if c.logger != nil {
		c.logger(format, args...)
	}
}

func buildCompactionPrompt(currentMD string, observations string) string {
	return fmt.Sprintf(`You are a developer profiling specialist.
Your task is to refine a developer preference profile based on new observations.

Current developer preference profile:
---
%s
---

New observations since last compaction:
---
%s
---

Instructions:
1. Analyze the new observations for developer preference signals.
2. Update confidence scores: +0.1 per confirming observation (max 0.95), -0.15 per contradicting (min 0.1).
3. Promote confirmed patterns from "관찰 로그" into the appropriate structured sections (코딩 스타일, 아키텍처 선호, 의사결정 패턴, 워크플로우).
4. Add newly discovered preferences with initial confidence [0.3-0.5].
5. Update "미확인 영역": remove items that now have data, add newly discovered gaps.
6. Remove observation log entries that have been fully incorporated.
7. If a stated preference contradicts observed behaviour 3+ times, flag the contradiction and trust observed behaviour.
8. Update the "Last compacted" timestamp to now and recalculate the overall "Confidence" percentage.
9. Keep the document concise — merge similar observations, avoid redundancy.
10. Preserve the exact markdown structure (headings, bullet format, metadata header).
11. Write preference descriptions and observations in Korean.

Return ONLY the complete updated preference.md content. No code fences, no commentary.`, currentMD, observations)
}

func formatTurnsForCompaction(turns []ConversationTurn) string {
	var sb strings.Builder
	for _, t := range turns {
		sb.WriteString(fmt.Sprintf("[%s]\n", t.Timestamp.Format("2006-01-02 15:04")))
		if t.UserInput != "" {
			sb.WriteString(fmt.Sprintf("  user: %s\n", t.UserInput))
		}
		if t.Response != "" {
			sb.WriteString(fmt.Sprintf("  assistant: %s\n", t.Response))
		}
		if t.UserAction != "" {
			sb.WriteString(fmt.Sprintf("  action: %s\n", t.UserAction))
		}
		if t.Context != "" {
			sb.WriteString(fmt.Sprintf("  context: %s\n", t.Context))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func extractMarkdownBlock(s string) string {
	// Try to extract content from ```markdown ... ``` or ``` ... ```
	for _, fence := range []string{"```markdown\n", "```md\n", "```\n"} {
		start := strings.Index(s, fence)
		if start < 0 {
			continue
		}
		content := s[start+len(fence):]
		end := strings.Index(content, "\n```")
		if end < 0 {
			end = strings.LastIndex(content, "```")
		}
		if end > 0 {
			return strings.TrimSpace(content[:end])
		}
	}
	return ""
}
