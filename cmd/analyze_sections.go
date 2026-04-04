package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dh-kam/yollo/internal/capture"
	"github.com/dh-kam/yollo/internal/tuianalyzer"
	"github.com/spf13/cobra"
)

var analyzeSectionsCmd = &cobra.Command{
	Use:   "analyze-sections [CAPTURE_FILE]",
	Short: "analyze terminal capture into semantic sections",
	Long: `Analyze a terminal capture file (plain or ANSI) into semantic sections:
header, footer, user prompt, spinner, assistant output, separator, etc.

Supports Codex, GLM/Claude, Gemini, and Copilot providers.`,
	RunE: runAnalyzeSections,
}

func init() {
	analyzeSectionsCmd.Flags().String("format", "plain", "output format: plain, json, or summary")
	analyzeSectionsCmd.Flags().String("provider", "", "provider hint (legacy alias for --frontend)")
	analyzeSectionsCmd.Flags().String("frontend", "", "frontend hint (claude-code, codex, gemini, copilot)")
	analyzeSectionsCmd.Flags().String("manifest", "", "manifest.json for live capture corpus validation")
	rootCmd.AddCommand(analyzeSectionsCmd)
}

func runAnalyzeSections(cmd *cobra.Command, args []string) error {
	format, err := cmd.Flags().GetString("format")
	if err != nil {
		return err
	}
	providerHint, err := cmd.Flags().GetString("provider")
	if err != nil {
		return err
	}
	frontendHint, err := cmd.Flags().GetString("frontend")
	if err != nil {
		return err
	}
	// --provider is a legacy alias for --frontend
	hint := frontendHint
	if hint == "" {
		hint = providerHint
	}
	manifestPath, err := cmd.Flags().GetString("manifest")
	if err != nil {
		return err
	}

	if manifestPath != "" {
		return runManifestAnalysis(manifestPath, format)
	}

	if len(args) < 1 {
		return fmt.Errorf("capture file argument required (or use --manifest)")
	}

	capturePath := args[0]
	plainCapture, ansiCapture, err := loadCaptureFile(capturePath)
	if err != nil {
		return err
	}

	var result tuianalyzer.AnalysisResult
	if hint != "" {
		result = tuianalyzer.AnalyzeWithHint(hint, ansiCapture, plainCapture)
	} else {
		result = tuianalyzer.Analyze(ansiCapture, plainCapture)
	}

	switch format {
	case "json":
		return printAnalysisJSON(result)
	case "summary":
		return printAnalysisSummary(result)
	case "plain":
		return printAnalysisPlain(result)
	default:
		return fmt.Errorf("unsupported format: %s (use plain, json, or summary)", format)
	}
}

func loadCaptureFile(path string) (plain string, ansi string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("read %s: %w", path, err)
	}
	plain = string(data)

	// Try to load ANSI companion file
	ansiPath := strings.TrimSuffix(path, ".capture") + ".ansi.capture"
	if ansiData, readErr := os.ReadFile(ansiPath); readErr == nil {
		ansi = string(ansiData)
	}

	// If the file is likely ANSI (contains escape sequences), use it as ANSI
	if strings.Contains(plain, "\x1b[") && ansi == "" {
		ansi = plain
		plain = capture.StripANSI(plain)
	}

	return plain, ansi, nil
}

func printAnalysisPlain(result tuianalyzer.AnalysisResult) error {
	fmt.Printf("FRONTEND: %s\n", result.FrontEnd)
	fmt.Printf("LINES: %d\n", result.TotalLines())
	fmt.Println()

	for _, s := range result.Sections {
		start := s.StartLine + 1 // 1-based for display
		end := s.EndLine + 1

		var rangeStr string
		if start == end {
			rangeStr = fmt.Sprintf("%5d", start)
		} else {
			rangeStr = fmt.Sprintf("%3d-%-3d", start, end)
		}

		firstLine := truncate(s.FirstNonEmptyPlain(), 60)
		fmt.Printf(" %s  [%-13s] (%.2f)  %s\n",
			rangeStr,
			s.Type,
			s.Confidence,
			firstLine,
		)
	}

	return nil
}

