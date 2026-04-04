package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/dh-kam/yollo/internal/buildinfo"
	"github.com/dh-kam/yollo/internal/capture"
	"github.com/dh-kam/yollo/internal/i18n"
	"github.com/dh-kam/yollo/internal/llm"
	"github.com/dh-kam/yollo/internal/policy"
	watchruntime "github.com/dh-kam/yollo/internal/runtime"
	sessioncheck "github.com/dh-kam/yollo/internal/sessioncheck"
	"github.com/dh-kam/yollo/internal/tmux"
	"github.com/dh-kam/yollo/internal/tui"
	"github.com/dh-kam/yollo/internal/updater"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var seoulLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return time.FixedZone("Asia/Seoul", 9*60*60)
	}
	return loc
}()

type captureDecision = sessioncheck.Decision

type captureDecisionArtifact struct {
	Command          string                `json:"command"`
	Timestamp        string                `json:"timestamp"`
	Iteration        int                   `json:"iteration"`
	Target           string                `json:"target"`
	LLMProvider      string                `json:"llm_provider"`
	LLMModel         string                `json:"llm_model"`
	CaptureLines     int                   `json:"capture_lines"`
	CaptureWithANSI  bool                  `json:"capture_with_ansi"`
	RawCapturePath   string                `json:"raw_capture_path"`
	PlainCapturePath string                `json:"plain_capture_path"`
	PromptPath       string                `json:"prompt_path"`
	DecisionRaw      string                `json:"decision_raw,omitempty"`
	DecisionTextPath string                `json:"decision_text_path"`
	Decision         captureDecisionFields `json:"decision"`
}

type captureDecisionFields struct {
	Action            string `json:"action"`
	Status            string `json:"status"`
	Working           bool   `json:"working"`
	MultiChoice       bool   `json:"multi_choice"`
	Completed         bool   `json:"completed"`
	RecommendedChoice string `json:"recommended_choice"`
	Reason            string `json:"reason"`
}

func safeFilename(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "session"
	}
	value = strings.ReplaceAll(value, "/", "_")
	value = strings.ReplaceAll(value, "\\", "_")
	value = strings.ReplaceAll(value, string(filepath.Separator), "_")
	value = strings.ReplaceAll(value, " ", "_")
	return value
}

func writeCaptureArtifacts(
	locale string,
	logger func(string, ...interface{}),
	stateDir string,
	command string,
	target string,
	timestamp string,
	iteration int,
	llmProvider string,
	llmModel string,
	captureLines int,
	captureWithANSI bool,
	rawCapture string,
	promptPath string,
	decisionPath string,
	decision captureDecision,
) {
	safeTarget := safeFilename(target)
	stem := fmt.Sprintf("%s-%s-%s-%03d", safeTarget, command, timestamp, iteration)
	capturedDir := filepath.Join(stateDir, "captured")
	if err := os.MkdirAll(capturedDir, 0o755); err != nil {
		logger(i18n.T(locale, "cmd.warn_capture_artifacts_dir", capturedDir, err))
		return
	}

	rawCapturePath := filepath.Join(capturedDir, stem+".ansi.txt")
	plainCapturePath := filepath.Join(capturedDir, stem+".plain.txt")
	decisionJSONPath := filepath.Join(capturedDir, stem+".json")

	if err := writeStateTextFile(rawCapturePath, []byte(rawCapture), locale, logger); err != nil {
		logger(i18n.T(locale, "cmd.warn_capture_raw_write", rawCapturePath, err))
	}
	plainCapture := stripANSICodesAll(rawCapture)
	if err := writeStateTextFile(plainCapturePath, []byte(plainCapture), locale, logger); err != nil {
		logger(i18n.T(locale, "cmd.warn_capture_plain_write", plainCapturePath, err))
	}

	record := captureDecisionArtifact{
		Command:          command,
		Timestamp:        timestamp,
		Iteration:        iteration,
		Target:           target,
		LLMProvider:      llmProvider,
		LLMModel:         llmModel,
		CaptureLines:     captureLines,
		CaptureWithANSI:  captureWithANSI,
		RawCapturePath:   rawCapturePath,
		PlainCapturePath: plainCapturePath,
		PromptPath:       promptPath,
		DecisionTextPath: decisionPath,
		DecisionRaw:      readTextFile(decisionPath),
		Decision: captureDecisionFields{
			Action:            decision.Action,
			Status:            decision.Status,
			Working:           decision.Working,
			MultiChoice:       decision.MultiChoice,
			Completed:         decision.Completed,
			RecommendedChoice: decision.RecommendedChoice,
			Reason:            decision.Reason,
		},
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		logger(i18n.T(locale, "cmd.warn_capture_decision_marshal", err))
		return
	}
	if err := os.WriteFile(decisionJSONPath, data, 0644); err != nil {
		logger(i18n.T(locale, "cmd.warn_capture_decision_json_write", decisionJSONPath, err))
	}
}

func readTextFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func writeStateTextFile(path string, data []byte, locale string, logger func(string, ...interface{})) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logger(i18n.T(locale, "cmd.warn_state_file_dir_mkdir", filepath.Dir(path), err))
		return err
	}
	return os.WriteFile(path, data, 0644)
}

