package devprofile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	preferenceFilename = "preference.md"
	defaultBaseDir     = ".yollo"
)

// Profile holds the parsed state of a preference.md file.
type Profile struct {
	Path      string
	Target    string
	LoadedAt  time.Time
	Sections  map[string]string // section heading → content
	RawText   string
	mu        sync.RWMutex
}

// ProfileManager handles loading, saving, and querying the preference.md file.
type ProfileManager struct {
	profile  *Profile
	baseDir  string
	target   string
	logger   func(string, ...interface{})
	mu       sync.RWMutex
}

// NewProfileManager creates a manager for the given target session.
func NewProfileManager(target string, logger func(string, ...interface{})) *ProfileManager {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	baseDir := filepath.Join(home, defaultBaseDir, "profiles")
	return &ProfileManager{
		baseDir: baseDir,
		target:  sanitizeTarget(target),
		logger:  logger,
	}
}

// Load reads or initialises the preference.md for the target session.
func (m *ProfileManager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := os.MkdirAll(m.baseDir, 0o755); err != nil {
		return fmt.Errorf("create profile dir: %w", err)
	}

	path := filepath.Join(m.baseDir, m.target+".md")

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read profile: %w", err)
		}
		// Bootstrap a new preference.md
		content := bootstrapPreferenceMD(m.target)
		if writeErr := os.WriteFile(path, []byte(content), 0o644); writeErr != nil {
			return fmt.Errorf("write initial profile: %w", writeErr)
		}
		data = []byte(content)
	}

	m.profile = &Profile{
		Path:     path,
		Target:   m.target,
		LoadedAt: time.Now(),
		RawText:  string(data),
		Sections: parseSections(string(data)),
	}
	return nil
}

// Save writes the current profile back to disk.
func (m *ProfileManager) Save() error {
	m.mu.RLock()
	p := m.profile
	m.mu.RUnlock()
	if p == nil {
		return fmt.Errorf("profile not loaded")
	}
	p.mu.RLock()
	raw := p.RawText
	p.mu.RUnlock()
	return os.WriteFile(p.Path, []byte(raw), 0o644)
}

// ReplaceContent atomically replaces the entire preference.md content (used by compactor).
func (m *ProfileManager) ReplaceContent(content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.profile == nil {
		return fmt.Errorf("profile not loaded")
	}
	m.profile.mu.Lock()
	m.profile.RawText = content
	m.profile.Sections = parseSections(content)
	m.profile.mu.Unlock()
	return os.WriteFile(m.profile.Path, []byte(content), 0o644)
}

// AppendObservation appends a raw observation entry to the observation log section.
func (m *ProfileManager) AppendObservation(entry string) error {
	m.mu.RLock()
	p := m.profile
	m.mu.RUnlock()
	if p == nil {
		return fmt.Errorf("profile not loaded")
	}

	ts := time.Now().Format("2006-01-02 15:04")
	block := fmt.Sprintf("\n### %s\n%s\n", ts, strings.TrimSpace(entry))

	p.mu.Lock()
	defer p.mu.Unlock()

	marker := "## 관찰 로그"
	idx := strings.Index(p.RawText, marker)
	if idx < 0 {
		// Append section if missing
		p.RawText += "\n" + marker + " (compaction 전 raw 데이터)\n" + block
	} else {
		// Insert after the marker line
		lineEnd := strings.Index(p.RawText[idx:], "\n")
		if lineEnd < 0 {
			p.RawText += block
		} else {
			insertAt := idx + lineEnd + 1
			// Skip the optional subheading line
			rest := p.RawText[insertAt:]
			if strings.HasPrefix(strings.TrimSpace(rest), "(compaction") {
				if nl := strings.Index(rest, "\n"); nl >= 0 {
					insertAt += nl + 1
				}
			}
			p.RawText = p.RawText[:insertAt] + block + p.RawText[insertAt:]
		}
	}
	p.Sections = parseSections(p.RawText)

	return os.WriteFile(p.Path, []byte(p.RawText), 0o644)
}

