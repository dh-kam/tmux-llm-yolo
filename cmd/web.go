package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dh-kam/tmux-llm-yolo/internal/webui"
	"github.com/spf13/cobra"
)

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "run web UI for tmux terminal sessions with OAuth login",
	RunE:  runWeb,
}

func init() {
	webCmd.Flags().String("listen", ":8080", "HTTP listen address (ex: :8080)")
	webCmd.Flags().Duration("poll-interval", 700*time.Millisecond, "tmux output polling interval")
	webCmd.Flags().Int("max-terminals", 3, "maximum number of terminal windows in the page")

	rootCmd.AddCommand(webCmd)
}

func runWeb(cmd *cobra.Command, args []string) error {
	listen, err := cmd.Flags().GetString("listen")
	if err != nil {
		return err
	}

	pollInterval, err := cmd.Flags().GetDuration("poll-interval")
	if err != nil {
		return err
	}

	maxTerminals, err := cmd.Flags().GetInt("max-terminals")
	if err != nil {
		return err
	}

	sessionTTL := parseDurationFromEnv("WEB_SESSION_TTL_SECONDS", 24*time.Hour)
	if sessionTTL <= 0 {
		sessionTTL = 24 * time.Hour
	}

	cookieSecure := false
	if cookieSecureRaw := strings.TrimSpace(os.Getenv("WEB_COOKIE_SECURE")); cookieSecureRaw != "" {
		if parsed, parseErr := strconv.ParseBool(cookieSecureRaw); parseErr == nil {
			cookieSecure = parsed
		}
	}

	cfg := webui.Config{
		ListenAddr:         listen,
		PollInterval:       pollInterval,
		MaxTerminals:       maxTerminals,
		OAuthBaseURL:       strings.TrimSpace(os.Getenv("WEB_OAUTH_BASE_URL")),
		GoogleClientID:     strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID")),
		GoogleClientSecret: strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET")),
		GitHubClientID:     strings.TrimSpace(os.Getenv("GITHUB_CLIENT_ID")),
		GitHubClientSecret: strings.TrimSpace(os.Getenv("GITHUB_CLIENT_SECRET")),
		AllowedEmails:      splitCSVEnv(os.Getenv("WEB_ALLOWED_EMAILS")),
		AdminEmails:        splitCSVEnv(os.Getenv("WEB_ADMIN_EMAILS")),
		AllowedSessions:    splitCSVEnv(os.Getenv("WEB_ALLOWED_SESSIONS")),
		TmuxCommand:        strings.TrimSpace(os.Getenv("TMUX_COMMAND")),
		CookieSecure:       cookieSecure,
		SessionTTL:         sessionTTL,
	}

	server, err := webui.New(cfg)
	if err != nil {
		return err
	}

	fmt.Printf("tmux web UI listening on %s\n", cfg.ListenAddr)
	return server.Run()
}

func parseDurationFromEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	seconds, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}

	if seconds <= 0 {
		return fallback
	}

	return time.Duration(seconds) * time.Second
}

func splitCSVEnv(raw string) []string {
	parts := strings.Split(raw, ",")
	var values []string
	for _, value := range parts {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" {
			continue
		}
		values = append(values, value)
	}
	return values
}