var rootCmd = &cobra.Command{
	Use:          buildinfo.AppName,
	Short:        "tmux watcher with llm autopilot",
	SilenceUsage: true,
	Version:      buildinfo.Version,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

var watchCmd = &cobra.Command{
	Use:   "watch [SESSION_NAME]",
	Short: "watch tmux session and auto-continue when needed",
	RunE:  runWatch,
}

var checkCmd = &cobra.Command{
	Use:   "check [SESSION_NAME]",
	Short: "capture one pane output once and classify next action",
	RunE:  runCheck,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.SetVersionTemplate(fmt.Sprintf("{{printf \"%%s\\ncommit: %%s\\nbuild-date: %%s\\n\" .Version %q %q}}", buildinfo.GitCommit, buildinfo.BuildDate))
	rootCmd.PersistentFlags().StringP("llm", "l", "glm", "llm selector: glm, codex, copilot, gemini, ollama/<model> or ollama with --llm-model (also accepts gemini-cli)")
	rootCmd.PersistentFlags().StringP("target", "t", "", "target tmux session name")
	rootCmd.PersistentFlags().String("locale", i18n.DefaultAppLocale, "UI and prompt locale (en, ko, ja, zh, vi, hi, ru, es, fr)")
	rootCmd.PersistentFlags().Int("interval-seconds", 4, "watch loop interval in seconds")
	rootCmd.PersistentFlags().Int("suspect-wait-seconds-1", 4, "first waiting suspicion recheck delay in seconds")
	rootCmd.PersistentFlags().Int("suspect-wait-seconds-2", 4, "second waiting suspicion recheck delay in seconds")
	rootCmd.PersistentFlags().Int("suspect-wait-seconds-3", 4, "third waiting suspicion recheck delay in seconds")
	rootCmd.PersistentFlags().Int("duration-seconds", 86400, "maximum watch duration in seconds")
	rootCmd.PersistentFlags().Int("capture-lines", 25, "number of visible lines to capture from target pane")
	rootCmd.PersistentFlags().Bool("capture-with-ansi", true, "capture tmux pane with ANSI escapes (-e)")
	rootCmd.PersistentFlags().StringP("continue-message", "c", "", "message sent when model waits for user input")
	rootCmd.PersistentFlags().String("policy", "default", "watcher policy: default, poc-completion, aggressive-architecture, parity-porting, creative-exploration")
	rootCmd.PersistentFlags().String("submit-key", "C-m", "tmux key used to submit the message")
	rootCmd.PersistentFlags().String("submit-key-fallback", "C-m", "fallback submit key for alternative mode")
	rootCmd.PersistentFlags().String("submit-key-fallback-delay", "0.15", "fallback submit key delay in seconds")
	rootCmd.PersistentFlags().Bool("disable-auto-update", false, "disable automatic github release self-update before running")
	rootCmd.PersistentFlags().String("github-token", "", "github token for auto-update API requests")
	rootCmd.PersistentFlags().String("github-repo", "dh-kam/yollo", "github repo for auto-update (owner/name)")
	rootCmd.PersistentFlags().Int("auto-update-retry-count", 2, "number of retry attempts for github update requests")
	rootCmd.PersistentFlags().String("auto-update-retry-delay", "0.5", "base retry delay in seconds for github update requests")
	rootCmd.PersistentFlags().Bool("auto-update-require-checksum", false, "require downloaded release artifact checksum verification")
	rootCmd.PersistentFlags().String("state-dir", "", "directory for watch state files")
	rootCmd.PersistentFlags().String("log-file", "", "path to log file")
	rootCmd.PersistentFlags().String("llm-model", "", "llm model override")
	rootCmd.PersistentFlags().String("fallback-llm", "", "fallback llm selector used if primary llm is unavailable or fails")
	rootCmd.PersistentFlags().String("fallback-llm-model", "", "fallback llm model override")

	_ = viper.BindPFlag("llm", rootCmd.PersistentFlags().Lookup("llm"))
	_ = viper.BindPFlag("target", rootCmd.PersistentFlags().Lookup("target"))
	_ = viper.BindPFlag("locale", rootCmd.PersistentFlags().Lookup("locale"))
	_ = viper.BindPFlag("interval-seconds", rootCmd.PersistentFlags().Lookup("interval-seconds"))
	_ = viper.BindPFlag("suspect-wait-seconds-1", rootCmd.PersistentFlags().Lookup("suspect-wait-seconds-1"))
	_ = viper.BindPFlag("suspect-wait-seconds-2", rootCmd.PersistentFlags().Lookup("suspect-wait-seconds-2"))
	_ = viper.BindPFlag("suspect-wait-seconds-3", rootCmd.PersistentFlags().Lookup("suspect-wait-seconds-3"))
	_ = viper.BindPFlag("duration-seconds", rootCmd.PersistentFlags().Lookup("duration-seconds"))
	_ = viper.BindPFlag("capture-lines", rootCmd.PersistentFlags().Lookup("capture-lines"))
	_ = viper.BindPFlag("capture-with-ansi", rootCmd.PersistentFlags().Lookup("capture-with-ansi"))
	_ = viper.BindPFlag("continue-message", rootCmd.PersistentFlags().Lookup("continue-message"))
	_ = viper.BindPFlag("policy", rootCmd.PersistentFlags().Lookup("policy"))
	_ = viper.BindPFlag("submit-key", rootCmd.PersistentFlags().Lookup("submit-key"))
	_ = viper.BindPFlag("submit-key-fallback", rootCmd.PersistentFlags().Lookup("submit-key-fallback"))
	_ = viper.BindPFlag("submit-key-fallback-delay", rootCmd.PersistentFlags().Lookup("submit-key-fallback-delay"))
	_ = viper.BindPFlag("disable-auto-update", rootCmd.PersistentFlags().Lookup("disable-auto-update"))
	_ = viper.BindPFlag("github-token", rootCmd.PersistentFlags().Lookup("github-token"))
	_ = viper.BindPFlag("github-repo", rootCmd.PersistentFlags().Lookup("github-repo"))
	_ = viper.BindPFlag("auto-update-retry-count", rootCmd.PersistentFlags().Lookup("auto-update-retry-count"))
	_ = viper.BindPFlag("auto-update-retry-delay", rootCmd.PersistentFlags().Lookup("auto-update-retry-delay"))
	_ = viper.BindPFlag("auto-update-require-checksum", rootCmd.PersistentFlags().Lookup("auto-update-require-checksum"))
	_ = viper.BindPFlag("state-dir", rootCmd.PersistentFlags().Lookup("state-dir"))
	_ = viper.BindPFlag("log-file", rootCmd.PersistentFlags().Lookup("log-file"))
	_ = viper.BindPFlag("llm-model", rootCmd.PersistentFlags().Lookup("llm-model"))
	_ = viper.BindPFlag("fallback-llm", rootCmd.PersistentFlags().Lookup("fallback-llm"))
	_ = viper.BindPFlag("fallback-llm-model", rootCmd.PersistentFlags().Lookup("fallback-llm-model"))

	viper.SetEnvPrefix("TMUX_YOLO")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	viper.SetDefault("llm", "glm")
	viper.SetDefault("target", "")
	viper.SetDefault("locale", i18n.DefaultAppLocale)
	viper.SetDefault("interval-seconds", 4)
	viper.SetDefault("suspect-wait-seconds-1", 4)
	viper.SetDefault("suspect-wait-seconds-2", 4)
	viper.SetDefault("suspect-wait-seconds-3", 4)
	viper.SetDefault("duration-seconds", 86400)
	viper.SetDefault("capture-lines", 25)
	viper.SetDefault("capture-with-ansi", true)
	viper.SetDefault("continue-message", "")
	viper.SetDefault("policy", "default")
	viper.SetDefault("submit-key", "C-m")
	viper.SetDefault("submit-key-fallback", "C-m")
	viper.SetDefault("submit-key-fallback-delay", "0.15")
	viper.SetDefault("disable-auto-update", false)
	viper.SetDefault("github-token", "")
	viper.SetDefault("github-repo", "dh-kam/yollo")
	viper.SetDefault("auto-update-retry-count", 2)
	viper.SetDefault("auto-update-retry-delay", "0.5")
	viper.SetDefault("auto-update-require-checksum", false)
	viper.SetDefault("state-dir", "")
	viper.SetDefault("log-file", "")
	viper.SetDefault("llm-model", "")
	viper.SetDefault("fallback-llm", "")
	viper.SetDefault("fallback-llm-model", "")

	rootCmd.AddCommand(watchCmd, checkCmd)
	checkCmd.Flags().String("capture-file", "", "offline mode: classify a saved capture file instead of querying tmux")
	watchCmd.Flags().Bool("once", false, "run exactly one watch cycle and exit")
	watchCmd.Flags().Bool("dry-run", false, "preview intended actions without sending any tmux keys")
	watchCmd.Flags().String("format", "plain", "dry-run output format: plain, json, or yaml")
}

func runWatch(cmd *cobra.Command, args []string) error {
	config, err := loadConfig(args)
	if err != nil {
		return err
	}
	once, err := cmd.Flags().GetBool("once")
	if err != nil {
		return err
	}
	config.once = once
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return err
	}
	outputFormat, err := cmd.Flags().GetString("format")
	if err != nil {
		return err
	}
	outputFormat, err = normalizeDryRunOutputFormat(outputFormat)
	if err != nil {
		return err
	}
	config.dryRun = dryRun
	config.dryRunOutputFormat = outputFormat

	ctx := cmd.Context()

	logBuffer := tui.NewLogBuffer(0)
	logger := newLogger(config.logFile, logBuffer)
	logger(i18n.T(config.locale, "cmd.watch_start", config.target, config.interval, config.duration))
	logger(i18n.T(config.locale, "cmd.watch_llm_status", config.llm, config.llmModel, config.fallbackLLM, config.fallbackLLMModel, config.policy, config.captureLines, config.suspectWait1, config.suspectWait2, config.suspectWait3))
	logger(i18n.T(config.locale, "cmd.watch_dryrun", config.dryRun, config.dryRunOutputFormat))
	logger(i18n.T(config.locale, "cmd.watch_auto_update_flag", config.autoUpdate))
	if err := applyAutoUpdate(ctx, logger, config); err != nil {
		logger(i18n.T(config.locale, "cmd.watch_auto_update_error", err))
	}

	tmuxClient, err := tmux.New()
	if err != nil {
		return err
	}
	logger(i18n.T(config.locale, "cmd.watch_tmux_target", config.target))

	sessions, err := tmuxClient.ListSessions(ctx)
	if err != nil {
		return fmt.Errorf(i18n.T(config.locale, "cmd.error_tmux_session_list"), err)
	}
	if len(sessions) > 0 {
		logger(i18n.T(config.locale, "cmd.watch_current_sessions"))
		for _, session := range sessions {
			logger(i18n.T(config.locale, "cmd.watch_session_item"), session.Name)
		}
	}
	runner := watchruntime.New(
		watchruntime.Config{
			Target:              config.target,
			CaptureLines:        config.captureLines,
			ContinueMessage:     config.continueMessage,
			SubmitKey:           config.submitKey,
			SubmitKeyFallback:   config.submitKeyFallback,
			SubmitFallbackDelay: config.submitFallbackDelay,
			BaseInterval:        time.Duration(config.interval) * time.Second,
			SuspectWait1:        time.Duration(config.suspectWait1) * time.Second,
			SuspectWait2:        time.Duration(config.suspectWait2) * time.Second,
			SuspectWait3:        time.Duration(config.suspectWait3) * time.Second,
			Duration:            time.Duration(config.duration) * time.Second,
			LLMName:             config.llm,
			LLMModel:            config.llmModel,
			FallbackLLMName:     config.fallbackLLM,
			FallbackLLMModel:    config.fallbackLLMModel,
			Locale:              config.locale,
			PolicyName:          config.policy,
			Once:                config.once,
			DryRun:              config.dryRun,
			DryRunOutputFormat:  config.dryRunOutputFormat,
			LogBuffer:           logBuffer,
			PromptReader:        os.Stdin,
		},
		tmuxClient,
		logger,
	)
	if config.once {
		if err := runner.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	} else {
		if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	}
	logger(i18n.T(config.locale, "cmd.watch_end"))
	return nil
}

