package tuianalyzer

import (
	"regexp"
	"strings"
	"sync"
)

// StableLine represents a line fingerprint that appeared consistently across captures.
type StableLine struct {
	Fingerprint string      // Normalized content for matching
	FromBottom  int         // Position from the last line (0 = last line)
	Count       int         // Times seen at this position
	TypeHint    SectionType // Most common section type at this position
}

// CaptureSummary holds learned patterns from accumulated captures.
type CaptureSummary struct {
	StableFooterLines   []StableLine
	StableHeaderLines   []StableLine
	StablePromptMarkers []StableLine
	FrontEnd            string
	TotalCaptures       int
}

// CaptureHistory accumulates section analysis results across captures
// to learn stable patterns (footer, header, prompt positions).
type CaptureHistory struct {
	mu      sync.Mutex
	records []analysisRecord
	summary CaptureSummary
}

type analysisRecord struct {
	frontEnd   string
	plainLines []string
	sections   []Section
}

// NewCaptureHistory creates a new, empty capture history.
func NewCaptureHistory() *CaptureHistory {
	return &CaptureHistory{}
}

// Record adds an analysis result to the history and updates learned patterns.
func (h *CaptureHistory) Record(result AnalysisResult) {
	h.mu.Lock()
	defer h.mu.Unlock()

	rec := analysisRecord{
		frontEnd:   result.FrontEnd,
		plainLines: result.PlainLines,
		sections:   result.Sections,
	}
	h.records = append(h.records, rec)
	h.recompute()
}

// Summary returns the current learned patterns.
func (h *CaptureHistory) Summary() CaptureSummary {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.summary
}

// Reset clears all learned history.
func (h *CaptureHistory) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = nil
	h.summary = CaptureSummary{}
}

// AnalyzeWithLearning performs analysis enhanced by learned patterns.
func (h *CaptureHistory) AnalyzeWithLearning(ansiCapture string, plainCapture string) AnalysisResult {
	summary := h.Summary()
	result := Analyze(ansiCapture, plainCapture)

	if summary.TotalCaptures < 2 {
		return result
	}

	// Boost confidence for lines matching stable patterns
	h.boostFromHistory(&result, summary)

	return result
}

const (
	minObservationsForStable = 2
	maxFooterScanLines       = 6
	maxHeaderScanLines       = 8
)

func (h *CaptureHistory) recompute() {
	total := len(h.records)
	if total == 0 {
		h.summary = CaptureSummary{}
		return
	}

	frontEnd := h.records[total-1].frontEnd
	h.summary.FrontEnd = frontEnd
	h.summary.TotalCaptures = total

	// Analyze footer stability: track line fingerprints at positions from bottom
	footerObservations := map[int]map[string]int{} // fromBottom → fingerprint → count
	for _, rec := range h.records {
		n := len(rec.plainLines)
		for offset := 0; offset < maxFooterScanLines && offset < n; offset++ {
			line := strings.TrimSpace(rec.plainLines[n-1-offset])
			if line == "" {
				continue
			}
			fp := normalizeFingerprint(line)
			if fp == "" {
				continue
			}
			if footerObservations[offset] == nil {
				footerObservations[offset] = map[string]int{}
			}
			footerObservations[offset][fp]++
		}
	}

	// Build stable footer lines
	h.summary.StableFooterLines = nil
	for offset, fps := range footerObservations {
		for fp, count := range fps {
			if count >= minObservationsForStable {
				h.summary.StableFooterLines = append(h.summary.StableFooterLines, StableLine{
					Fingerprint: fp,
					FromBottom:  offset,
					Count:       count,
					TypeHint:    SectionFooter,
				})
			}
		}
	}

	// Analyze header stability: track line fingerprints at positions from top
	headerObservations := map[int]map[string]int{}
	for _, rec := range h.records {
		n := len(rec.plainLines)
		for offset := 0; offset < maxHeaderScanLines && offset < n; offset++ {
			line := strings.TrimSpace(rec.plainLines[offset])
			if line == "" {
				continue
			}
			fp := normalizeFingerprint(line)
			if fp == "" {
				continue
			}
			if headerObservations[offset] == nil {
				headerObservations[offset] = map[string]int{}
			}
			headerObservations[offset][fp]++
		}
	}

	// Build stable header lines
	h.summary.StableHeaderLines = nil
	for _, fps := range headerObservations {
		for fp, count := range fps {
			if count >= minObservationsForStable {
				h.summary.StableHeaderLines = append(h.summary.StableHeaderLines, StableLine{
					Fingerprint: fp,
					FromBottom:  -1, // Not applicable for headers
					Count:       count,
					TypeHint:    SectionHeader,
				})
			}
		}
	}

	// Analyze prompt marker stability
	promptObservations := map[string]int{} // fingerprint → count
	for _, rec := range h.records {
		for _, sec := range rec.sections {
			if sec.Type == SectionUserPrompt {
				first := sec.FirstNonEmptyPlain()
				if first != "" {
					fp := normalizeFingerprint(first)
					if fp != "" {
						promptObservations[fp]++
					}
				}
			}
		}
	}

	h.summary.StablePromptMarkers = nil
	for fp, count := range promptObservations {
		if count >= minObservationsForStable {
			h.summary.StablePromptMarkers = append(h.summary.StablePromptMarkers, StableLine{
				Fingerprint: fp,
				Count:       count,
				TypeHint:    SectionUserPrompt,
			})
		}
	}
}