func printAnalysisSummary(result tuianalyzer.AnalysisResult) error {
	fmt.Printf("Frontend: %s\n", result.FrontEnd)
	fmt.Printf("Total Lines: %d\n", result.TotalLines())
	fmt.Printf("Sections: %d\n", len(result.Sections))
	fmt.Println()

	// Count by type
	counts := make(map[tuianalyzer.SectionType]int)
	for _, s := range result.Sections {
		counts[s.Type]++
	}

	fmt.Println("Section breakdown:")
	for _, st := range []tuianalyzer.SectionType{
		tuianalyzer.SectionHeader,
		tuianalyzer.SectionAssistantOutput,
		tuianalyzer.SectionSeparator,
		tuianalyzer.SectionSpinner,
		tuianalyzer.SectionAssistantQuestion,
		tuianalyzer.SectionUserPrompt,
		tuianalyzer.SectionFooter,
		tuianalyzer.SectionUnknown,
	} {
		if c, ok := counts[st]; ok {
			fmt.Printf("  %-13s: %d\n", st, c)
		}
	}

	// Show last user prompt
	if prompt := result.LastSectionByType(tuianalyzer.SectionUserPrompt); prompt != nil {
		fmt.Printf("\nLast user prompt: %s\n", truncate(prompt.PlainText(), 120))
	}

	// Show last footer
	if footer := result.LastSectionByType(tuianalyzer.SectionFooter); footer != nil {
		fmt.Printf("Last footer: %s\n", truncate(footer.PlainText(), 120))
	}

	return nil
}

func printAnalysisJSON(result tuianalyzer.AnalysisResult) error {
	type jsonSection struct {
		Type       string   `json:"type"`
		StartLine  int      `json:"start_line"`
		EndLine    int      `json:"end_line"`
		LineCount  int      `json:"line_count"`
		Confidence float64  `json:"confidence"`
		Preview    string   `json:"preview"`
		PlainLines []string `json:"plain_lines,omitempty"`
	}

	type jsonOutput struct {
		FrontEnd string        `json:"frontend"`
		Lines    int           `json:"total_lines"`
		Sections []jsonSection `json:"sections"`
	}

	sections := make([]jsonSection, len(result.Sections))
	for i, s := range result.Sections {
		sections[i] = jsonSection{
			Type:       s.Type.String(),
			StartLine:  s.StartLine,
			EndLine:    s.EndLine,
			LineCount:  s.LineCount(),
			Confidence: s.Confidence,
			Preview:    truncate(s.FirstNonEmptyPlain(), 100),
		}
	}

	out := jsonOutput{
		FrontEnd: result.FrontEnd,
		Lines:    result.TotalLines(),
		Sections: sections,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

type liveCaptureManifest struct {
	Provider string `json:"provider"`
	Files    []struct {
		ANSI  string `json:"ansi"`
		Plain string `json:"plain"`
	} `json:"files"`
}

func runManifestAnalysis(manifestPath string, format string) error {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	var manifest liveCaptureManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}

	baseDir := filepath.Dir(manifestPath)
	history := tuianalyzer.NewCaptureHistory()
	totalSections := 0
	totalFiles := 0

	for _, f := range manifest.Files {
		var ansi, plain string

		if f.Plain != "" {
			p := resolvePath(baseDir, f.Plain)
			if d, err := os.ReadFile(p); err == nil {
				plain = string(d)
			}
		}
		if f.ANSI != "" {
			p := resolvePath(baseDir, f.ANSI)
			if d, err := os.ReadFile(p); err == nil {
				ansi = string(d)
			}
		}

		if plain == "" {
			continue
		}

		result := history.AnalyzeWithLearning(ansi, plain)
		history.Record(result)
		totalSections += len(result.Sections)
		totalFiles++

		if format == "plain" {
			fmt.Printf("--- %s (%d sections, provider=%s) ---\n",
				filepath.Base(f.Plain), len(result.Sections), result.FrontEnd)
		}
	}

	summary := history.Summary()

	if format == "json" {
		type manifestResult struct {
			Frontend          string                   `json:"frontend"`
			TotalFiles        int                      `json:"total_files"`
			TotalSections     int                      `json:"total_sections"`
			StableFooterLines []tuianalyzer.StableLine `json:"stable_footer_lines"`
			StableHeaderLines []tuianalyzer.StableLine `json:"stable_header_lines"`
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(manifestResult{
			Frontend:          summary.FrontEnd,
			TotalFiles:        totalFiles,
			TotalSections:     totalSections,
			StableFooterLines: summary.StableFooterLines,
			StableHeaderLines: summary.StableHeaderLines,
		})
	}

	fmt.Printf("\nManifest: %s\n", manifestPath)
	fmt.Printf("Files analyzed: %d\n", totalFiles)
	fmt.Printf("Total sections: %d\n", totalSections)
	fmt.Printf("Frontend: %s\n", summary.FrontEnd)
	fmt.Printf("Stable footer lines: %d\n", len(summary.StableFooterLines))
	fmt.Printf("Stable header lines: %d\n", len(summary.StableHeaderLines))

	if len(summary.StableFooterLines) > 0 {
		fmt.Println("\nStable footer patterns:")
		for _, sl := range summary.StableFooterLines {
			fmt.Printf("  fromBottom=%d count=%d fp=%q\n", sl.FromBottom, sl.Count, sl.Fingerprint)
		}
	}

	return nil
}

func resolvePath(baseDir string, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(baseDir, p)
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