func applyAutoUpdate(ctx context.Context, logger func(string, ...interface{}), config watchConfig) error {
	if !config.autoUpdate {
		return nil
	}

	if !strings.Contains(config.autoUpdateRepo, "/") {
		return fmt.Errorf("github-repo format must be owner/repo: %s", config.autoUpdateRepo)
	}

	variant := strings.TrimSpace(buildinfo.Variant)
	if variant == "" || strings.EqualFold(variant, "unknown") {
		variant = "release"
	}

	newVersion, updated, err := updater.SelfUpdate(ctx, updater.Config{
		Repo:              config.autoUpdateRepo,
		AppName:           buildinfo.AppName,
		Variant:           variant,
		OS:                runtime.GOOS,
		Arch:              runtime.GOARCH,
		CurrentVersion:    buildinfo.Version,
		AuthToken:         config.autoUpdateToken,
		MaxRetryCount:     config.autoUpdateRetryCount,
		RetryDelay:        config.autoUpdateRetryDelay,
		RequireChecksum:   config.autoUpdateRequireChecksum,
		Logger:            logger,
		AllowPrerelease:   false,
		CurrentExecutable: "",
	})
	if err != nil {
		return err
	}
	if !updated {
		return nil
	}

	logger(i18n.T(config.locale, "cmd.watch_auto_update_applied", buildinfo.Version, strings.TrimSpace(newVersion)))
	logger(i18n.T(config.locale, "cmd.watch_auto_update_restart"))
	return updater.RestartSelf()
}

