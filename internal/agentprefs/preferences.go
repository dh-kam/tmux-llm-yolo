package agentprefs

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dh-kam/tmux-llm-yolo/internal/llm"
)

const agentsFilename = "AGENTS.md"

//go:embed AGENTS.md
var embeddedDefaultAgentsMD []byte

type ProviderGetter func(context.Context) (llm.Provider, error)

func EmbeddedPreferencesMarkdown() []byte {
	return append([]byte(nil), embeddedDefaultAgentsMD...)
}

func EnsureAgentsDocument(ctx context.Context, cwd string, getProvider ProviderGetter) (string, string, error) {
	baseDir := strings.TrimSpace(cwd)
	if baseDir == "" {
		var err error
		baseDir, err = os.Getwd()
		if err != nil {
			return "", "", err
		}
	}
	path := filepath.Join(baseDir, agentsFilename)
	if existing, err := os.ReadFile(path); err == nil {
		text := strings.TrimSpace(string(existing))
		if text != "" {
			return path, text, nil
		}
	}

	content := ""
	if getProvider != nil {
		if provider, err := getProvider(ctx); err == nil && provider != nil {
			if generated, genErr := generateAgentsWithLLM(ctx, provider); genErr == nil {
				content = strings.TrimSpace(generated)
			}
		}
	}
	if content == "" {
		content = fallbackAgentsFromPreferences()
	}

	if err := os.WriteFile(path, []byte(content+"\n"), 0o644); err != nil {
		return "", "", err
	}
	return path, content, nil
}

func generateAgentsWithLLM(ctx context.Context, provider llm.Provider) (string, error) {
	prompt := fmt.Sprintf(`You are writing AGENTS.md for a coding agent runtime.
Create concise, strict engineering rules in Markdown from the preference document.
Output Markdown only. Start with "# AGENTS.md".
Use clear sections and imperative rules. Preserve all constraints from the document.

Preferences Markdown:
%s
`, string(embeddedDefaultAgentsMD))
	out, err := provider.RunPrompt(ctx, prompt)
	if err != nil {
		return "", err
	}
	out = normalizeGeneratedAgents(strings.TrimSpace(out))
	if out == "" {
		return "", fmt.Errorf("empty AGENTS output")
	}
	if !strings.HasPrefix(out, "# AGENTS.md") {
		return "", fmt.Errorf("generated output does not start with AGENTS header")
	}
	return out, nil
}

func normalizeGeneratedAgents(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = trimNoiseTail(raw)
	if raw == "" {
		return ""
	}
	if !looksLikeTranscriptNoise(raw) {
		return raw
	}
	candidates := collectAgentsCandidates(raw)
	for _, candidate := range candidates {
		candidate = trimNoiseTail(candidate)
		if candidate == "" {
			continue
		}
		if !looksLikeTranscriptNoise(candidate) {
			return candidate
		}
	}
	return ""
}

func trimNoiseTail(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	stop := len(lines)
	for i, line := range lines {
		t := strings.TrimSpace(line)
		lower := strings.ToLower(t)
		switch {
		case strings.HasPrefix(lower, "warning:"):
			stop = i
		case strings.HasPrefix(lower, "openai codex v"):
			stop = i
		case lower == "codex":
			stop = i
		case strings.HasPrefix(lower, "workdir:"),
			strings.HasPrefix(lower, "model:"),
			strings.HasPrefix(lower, "provider:"),
			strings.HasPrefix(lower, "approval:"),
			strings.HasPrefix(lower, "sandbox:"),
			strings.HasPrefix(lower, "session id:"):
			stop = i
		}
		if stop != len(lines) {
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines[:stop], "\n"))
}

func collectAgentsCandidates(raw string) []string {
	const marker = "# AGENTS.md"
	var out []string
	remaining := raw
	for {
		idx := strings.Index(remaining, marker)
		if idx < 0 {
			break
		}
		candidate := strings.TrimSpace(remaining[idx:])
		if candidate != "" {
			out = append(out, candidate)
		}
		nextStart := idx + len(marker)
		if nextStart >= len(remaining) {
			break
		}
		remaining = remaining[nextStart:]
	}
	return out
}

func looksLikeTranscriptNoise(value string) bool {
	lower := strings.ToLower(value)
	noiseMarkers := []string{
		"openai codex v",
		"workdir:",
		"provider:",
		"approval:",
		"sandbox:",
		"reasoning effort:",
		"session id:",
		"\nuser\n",
		"\nassistant\n",
		"preferences markdown:",
	}
	for _, marker := range noiseMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func fallbackAgentsFromPreferences() string {
	src := strings.TrimSpace(string(embeddedDefaultAgentsMD))
	if strings.HasPrefix(src, "# AGENTS.md") {
		return src
	}
	return "# AGENTS.md\n\n## Source Preferences\n\n" + src
}