func (h *CaptureHistory) boostFromHistory(result *AnalysisResult, summary CaptureSummary) {
	n := len(result.PlainLines)
	if n == 0 {
		return
	}

	// Check each section for stable pattern matches
	for i := range result.Sections {
		sec := &result.Sections[i]

		// Boost footer sections that match stable footer lines
		if sec.Type == SectionFooter || sec.EndLine >= n*3/4 {
			for _, line := range sec.PlainLines {
				fp := normalizeFingerprint(strings.TrimSpace(line))
				if matchesStableLine(fp, summary.StableFooterLines) {
					sec.Confidence = min(1.0, sec.Confidence+0.15)
					if sec.Type == SectionUnknown {
						sec.Type = SectionFooter
					}
				}
			}
		}

		// Boost header sections that match stable header lines
		if sec.Type == SectionHeader || sec.StartLine < n/4 {
			for _, line := range sec.PlainLines {
				fp := normalizeFingerprint(strings.TrimSpace(line))
				if matchesStableLine(fp, summary.StableHeaderLines) {
					sec.Confidence = min(1.0, sec.Confidence+0.15)
					if sec.Type == SectionUnknown {
						sec.Type = SectionHeader
					}
				}
			}
		}

		// Boost user prompt sections that match stable prompt markers
		if sec.Type == SectionUserPrompt {
			first := sec.FirstNonEmptyPlain()
			if first != "" {
				fp := normalizeFingerprint(first)
				if matchesStableLine(fp, summary.StablePromptMarkers) {
					sec.Confidence = min(1.0, sec.Confidence+0.1)
				}
			}
		}
	}
}

func matchesStableLine(fp string, stable []StableLine) bool {
	for _, s := range stable {
		if s.Fingerprint == fp {
			return true
		}
	}
	return false
}

// normalizeFingerprint creates a stable fingerprint for a line,
// replacing variable content with placeholders.
func normalizeFingerprint(plain string) string {
	plain = strings.TrimSpace(plain)
	plain = strings.ToLower(plain)
	// Replace digits with #
	digitReplace := regexp.MustCompile(`\d+`)
	plain = digitReplace.ReplaceAllString(plain, "#")
	// Normalize whitespace
	spaceReplace := regexp.MustCompile(`\s+`)
	plain = spaceReplace.ReplaceAllString(plain, " ")
	return plain
}