func runCheck(cmd *cobra.Command, args []string) error {
	config, err := loadConfig(args)
	if err != nil {
		return err
	}

	ctx := cmd.Context()

	logger := newLogger(config.logFile, nil)
	logger(i18n.T(config.locale, "cmd.check_start", config.target, config.captureLines))
	logger(i18n.T(config.locale, "cmd.check_llm_status", config.llm, config.llmModel, config.fallbackLLM, config.fallbackLLMModel))
	captureFile, err := cmd.Flags().GetString("capture-file")
	if err != nil {
		return err
	}
	captureFile = strings.TrimSpace(captureFile)

	llmManager, providerLabel, err := initCheckProvider(ctx, config, logger)
	if err != nil {
		return err
	}
	logger(i18n.T(config.locale, "cmd.check_provider_selected", providerLabel))
	logger(i18n.T(config.locale, "cmd.watch_tmux_target", config.target))
	if captureFile != "" {
		logger(i18n.T(config.locale, "cmd.check_offline_mode", captureFile))
	}

	timestamp := time.Now().In(seoulLocation).Format("20060102-150405")
	statePromptPath := filepath.Join(config.stateDir, fmt.Sprintf("session-state-prompt-%s.txt", timestamp))
	sessionStatePath := filepath.Join(config.stateDir, fmt.Sprintf("session-state-%s.txt", timestamp))
	capturePath := filepath.Join(config.stateDir, fmt.Sprintf("capture-%s.txt", timestamp))
	promptPath := filepath.Join(config.stateDir, fmt.Sprintf("prompt-%s.txt", timestamp))
	decisionPath := filepath.Join(config.stateDir, fmt.Sprintf("decision-%s.txt", timestamp))

	if captureFile != "" {
		rawCapture, err := os.ReadFile(captureFile)
		if err != nil {
			return fmt.Errorf(i18n.T(config.locale, "cmd.error_capture_file_read", captureFile), err)
		}
		captureText := string(rawCapture)
		if err := writeStateTextFile(capturePath, rawCapture, config.locale, logger); err != nil {
			logger(i18n.T(config.locale, "cmd.warn_capture_text_write", capturePath, err))
		}

		displayText := stripANSICodes(captureText, config.captureWithANSI)
		emptyCaptureDecision := captureDecision{
			Action: "SKIP",
			Status: "EMPTY",
			Reason: i18n.T(config.locale, "cmd.reason_empty_capture"),
		}
		fmt.Printf("SESSION_STATE=OFFLINE\n")
		if strings.TrimSpace(displayText) == "" {
			writeCaptureArtifacts(
				config.locale,
				logger,
				config.stateDir,
				"check",
				config.target,
				timestamp,
				1,
				llmManager.Name(),
				config.llmModel,
				config.captureLines,
				config.captureWithANSI,
				captureText,
				promptPath,
				decisionPath,
				emptyCaptureDecision,
			)
			fmt.Printf("DECISION=%s\n", emptyCaptureDecision.Action)
			fmt.Printf("STATUS=%s\n", emptyCaptureDecision.Status)
			fmt.Printf("WORKING=%t\n", emptyCaptureDecision.Working)
			fmt.Printf("MULTIPLE_CHOICE=%t\n", emptyCaptureDecision.MultiChoice)
			fmt.Printf("COMPLETED=%t\n", emptyCaptureDecision.Completed)
			fmt.Printf("REASON=%s\n", emptyCaptureDecision.Reason)
			logger(i18n.T(config.locale, "cmd.check_offline_empty"))
			return nil
		}

		fmt.Printf("CAPTURE_START\n%s\nCAPTURE_END\n", displayText)
		decision := classifyNeedToContinueFromReader(
			ctx,
			llmManager,
			bytes.NewReader(rawCapture),
			logger,
			promptPath,
			decisionPath,
			captureFile,
			config.llm,
			config.llmModel,
			config.locale,
		)
		writeCaptureArtifacts(
			config.locale,
			logger,
			config.stateDir,
			"check",
			config.target,
			timestamp,
			1,
			llmManager.Name(),
			config.llmModel,
			config.captureLines,
			config.captureWithANSI,
			captureText,
			promptPath,
			decisionPath,
			decision,
		)
		fmt.Printf("DECISION=%s\n", decision.Action)
		fmt.Printf("STATUS=%s\n", decision.Status)
		fmt.Printf("WORKING=%t\n", decision.Working)
		fmt.Printf("MULTIPLE_CHOICE=%t\n", decision.MultiChoice)
		fmt.Printf("COMPLETED=%t\n", decision.Completed)
		fmt.Printf("REASON=%s\n", decision.Reason)
		if decision.RecommendedChoice != "" {
			fmt.Printf("RECOMMENDED_CHOICE=%s\n", decision.RecommendedChoice)
		}
		logger(i18n.T(config.locale, "cmd.check_result", decision.Action, decision.Reason))
		return nil
	}

	tmuxClient, err := tmux.New()
	if err != nil {
		return err
	}

	sessionState := checkTmuxSessionState(
		ctx,
		llmManager,
		tmuxClient,
		config.target,
		logger,
		config.locale,
		statePromptPath,
		sessionStatePath,
	)
	fmt.Printf("SESSION_STATE=%s\n", sessionState)
	if sessionState != "EXISTS" {
		fmt.Printf("%s\n", i18n.T(config.locale, "cmd.check_session_not_found"))
		return nil
	}

	capture, err := tmuxClient.CapturePane(ctx, config.target, config.captureLines, config.captureWithANSI)
	if err != nil {
		return fmt.Errorf(i18n.T(config.locale, "cmd.error_tmux_capture_failed", config.target), err)
	}
	captureText := stripANSICodes(capture, config.captureWithANSI)
	if err := writeStateTextFile(capturePath, []byte(capture), config.locale, logger); err != nil {
		logger(i18n.T(config.locale, "cmd.warn_capture_text_write", capturePath, err))
	}
	emptyCaptureDecision := captureDecision{
		Action: "SKIP",
		Status: "EMPTY",
		Reason: i18n.T(config.locale, "cmd.reason_empty_capture"),
	}
	if strings.TrimSpace(captureText) == "" {
		writeCaptureArtifacts(
			config.locale,
			logger,
			config.stateDir,
			"check",
			config.target,
			timestamp,
			1,
			llmManager.Name(),
			config.llmModel,
			config.captureLines,
			config.captureWithANSI,
			capture,
			promptPath,
			decisionPath,
			emptyCaptureDecision,
		)
		fmt.Printf("CAPTURE_STATE=EMPTY\n")
		return nil
	}
	fmt.Printf("CAPTURE_START\n%s\nCAPTURE_END\n", captureText)

	decision := classifyNeedToContinue(
		ctx,
		llmManager,
		capture,
		logger,
		promptPath,
		decisionPath,
		"",
		config.llm,
		config.llmModel,
		config.locale,
	)
	writeCaptureArtifacts(
		config.locale,
		logger,
		config.stateDir,
		"check",
		config.target,
		timestamp,
		1,
		llmManager.Name(),
		config.llmModel,
		config.captureLines,
		config.captureWithANSI,
		capture,
		promptPath,
		decisionPath,
		decision,
	)
	fmt.Printf("DECISION=%s\n", decision.Action)
	fmt.Printf("STATUS=%s\n", decision.Status)
	fmt.Printf("WORKING=%t\n", decision.Working)
	fmt.Printf("MULTIPLE_CHOICE=%t\n", decision.MultiChoice)
	fmt.Printf("COMPLETED=%t\n", decision.Completed)
	fmt.Printf("REASON=%s\n", decision.Reason)
	if decision.RecommendedChoice != "" {
		fmt.Printf("RECOMMENDED_CHOICE=%s\n", decision.RecommendedChoice)
	}
	logger(i18n.T(config.locale, "cmd.check_result", decision.Action, decision.Reason))

	return nil
}