// SummarizeForContext returns a concise preference summary relevant to the given screen context.
// It extracts confirmed preferences (excluding raw observation log) and trims to fit in an LLM prompt.
func (m *ProfileManager) SummarizeForContext(outputBlock string, promptText string) string {
	m.mu.RLock()
	p := m.profile
	m.mu.RUnlock()
	if p == nil {
		return ""
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	var lines []string
	// Include confirmed preference sections, skip raw observations and metadata header
	for _, heading := range sectionOrder {
		content, ok := p.Sections[heading]
		if !ok || strings.TrimSpace(content) == "" {
			continue
		}
		// Only include lines with confidence markers or bullet points
		for _, line := range strings.Split(content, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
				lines = append(lines, trimmed)
			}
		}
	}

	if len(lines) == 0 {
		return ""
	}

	// Cap at 10 most relevant lines to keep prompt concise
	if len(lines) > 10 {
		lines = lines[:10]
	}

	return strings.Join(lines, "\n")
}

// RawContent returns the full preference.md text.
func (m *ProfileManager) RawContent() string {
	m.mu.RLock()
	p := m.profile
	m.mu.RUnlock()
	if p == nil {
		return ""
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.RawText
}

// UncoveredAreas returns the checklist items from the uncovered section.
func (m *ProfileManager) UncoveredAreas() []string {
	m.mu.RLock()
	p := m.profile
	m.mu.RUnlock()
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()

	content, ok := p.Sections["미확인 영역"]
	if !ok {
		return nil
	}
	var items []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- [ ]") {
			items = append(items, strings.TrimSpace(strings.TrimPrefix(trimmed, "- [ ]")))
		}
	}
	return items
}

// Path returns the file path of the preference.md.
func (m *ProfileManager) Path() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.profile == nil {
		return ""
	}
	return m.profile.Path
}

// --- helpers ---

var sectionOrder = []string{
	"코딩 스타일",
	"아키텍처 선호",
	"의사결정 패턴",
	"워크플로우",
}

func parseSections(text string) map[string]string {
	sections := make(map[string]string)
	lines := strings.Split(text, "\n")
	var currentHeading string
	var buf strings.Builder

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			if currentHeading != "" {
				sections[currentHeading] = buf.String()
			}
			currentHeading = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			buf.Reset()
		} else if currentHeading != "" {
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}
	if currentHeading != "" {
		sections[currentHeading] = buf.String()
	}
	return sections
}

func sanitizeTarget(target string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", " ", "_", ":", "_")
	v := strings.TrimSpace(replacer.Replace(target))
	if v == "" {
		return "default"
	}
	return v
}

func bootstrapPreferenceMD(target string) string {
	return fmt.Sprintf(`# Developer Preference Profile
> Target: %s
> Created: %s
> Last compacted: (never)
> Confidence: 0%%

## 코딩 스타일

## 아키텍처 선호

## 의사결정 패턴

## 워크플로우

## 미확인 영역
- [ ] 에러 처리 스타일 (defensive / let-it-crash / result-type)
- [ ] 테스트 작성 시점 (TDD / after / integration-first)
- [ ] 네이밍 스타일 (verbose / concise / domain-driven)
- [ ] 리스크 허용도 (conservative / moderate / aggressive)
- [ ] 리팩토링 시점 (continuous / after-feature / dedicated)
- [ ] 기능 범위 (mvp-first / complete / iterative)
- [ ] 아키텍처 스타일 (clean-arch / pragmatic / ddd)
- [ ] 의존성 정책 (minimal / standard-lib-prefer / best-tool)
- [ ] 커밋 단위 (atomic / feature / wip)
- [ ] PR 스타일 (small-focused / bundled / trunk-based)
- [ ] 성능 vs 가독성 트레이드오프 기준
- [ ] 동시성 패턴 선호
- [ ] 로깅 레벨 기준

## 관찰 로그 (compaction 전 raw 데이터)
`, target, time.Now().Format("2006-01-02"))
}