type watchConfig struct {
	locale                    string
	llm                       string
	fallbackLLM               string
	target                    string
	interval                  int
	suspectWait1              int
	suspectWait2              int
	suspectWait3              int
	duration                  int
	captureLines              int
	continueMessage           string
	policy                    string
	submitKey                 string
	submitKeyFallback         string
	submitFallbackDelay       float64
	stateDir                  string
	logFile                   string
	llmModel                  string
	fallbackLLMModel          string
	captureWithANSI           bool
	once                      bool
	dryRun                    bool
	dryRunOutputFormat        string
	autoUpdate                bool
	autoUpdateRepo            string
	autoUpdateToken           string
	autoUpdateRetryCount      int
	autoUpdateRetryDelay      time.Duration
	autoUpdateRequireChecksum bool
}

func loadConfig(args []string) (watchConfig, error) {
	target := strings.TrimSpace(viper.GetString("target"))
	target = firstNonEmpty(os.Getenv("TMUX_SESSION"), target)
	if target == "" && len(args) > 0 {
		target = strings.TrimSpace(args[0])
	}
	if target == "" {
		target = "dev-pdf-codex"
	}

	interval := intFromEnv("INTERVAL_SECONDS", viper.GetInt("interval-seconds"))
	if interval <= 0 {
		return watchConfig{}, errors.New(i18n.T(i18n.DefaultAppLocale, "cmd.error_interval"))
	}
	suspectWait1 := intFromEnv("SUSPECT_WAIT_SECONDS_1", viper.GetInt("suspect-wait-seconds-1"))
	if suspectWait1 <= 0 {
		return watchConfig{}, errors.New(i18n.T(i18n.DefaultAppLocale, "cmd.error_suspect_wait_1"))
	}
	suspectWait2 := intFromEnv("SUSPECT_WAIT_SECONDS_2", viper.GetInt("suspect-wait-seconds-2"))
	if suspectWait2 <= 0 {
		return watchConfig{}, errors.New(i18n.T(i18n.DefaultAppLocale, "cmd.error_suspect_wait_2"))
	}
	suspectWait3 := intFromEnv("SUSPECT_WAIT_SECONDS_3", viper.GetInt("suspect-wait-seconds-3"))
	if suspectWait3 <= 0 {
		return watchConfig{}, errors.New(i18n.T(i18n.DefaultAppLocale, "cmd.error_suspect_wait_3"))
	}

	duration := intFromEnv("DURATION_SECONDS", viper.GetInt("duration-seconds"))
	if duration <= 0 {
		return watchConfig{}, errors.New(i18n.T(i18n.DefaultAppLocale, "cmd.error_duration"))
	}

	captureLines := intFromEnv("CAPTURE_LINES", viper.GetInt("capture-lines"))
	if captureLines <= 0 {
		return watchConfig{}, errors.New(i18n.T(i18n.DefaultAppLocale, "cmd.error_capture_lines"))
	}
	captureWithANSI := boolFromEnv("CAPTURE_WITH_ANSI", viper.GetBool("capture-with-ansi"))
	autoUpdateDisabled := boolFromEnv("DISABLE_AUTO_UPDATE", viper.GetBool("disable-auto-update"))
	autoUpdate := !autoUpdateDisabled
	autoUpdateRepo := strings.TrimSpace(firstNonEmpty(os.Getenv("GITHUB_REPO"), viper.GetString("github-repo")))
	if autoUpdateRepo == "" {
		autoUpdateRepo = "dh-kam/yollo"
	}
	autoUpdateToken := firstNonEmpty(os.Getenv("GITHUB_TOKEN"), strings.TrimSpace(viper.GetString("github-token")))
	autoUpdateRetryCount := intFromEnv("AUTO_UPDATE_RETRY_COUNT", viper.GetInt("auto-update-retry-count"))
	autoUpdateRetryDelaySeconds := parseFloat(
		firstNonEmpty(os.Getenv("AUTO_UPDATE_RETRY_DELAY"), viper.GetString("auto-update-retry-delay")),
		0.5,
	)
	autoUpdateRetryDelay := time.Duration(autoUpdateRetryDelaySeconds * float64(time.Second))
	autoUpdateRequireChecksum := boolFromEnv("AUTO_UPDATE_REQUIRE_CHECKSUM", viper.GetBool("auto-update-require-checksum"))
	rawLocale := firstNonEmpty(os.Getenv("TMUX_YOLO_LOCALE"), firstNonEmpty(os.Getenv("LOCALE"), viper.GetString("locale")))
	if !i18n.IsSupportedLocale(rawLocale) {
		rawLocale = i18n.DefaultAppLocale
	}
	locale := i18n.NormalizeLocale(rawLocale)

	continueMessage := firstNonEmpty(os.Getenv("CONTINUE_MESSAGE"), viper.GetString("continue-message"))
	policyName := policy.Resolve(firstNonEmpty(os.Getenv("POLICY"), viper.GetString("policy"))).Name()
	submitKey := firstNonEmpty(os.Getenv("SUBMIT_KEY"), viper.GetString("submit-key"))
	submitFallback := firstNonEmpty(os.Getenv("SUBMIT_KEY_FALLBACK"), viper.GetString("submit-key-fallback"))
	submitFallbackDelay := parseFloat(
		firstNonEmpty(os.Getenv("SUBMIT_KEY_FALLBACK_DELAY"), viper.GetString("submit-key-fallback-delay")),
		0.15,
	)
	stateDir := firstNonEmpty(os.Getenv("STATE_DIR"), viper.GetString("state-dir"))
	if stateDir == "" {
		stateDir = defaultStateDir()
	}

	logFile := firstNonEmpty(os.Getenv("LOG_FILE"), viper.GetString("log-file"))
	if logFile == "" {
		logFile = filepath.Join(stateDir, "watch.log")
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return watchConfig{}, errors.New(i18n.T(i18n.DefaultAppLocale, "cmd.error_watch_state_dir", err))
	}

	llmName := strings.ToLower(strings.TrimSpace(viper.GetString("llm")))
	if llmName == "" {
		llmName = "glm"
	}
	parsedLLMName, parsedLLMModel := parseLLMNameAndModel(llmName)
	llmModel := firstNonEmpty(os.Getenv("LLM_MODEL"), os.Getenv("CODEX_MODEL"), viper.GetString("llm-model"))
	if parsedLLMModel != "" {
		llmModel = parsedLLMModel
	}
	fallbackLLMRaw := strings.ToLower(strings.TrimSpace(firstNonEmpty(os.Getenv("FALLBACK_LLM"), viper.GetString("fallback-llm"))))
	parsedFallbackLLMName, parsedFallbackLLMModel := parseLLMNameAndModel(fallbackLLMRaw)
	fallbackLLMModel := firstNonEmpty(os.Getenv("FALLBACK_LLM_MODEL"), viper.GetString("fallback-llm-model"))
	if parsedFallbackLLMModel != "" {
		fallbackLLMModel = parsedFallbackLLMModel
	}
	if parsedFallbackLLMName == parsedLLMName && fallbackLLMModel == llmModel {
		parsedFallbackLLMName = ""
		fallbackLLMModel = ""
	}

	return watchConfig{
		locale:                    locale,
		llm:                       parsedLLMName,
		fallbackLLM:               parsedFallbackLLMName,
		target:                    target,
		interval:                  interval,
		suspectWait1:              suspectWait1,
		suspectWait2:              suspectWait2,
		suspectWait3:              suspectWait3,
		duration:                  duration,
		captureLines:              captureLines,
		continueMessage:           continueMessage,
		policy:                    policyName,
		submitKey:                 submitKey,
		submitKeyFallback:         submitFallback,
		submitFallbackDelay:       submitFallbackDelay,
		stateDir:                  stateDir,
		logFile:                   logFile,
		llmModel:                  llmModel,
		fallbackLLMModel:          fallbackLLMModel,
		captureWithANSI:           captureWithANSI,
		dryRun:                    false,
		dryRunOutputFormat:        "plain",
		autoUpdate:                autoUpdate,
		autoUpdateRepo:            autoUpdateRepo,
		autoUpdateToken:           autoUpdateToken,
		autoUpdateRetryCount:      autoUpdateRetryCount,
		autoUpdateRetryDelay:      autoUpdateRetryDelay,
		autoUpdateRequireChecksum: autoUpdateRequireChecksum,
	}, nil
}

func normalizeDryRunOutputFormat(raw string) (string, error) {
	format := strings.ToLower(strings.TrimSpace(raw))
	switch format {
	case "", "plain", "json", "yaml":
		if format == "" {
			format = "plain"
		}
		return format, nil
	default:
		return "", fmt.Errorf("unsupported dry-run format: %s", raw)
	}
}

func parseLLMNameAndModel(raw string) (string, string) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "glm", ""
	}
	if strings.HasPrefix(clean, "ollama/") {
		model := strings.TrimSpace(strings.TrimPrefix(clean, "ollama/"))
		if model == "" {
			return "ollama", ""
		}
		return "ollama", model
	}
	return clean, ""
}

func initCheckProvider(ctx context.Context, config watchConfig, logger func(string, ...interface{})) (llm.Provider, string, error) {
	candidates := []struct {
		name  string
		model string
		label string
	}{
		{name: config.llm, model: config.llmModel, label: "primary"},
	}
	if strings.TrimSpace(config.fallbackLLM) != "" {
		candidates = append(candidates, struct {
			name  string
			model string
			label string
		}{
			name:  config.fallbackLLM,
			model: config.fallbackLLMModel,
			label: "fallback",
		})
	}

	var errs []string
	for _, candidate := range candidates {
		provider, err := llm.New(candidate.name, candidate.model)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s=%v", candidate.label, err))
			continue
		}
		binary, err := provider.ValidateBinary()
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s=%v", candidate.label, err))
			continue
		}
		usage, err := provider.CheckUsage(ctx)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s=%v", candidate.label, err))
			continue
		}
		if usage.HasKnownLimit && usage.Remaining <= 0 {
			errs = append(errs, fmt.Sprintf("%s=quota exhausted (source=%s)", candidate.label, usage.Source))
			continue
		}
		if usage.HasKnownLimit {
			logger(i18n.T(config.locale, "cmd.log_llm_usage", candidate.label, usage.Source, usage.Remaining))
		} else {
			logger(i18n.T(config.locale, "cmd.log_llm_usage_unknown", candidate.label, usage.Source))
		}
		logger(i18n.T(config.locale, "cmd.log_llm_selected", candidate.label, candidate.name, binary))
		return provider, fmt.Sprintf("%s:%s/%s", candidate.label, candidate.name, candidate.model), nil
	}
	if len(errs) == 0 {
		return nil, "", errors.New(i18n.T(config.locale, "cmd.error_no_llm_provider"))
	}
	return nil, "", errors.New(i18n.T(config.locale, "cmd.error_llm_provider_init_failed", strings.Join(errs, "; ")))
}

func isCompletedCapturePath(path string) bool { return sessioncheck.IsCompletedCapturePath(path) }

func completedCaptureDecision(path string, locale string) captureDecision {
	return sessioncheck.CompletedCaptureDecision(path, locale)
}

func buildNeedToContinuePrompt(captureForLLM string, llmName string, llmModel string) string {
	return sessioncheck.BuildNeedToContinuePrompt(captureForLLM, llmName, llmModel)
}

func buildNeedToContinuePromptForLocale(captureForLLM string, llmName string, llmModel string, locale string) string {
	return sessioncheck.BuildNeedToContinuePromptForLocale(captureForLLM, llmName, llmModel, locale)
}

func checkTmuxSessionState(
	ctx context.Context,
	p llm.Provider,
	client tmux.API,
	target string,
	logger func(string, ...interface{}),
	locale string,
	promptFile string,
	stateFile string,
) string {
	return sessioncheck.CheckTmuxSessionState(ctx, p, client, target, logger, locale, func(path string, data []byte) error {
		return writeStateTextFile(path, data, locale, logger)
	}, promptFile, stateFile)
}

func classifyNeedToContinue(
	ctx context.Context,
	p llm.Provider,
	capture string,
	logger func(string, ...interface{}),
	promptPath string,
	decisionPath string,
	capturePath string,
	llmName string,
	llmModel string,
	locale string,
) captureDecision {
	return sessioncheck.ClassifyNeedToContinue(ctx, p, capture, logger, locale, func(path string, data []byte) error {
		return writeStateTextFile(path, data, locale, logger)
	}, promptPath, decisionPath, capturePath, llmName, llmModel)
}

func classifyNeedToContinueFromReader(
	ctx context.Context,
	p llm.Provider,
	capture io.Reader,
	logger func(string, ...interface{}),
	promptPath string,
	decisionPath string,
	capturePath string,
	llmName string,
	llmModel string,
	locale string,
) captureDecision {
	return sessioncheck.ClassifyNeedToContinueFromReader(ctx, p, capture, logger, locale, func(path string, data []byte) error {
		return writeStateTextFile(path, data, locale, logger)
	}, promptPath, decisionPath, capturePath, llmName, llmModel)
}

func parseCaptureDecision(out string, locale string) captureDecision {
	return sessioncheck.ParseDecision(out, locale)
}

func normalizeChoiceCandidate(raw string) string { return sessioncheck.NormalizeChoiceCandidate(raw) }

func parseChoiceCandidateAndLabel(raw string) (string, string) {
	return sessioncheck.ParseChoiceCandidateAndLabel(raw)
}

func parseTruthy(raw string) bool { return sessioncheck.ParseTruthy(raw) }

func isContinuationReadySignal(raw string) bool {
	text := strings.TrimSpace(strings.ToLower(raw))
	if text == "" {
		return false
	}

	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isCodexFooterLine(trimmed) {
			continue
		}
		compact := strings.Join(strings.Fields(trimmed), " ")
		if strings.Contains(compact, "원하면") {
			if strings.Contains(compact, "다음") || strings.Contains(compact, "진행") || strings.Contains(compact, "작업") || strings.Contains(compact, "확장") {
				return true
			}
		}
		if strings.Contains(compact, "그럼 다음") || strings.Contains(compact, "다음 꺼") || strings.Contains(compact, "next input") || strings.Contains(compact, "next step") {
			return true
		}
		if strings.Contains(compact, "다음 작업") && (strings.Contains(compact, "진행") || strings.Contains(compact, "요청")) {
			return true
		}
	}

	return false
}

func trimTailLines(value string, maxLines int) string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	if maxLines <= 0 || len(lines) <= maxLines {
		return strings.TrimRight(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	}
	start := len(lines) - maxLines
	return strings.TrimRight(strings.Join(lines[start:], "\n"), "\n")
}

func normalizeCaptureForWorkingCheck(raw string) string {
	raw = capture.StripANSI(raw)
	return strings.TrimRight(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
}

func intFromEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

func boolFromEnv(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseFloat(raw string, fallback float64) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func stripANSICodesAll(value string) string {
	return capture.StripANSI(value)
}

var codexFooterLinePattern = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^›?\s*Use\s+/skills\b.*$`),
	regexp.MustCompile(`(?i)^•\s*Context\s+compacted$`),
	regexp.MustCompile(`(?i)^\?\s*for\s+shortcuts\b.*$`),
	regexp.MustCompile(`(?i)^\d+\s*%\s*context\s+left\b.*$`),
}

func stripCodexFooterNoise(value string) string {
	normalized := strings.ReplaceAll(value, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")

	end := len(lines)
	for end > 0 {
		line := strings.TrimSpace(lines[end-1])
		if line == "" || isCodexFooterLine(line) {
			end--
			continue
		}
		break
	}

	return strings.TrimRight(strings.Join(lines[:end], "\n"), "\n")
}

func isCodexFooterLine(line string) bool {
	for _, pattern := range codexFooterLinePattern {
		if pattern.MatchString(line) {
			return true
		}
	}
	return false
}

func stripANSICodes(value string, includeANSICodes bool) string {
	if !includeANSICodes {
		return value
	}
	return stripANSICodesAll(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func defaultStateDir() string {
	execPath, err := os.Executable()
	if err != nil {
		return filepath.Join(".", ".watch-state")
	}
	return filepath.Join(filepath.Dir(execPath), ".watch-state")
}

func newLogger(logPath string, logBuffer *tui.LogBuffer) func(format string, args ...interface{}) {
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	return func(format string, args ...interface{}) {
		timestamp := time.Now().In(seoulLocation).Format("2006-01-02 15:04:05")
		message := fmt.Sprintf(format, args...)
		normalized := strings.ReplaceAll(message, "\r\n", "\n")
		lines := strings.Split(normalized, "\n")
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			if logBuffer != nil {
				for _, entry := range lines {
					logBuffer.Append(fmt.Sprintf("[%s] %s", timestamp, entry))
				}
			}
			return
		}
		defer f.Close()
		for _, entry := range lines {
			line := fmt.Sprintf("[%s] %s", timestamp, entry)
			if logBuffer != nil {
				logBuffer.Append(line)
			}
			_, _ = f.WriteString(line + "\n")
		}
	}
}
