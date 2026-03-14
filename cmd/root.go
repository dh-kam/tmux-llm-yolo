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
	"strconv"
	"strings"
	"time"

	"github.com/dh-kam/tmux-llm-yolo/internal/buildinfo"
	"github.com/dh-kam/tmux-llm-yolo/internal/llm"
	"github.com/dh-kam/tmux-llm-yolo/internal/policy"
	watchruntime "github.com/dh-kam/tmux-llm-yolo/internal/runtime"
	"github.com/dh-kam/tmux-llm-yolo/internal/tmux"
	"github.com/dh-kam/tmux-llm-yolo/internal/tui"
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

type captureDecision struct {
	Action            string
	Status            string
	Working           bool
	MultiChoice       bool
	Completed         bool
	RecommendedChoice string
	Reason            string
}

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
		logger("경고: captured 디렉터리 생성 실패 (%s): %s", capturedDir, err)
		return
	}

	rawCapturePath := filepath.Join(capturedDir, stem+".ansi.txt")
	plainCapturePath := filepath.Join(capturedDir, stem+".plain.txt")
	decisionJSONPath := filepath.Join(capturedDir, stem+".json")

	if err := writeStateTextFile(rawCapturePath, []byte(rawCapture), logger); err != nil {
		logger("경고: captured ANSI 저장 실패 (%s): %s", rawCapturePath, err)
	}
	plainCapture := stripANSICodesAll(rawCapture)
	if err := writeStateTextFile(plainCapturePath, []byte(plainCapture), logger); err != nil {
		logger("경고: captured plain 저장 실패 (%s): %s", plainCapturePath, err)
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
		logger("경고: 판정 JSON marshal 실패: %s", err)
		return
	}
	if err := os.WriteFile(decisionJSONPath, data, 0644); err != nil {
		logger("경고: 판정 JSON 저장 실패 (%s): %s", decisionJSONPath, err)
	}
}

func readTextFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func writeStateTextFile(path string, data []byte, logger func(string, ...interface{})) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logger("경고: 상태 파일 디렉터리 생성 실패 (%s): %s", filepath.Dir(path), err)
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
	rootCmd.PersistentFlags().Int("interval-seconds", 4, "watch loop interval in seconds")
	rootCmd.PersistentFlags().Int("suspect-wait-seconds-1", 4, "first waiting suspicion recheck delay in seconds")
	rootCmd.PersistentFlags().Int("suspect-wait-seconds-2", 4, "second waiting suspicion recheck delay in seconds")
	rootCmd.PersistentFlags().Int("suspect-wait-seconds-3", 4, "third waiting suspicion recheck delay in seconds")
	rootCmd.PersistentFlags().Int("duration-seconds", 86400, "maximum watch duration in seconds")
	rootCmd.PersistentFlags().Int("capture-lines", 25, "number of visible lines to capture from target pane")
	rootCmd.PersistentFlags().Bool("capture-with-ansi", true, "capture tmux pane with ANSI escapes (-e)")
	rootCmd.PersistentFlags().StringP("continue-message", "c", "응 계속 이어서 진행해서 포팅 완료까지 진행해보자", "message sent when model waits for user input")
	rootCmd.PersistentFlags().String("policy", "default", "watcher policy: default, poc-completion, aggressive-architecture, parity-porting, creative-exploration")
	rootCmd.PersistentFlags().String("submit-key", "C-m", "tmux key used to submit the message")
	rootCmd.PersistentFlags().String("submit-key-fallback", "C-m", "fallback submit key for alternative mode")
	rootCmd.PersistentFlags().String("submit-key-fallback-delay", "0.15", "fallback submit key delay in seconds")
	rootCmd.PersistentFlags().String("state-dir", "", "directory for watch state files")
	rootCmd.PersistentFlags().String("log-file", "", "path to log file")
	rootCmd.PersistentFlags().String("llm-model", "", "llm model override")
	rootCmd.PersistentFlags().String("fallback-llm", "", "fallback llm selector used if primary llm is unavailable or fails")
	rootCmd.PersistentFlags().String("fallback-llm-model", "", "fallback llm model override")

	_ = viper.BindPFlag("llm", rootCmd.PersistentFlags().Lookup("llm"))
	_ = viper.BindPFlag("target", rootCmd.PersistentFlags().Lookup("target"))
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
	viper.SetDefault("interval-seconds", 4)
	viper.SetDefault("suspect-wait-seconds-1", 4)
	viper.SetDefault("suspect-wait-seconds-2", 4)
	viper.SetDefault("suspect-wait-seconds-3", 4)
	viper.SetDefault("duration-seconds", 86400)
	viper.SetDefault("capture-lines", 25)
	viper.SetDefault("capture-with-ansi", true)
	viper.SetDefault("continue-message", "응 계속 이어서 진행해서 포팅 완료까지 진행해보자")
	viper.SetDefault("policy", "default")
	viper.SetDefault("submit-key", "C-m")
	viper.SetDefault("submit-key-fallback", "C-m")
	viper.SetDefault("submit-key-fallback-delay", "0.15")
	viper.SetDefault("state-dir", "")
	viper.SetDefault("log-file", "")
	viper.SetDefault("llm-model", "")
	viper.SetDefault("fallback-llm", "")
	viper.SetDefault("fallback-llm-model", "")

	rootCmd.AddCommand(watchCmd, checkCmd)
	checkCmd.Flags().String("capture-file", "", "offline mode: classify a saved capture file instead of querying tmux")
	watchCmd.Flags().Bool("once", false, "run exactly one watch cycle and exit")
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

	ctx := cmd.Context()

	logBuffer := tui.NewLogBuffer(0)
	logger := newLogger(config.logFile, logBuffer)
	logger("감시 시작: session=%s interval=%ds duration=%ds", config.target, config.interval, config.duration)
	logger("llm=%s model=%s fallback_llm=%s fallback_model=%s policy=%s capture=%d suspect_waits=%ds/%ds/%ds", config.llm, config.llmModel, config.fallbackLLM, config.fallbackLLMModel, config.policy, config.captureLines, config.suspectWait1, config.suspectWait2, config.suspectWait3)

	tmuxClient, err := tmux.New()
	if err != nil {
		return err
	}
	logger("tmux target: %s", config.target)

	sessions, err := tmuxClient.ListSessions(ctx)
	if err != nil {
		return fmt.Errorf("tmux 세션 목록 조회 실패: %w", err)
	}
	if len(sessions) > 0 {
		logger("현재 tmux 세션:")
		for _, session := range sessions {
			logger("  - %s", session.Name)
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
			PolicyName:          config.policy,
			Once:                config.once,
			LogBuffer:           logBuffer,
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
	logger("감시 종료")
	return nil
}

func runCheck(cmd *cobra.Command, args []string) error {
	config, err := loadConfig(args)
	if err != nil {
		return err
	}

	ctx := cmd.Context()

	logger := newLogger(config.logFile, nil)
	logger("1회 검사 시작: session=%s capture=%d", config.target, config.captureLines)
	logger("llm=%s model=%s fallback_llm=%s fallback_model=%s", config.llm, config.llmModel, config.fallbackLLM, config.fallbackLLMModel)
	captureFile, err := cmd.Flags().GetString("capture-file")
	if err != nil {
		return err
	}
	captureFile = strings.TrimSpace(captureFile)

	llmManager, providerLabel, err := initCheckProvider(ctx, config, logger)
	if err != nil {
		return err
	}
	logger("LLM provider selected: %s", providerLabel)
	logger("tmux target: %s", config.target)
	if captureFile != "" {
		logger("오프라인 판정 모드: capture file=%s", captureFile)
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
			return fmt.Errorf("capture 파일 읽기 실패 (%s): %w", captureFile, err)
		}
		captureText := string(rawCapture)
		if err := writeStateTextFile(capturePath, rawCapture, logger); err != nil {
			logger("경고: 캡처 결과 파일 저장 실패 (%s): %s", capturePath, err)
		}

		displayText := stripANSICodes(captureText, config.captureWithANSI)
		emptyCaptureDecision := captureDecision{
			Action: "SKIP",
			Status: "EMPTY",
			Reason: "trimmed capture is empty",
		}
		fmt.Printf("SESSION_STATE=OFFLINE\n")
		if strings.TrimSpace(displayText) == "" {
			writeCaptureArtifacts(
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
			logger("오프라인 체크 완료: 빈 캡처")
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
		)
		writeCaptureArtifacts(
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
		logger("오프라인 체크 결과: %s / %s", decision.Action, decision.Reason)
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
		statePromptPath,
		sessionStatePath,
	)
	fmt.Printf("SESSION_STATE=%s\n", sessionState)
	if sessionState != "EXISTS" {
		fmt.Printf("결과: 세션이 존재하지 않습니다\n")
		return nil
	}

	capture, err := tmuxClient.CapturePane(ctx, config.target, config.captureLines, config.captureWithANSI)
	if err != nil {
		return fmt.Errorf("tmux 패널 캡처 실패 (session=%s): %w", config.target, err)
	}
	captureText := stripANSICodes(capture, config.captureWithANSI)
	if err := writeStateTextFile(capturePath, []byte(capture), logger); err != nil {
		logger("경고: 캡처 결과 파일 저장 실패 (%s): %s", capturePath, err)
	}
	emptyCaptureDecision := captureDecision{
		Action: "SKIP",
		Status: "EMPTY",
		Reason: "trimmed capture is empty",
	}
	if strings.TrimSpace(captureText) == "" {
		writeCaptureArtifacts(
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
	)
	writeCaptureArtifacts(
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
	logger("체크 결과: %s / %s", decision.Action, decision.Reason)

	return nil
}

type watchConfig struct {
	llm                 string
	fallbackLLM         string
	target              string
	interval            int
	suspectWait1        int
	suspectWait2        int
	suspectWait3        int
	duration            int
	captureLines        int
	continueMessage     string
	policy              string
	submitKey           string
	submitKeyFallback   string
	submitFallbackDelay float64
	stateDir            string
	logFile             string
	llmModel            string
	fallbackLLMModel    string
	captureWithANSI     bool
	once                bool
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
		return watchConfig{}, fmt.Errorf("interval-seconds는 0보다 커야 합니다")
	}
	suspectWait1 := intFromEnv("SUSPECT_WAIT_SECONDS_1", viper.GetInt("suspect-wait-seconds-1"))
	if suspectWait1 <= 0 {
		return watchConfig{}, fmt.Errorf("suspect-wait-seconds-1은 0보다 커야 합니다")
	}
	suspectWait2 := intFromEnv("SUSPECT_WAIT_SECONDS_2", viper.GetInt("suspect-wait-seconds-2"))
	if suspectWait2 <= 0 {
		return watchConfig{}, fmt.Errorf("suspect-wait-seconds-2은 0보다 커야 합니다")
	}
	suspectWait3 := intFromEnv("SUSPECT_WAIT_SECONDS_3", viper.GetInt("suspect-wait-seconds-3"))
	if suspectWait3 <= 0 {
		return watchConfig{}, fmt.Errorf("suspect-wait-seconds-3은 0보다 커야 합니다")
	}

	duration := intFromEnv("DURATION_SECONDS", viper.GetInt("duration-seconds"))
	if duration <= 0 {
		return watchConfig{}, fmt.Errorf("duration-seconds는 0보다 커야 합니다")
	}

	captureLines := intFromEnv("CAPTURE_LINES", viper.GetInt("capture-lines"))
	if captureLines <= 0 {
		return watchConfig{}, fmt.Errorf("capture-lines는 0보다 커야 합니다")
	}
	captureWithANSI := boolFromEnv("CAPTURE_WITH_ANSI", viper.GetBool("capture-with-ansi"))

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
		return watchConfig{}, fmt.Errorf("watch state 디렉터리 생성 실패: %w", err)
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
		llm:                 parsedLLMName,
		fallbackLLM:         parsedFallbackLLMName,
		target:              target,
		interval:            interval,
		suspectWait1:        suspectWait1,
		suspectWait2:        suspectWait2,
		suspectWait3:        suspectWait3,
		duration:            duration,
		captureLines:        captureLines,
		continueMessage:     continueMessage,
		policy:              policyName,
		submitKey:           submitKey,
		submitKeyFallback:   submitFallback,
		submitFallbackDelay: submitFallbackDelay,
		stateDir:            stateDir,
		logFile:             logFile,
		llmModel:            llmModel,
		fallbackLLMModel:    fallbackLLMModel,
		captureWithANSI:     captureWithANSI,
	}, nil
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
			logger("LLM 사용량(%s): source=%s, remaining=%d", candidate.label, usage.Source, usage.Remaining)
		} else {
			logger("LLM 사용량(%s): 공급자가 제공하지 않음 (source=%s)", candidate.label, usage.Source)
		}
		logger("LLM(%s): %s (%s)", candidate.label, candidate.name, binary)
		return provider, fmt.Sprintf("%s:%s/%s", candidate.label, candidate.name, candidate.model), nil
	}
	if len(errs) == 0 {
		return nil, "", fmt.Errorf("사용 가능한 llm provider가 없습니다")
	}
	return nil, "", fmt.Errorf("llm provider 초기화 실패: %s", strings.Join(errs, "; "))
}

func isCompletedCapturePath(path string) bool {
	name := filepath.Base(strings.TrimSpace(path))
	if name == "" {
		return false
	}
	return strings.Contains(strings.ToLower(name), ".completed.") || strings.HasSuffix(strings.ToLower(name), ".completed")
}

func completedCaptureDecision(path string) captureDecision {
	return captureDecision{
		Action:    "INJECT_CONTINUE",
		Status:    "COMPLETED",
		Completed: true,
		Reason:    "capture file name indicates completed fixture: " + filepath.Base(strings.TrimSpace(path)),
	}
}

func llmPromptHint(name string, model string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case "gemini":
		return `
LLM-specific note: For gemini, prioritize WAITING/COMPLETED only when the visible text clearly indicates completion handoff.
Avoid false positives from progress/status metadata.`
	case "codex":
		return `
LLM-specific note: For codex, return exactly six lines and only from terminal evidence.
Prefer ACTION=INJECT_CONTINUE with STATUS=COMPLETED/WAITING when a prompt is ready for free input.`
	case "copilot":
		return `
LLM-specific note: For copilot, treat short completion confirmations (yes/no, proceed?, next?) as STATUS=WAITING or COMPLETED.
Output INJECT_CONTINUE if the screen is not working.`
	case "glm":
		return `
LLM-specific note: For glm, classify only by terminal content and set COMPLETED/WAITING when a handoff prompt is shown.`
	case "ollama":
		normalizedModel := strings.TrimSpace(model)
		if normalizedModel == "" {
			normalizedModel = "default"
		}
		return fmt.Sprintf(`
LLM-specific note: For ollama/%s, prefer STATUS=COMPLETED and ACTION=INJECT_CONTINUE when ready for next input.
If screen is fully idle, do not return WORKING/ASKING.`, normalizedModel)
	default:
		return ""
	}
}

func buildNeedToContinuePrompt(captureForLLM string, llmName string, llmModel string) string {
	return fmt.Sprintf(`You are a strict classifier.
Determine the terminal state and classify user intent from the following terminal capture.
Context assumptions:
- This output is from an autonomous terminal session; we only know what's on screen.
- We want to decide whether to inject a new user action now, and if needed which action to inject.
- "작업중" means a command is still active, progressing, or the screen is showing a non-terminal prompt state.
- 사용자 요청은 자유 응답, 완료 후 다음 입력 대기, 또는 객관식 선택지 중 하나일 수 있다.
- For provider %s/%s, prefer WAITING/COMPLETED + INJECT_CONTINUE for short natural language confirmations of readiness.

	Return exactly 6 lines in this format:
	ACTION: INJECT_CONTINUE, INJECT_SELECT, INJECT_INPUT, or SKIP
	STATUS: WORKING | ASKING | COMPLETED | WAITING | UNKNOWN
	WORKING: true or false
	MULTIPLE_CHOICE: true or false
	RECOMMENDED_CHOICE: <number or number + option label>
	REASON: <one short sentence>

	Rules:
	- ACTION should be INJECT_CONTINUE when it looks like the model is awaiting a free-text response or can safely proceed after completion.
	- ACTION may be INJECT_INPUT when a specific free-text value should be entered directly (including a short command-like token).
	- ACTION should be INJECT_SELECT when numbered objectives are shown and one choice number should be typed.
	- For INJECT_SELECT, RECOMMENDED_CHOICE should include option number and label when possible (e.g. "3) Exit").
	- STATUS=ASKING when options like "1) ..., 2) ..., 3) ..." are shown and the output asks for exactly one answer.
	- For ASKING/INJECT_SELECT, MULTIPLE_CHOICE=true and RECOMMENDED_CHOICE should be the highest-priority option number (usually 1 if no stronger signal).
	- Ignore footer lines like "Use /skills", "Context compacted", "? for shortcuts", and percentage context left.
	- WORKING signals include long-running commands, spinners, progress bars, running tests/builds, and text like "esc to interrupt", "작업 중", "processing", "press ctrl+c".
	- If working signals exist, STATUS should be WORKING and ACTION SKIP, even if any choice-like text is also present.
	- Ignore footer lines like "Use /skills", "Context compacted", "? for shortcuts", and percentage context left.
	- COMPLETED/WAITING means short completion or handoff prompts such as "next input", "원하면", "그럼 다음", "다음 작업", "Ready for next step", "what would you like me to do next".
	- If unclear, set STATUS=UNKNOWN and ACTION=SKIP.
	- REASON must include concrete evidence from the capture in one short sentence.
	- Do not output anything except those 6 lines.

	Few-shot examples:
	- Example 1:
		Terminal:
		1) Build project
		2) Run tests
		3) Exit
		Which one do you want?
		ACTION: INJECT_SELECT
		STATUS: ASKING
		WORKING: false
		MULTIPLE_CHOICE: true
		RECOMMENDED_CHOICE: 1
		REASON: 번호형 메뉴가 표시되고 하나의 번호 입력이 요구됨.

	- Example 2:
		Terminal:
		Tests finished with warnings.
		다음 작업을 진행할까요? (yes / no)
		ACTION: INJECT_CONTINUE
		STATUS: COMPLETED
		WORKING: false
		MULTIPLE_CHOICE: false
		RECOMMENDED_CHOICE: none
		REASON: 완료 후 다음 입력을 기다리는 완료 상태로 판단됨.

	- Example 3:
		Terminal:
		Running lint...
		████████ 78%% [elapsed: 00:01:12] esc to interrupt
		ACTION: SKIP
		STATUS: WORKING
		WORKING: true
		MULTIPLE_CHOICE: false
		RECOMMENDED_CHOICE: none
		REASON: 진행률/진행중 표기와 인터럽트 안내가 있어 아직 작업이 진행 중임.

[Terminal Output Start]
%s
[Terminal Output End]
%s`, llmName, llmModel, captureForLLM, llmPromptHint(llmName, llmModel))
}

func checkTmuxSessionState(
	ctx context.Context,
	p llm.Provider,
	client tmux.API,
	target string,
	logger func(string, ...interface{}),
	promptFile string,
	stateFile string,
) string {
	sessions, err := client.ListSessions(ctx)
	if err != nil {
		logger("경고: tmux 세션 목록 조회 실패: %s", err)
		exists, hasErr := client.HasSession(ctx, target)
		if hasErr != nil {
			logger("경고: tmux 세션 확인 폴백 실패: %s", hasErr)
			return "MISSING"
		}
		if exists {
			return "EXISTS"
		}
		return "MISSING"
	}

	var names []string
	for _, session := range sessions {
		names = append(names, session.Name)
	}

	prompt := fmt.Sprintf(`You are a strict binary classifier.
Decide whether the target tmux session currently exists among the listed sessions.

Return exactly 1 line in this format:
STATE: EXISTS or MISSING

Rules:
- Return EXISTS only if the target session name is present.
- Return MISSING if the target session name is not present.
- Output only that one line, nothing else.

Target session: %s
Sessions:
%s
`, target, strings.Join(names, "\n"))

	if err := writeStateTextFile(promptFile, []byte(prompt), logger); err != nil {
		logger("경고: 세션 상태 프롬프트 저장 실패 (%s): %s", promptFile, err)
	}

	startAt := time.Now()
	logger("LLM 요청 시작: session-state 판정 (%s, binary=%s, prompt_len=%d)", p.Name(), p.Binary(), len(prompt))
	out, err := p.RunPrompt(ctx, prompt)
	elapsed := time.Since(startAt)
	if err != nil {
		logger("LLM 응답 완료: session-state 판정 실패 (%s) elapsed=%s", err, elapsed)
		exists, hasErr := client.HasSession(ctx, target)
		if hasErr != nil {
			logger("경고: tmux 세션 확인 폴백 실패: %s", hasErr)
			return "MISSING"
		}
		if exists {
			return "EXISTS"
		}
		return "MISSING"
	}
	logger("LLM 응답 완료: session-state 판정 성공 (elapsed=%s)", elapsed)
	if err := writeStateTextFile(stateFile, []byte(out), logger); err != nil {
		logger("경고: 세션 상태 결과 저장 실패 (%s): %s", stateFile, err)
	}

	if strings.Contains(out, "EXISTS") {
		return "EXISTS"
	}
	if strings.Contains(out, "MISSING") {
		return "MISSING"
	}

	logger("경고: 세션 판정 파싱 실패: %s", out)
	exists, hasErr := client.HasSession(ctx, target)
	if hasErr != nil {
		logger("경고: tmux 세션 확인 폴백 실패: %s", hasErr)
		return "MISSING"
	}
	if exists {
		return "EXISTS"
	}
	return "MISSING"
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
) captureDecision {
	return classifyNeedToContinueFromReader(
		ctx,
		p,
		strings.NewReader(capture),
		logger,
		promptPath,
		decisionPath,
		capturePath,
		llmName,
		llmModel,
	)
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
) captureDecision {
	providerName := strings.TrimSpace(llmName)
	if providerName == "" {
		providerName = p.Name()
	}

	captureObj, err := llm.NewCaptureFromReader(capture)
	if err != nil {
		return captureDecision{
			Action: "SKIP",
			Status: "UNKNOWN",
			Reason: "capture read failed",
		}
	}
	captureText := captureObj.Text
	captureForLLM := stripANSICodesAll(captureText)
	captureForLLM = stripCodexFooterNoise(captureForLLM)
	captureForLLM = trimTailLines(captureForLLM, 25)
	trimmedCapture := strings.TrimSpace(captureForLLM)
	if trimmedCapture == "" {
		return captureDecision{
			Action:  "SKIP",
			Status:  "EMPTY",
			Reason:  "캡처 결과가 비어 있어 판정 보류",
			Working: false,
		}
	}

	if isCompletedCapturePath(capturePath) {
		return completedCaptureDecision(capturePath)
	}

	if working, evidence := p.IsProgressingCapture(llm.NewCapture(captureText)); working {
		if strings.TrimSpace(evidence) == "" {
			evidence = "프롬프트 위를 기반으로 추정"
		}
		return captureDecision{
			Action:  "SKIP",
			Status:  "WORKING",
			Working: true,
			Reason:  "작업 중으로 판단: " + evidence,
		}
	}

	prompt := buildNeedToContinuePrompt(captureForLLM, providerName, llmModel)

	if err := writeStateTextFile(promptPath, []byte(prompt), logger); err != nil {
		logger("경고: 판정 프롬프트 저장 실패 (%s): %s", promptPath, err)
	}

	startAt := time.Now()
	logger("LLM 요청 시작: 판정 NEED_CONTINUE 요청 (%s, binary=%s, prompt_len=%d)", p.Name(), p.Binary(), len(prompt))
	out, err := p.RunPrompt(ctx, prompt)
	elapsed := time.Since(startAt)
	if err != nil {
		logger("LLM 응답 완료: 판정 NEED_CONTINUE 실패 (%s) elapsed=%s", err, elapsed)
		_ = writeStateTextFile(decisionPath, []byte(fmt.Sprintf("ERROR: %s", err)), logger)
		return captureDecision{
			Action: "SKIP",
			Status: "UNKNOWN",
			Reason: fmt.Sprintf("llm execution failed: %s", err),
		}
	}
	logger("LLM 응답 완료: 판정 NEED_CONTINUE 성공 (elapsed=%s)", elapsed)
	if err := writeStateTextFile(decisionPath, []byte(out), logger); err != nil {
		logger("경고: 판정 결과 저장 실패 (%s): %s", decisionPath, err)
	}

	decision := parseCaptureDecision(out)
	if decision.Status == "WORKING" && isContinuationReadySignal(captureForLLM) {
		logger("판정 후처리: completion-ready signal 감지로 WORKING->COMPLETED 강제 전환")
		decision.Status = "COMPLETED"
		decision.Completed = true
		decision.Working = false
		if decision.Action == "SKIP" {
			decision.Action = "INJECT_CONTINUE"
		}
	}
	if decision.Reason == "" {
		decision.Reason = "(no reason provided)"
	}
	return decision
}

func parseCaptureDecision(out string) captureDecision {
	result := captureDecision{
		Action: "SKIP",
		Status: "UNKNOWN",
		Reason: "(no reason provided)",
	}

	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		upper := strings.ToUpper(trimmed)
		switch {
		case strings.HasPrefix(upper, "ACTION:"):
			result.Action = strings.TrimSpace(trimmed[len("ACTION:"):])
		case strings.HasPrefix(upper, "STATUS:"):
			result.Status = strings.TrimSpace(trimmed[len("STATUS:"):])
		case strings.HasPrefix(upper, "WORKING:"):
			result.Working = parseTruthy(strings.TrimSpace(trimmed[len("WORKING:"):]))
		case strings.HasPrefix(upper, "MULTIPLE_CHOICE:"):
			result.MultiChoice = parseTruthy(strings.TrimSpace(trimmed[len("MULTIPLE_CHOICE:"):]))
		case strings.HasPrefix(upper, "RECOMMENDED_CHOICE:"):
			value := strings.TrimSpace(trimmed[len("RECOMMENDED_CHOICE:"):])
			if value != "" && !strings.EqualFold(value, "none") {
				result.RecommendedChoice = value
			}
		case strings.HasPrefix(upper, "REASON:"):
			result.Reason = strings.TrimSpace(trimmed[len("REASON:"):])
		}
	}

	status := strings.ToUpper(strings.TrimSpace(result.Status))
	switch status {
	case "WORKING":
		result.Working = true
	case "ASKING":
		result.MultiChoice = true
	case "MULTIPLE_CHOICE":
		result.MultiChoice = true
	case "COMPLETED":
		result.Completed = true
	}
	switch strings.ToUpper(strings.TrimSpace(result.Action)) {
	case "SEND":
		result.Action = "INJECT_CONTINUE"
	case "INJECT_CONTINUE":
		result.Action = "INJECT_CONTINUE"
	case "INJECT_INPUT":
		result.Action = "INJECT_INPUT"
	case "INJECT_SELECT":
		result.Action = "INJECT_SELECT"
	case "SKIP":
		if status == "COMPLETED" || status == "WAITING" {
			result.Action = "INJECT_CONTINUE"
		} else {
			result.Action = "SKIP"
		}
	default:
		result.Action = "SKIP"
	}
	if result.MultiChoice && result.RecommendedChoice != "" && !result.Working {
		result.Action = "INJECT_SELECT"
	}
	if result.Status == "WORKING" {
		result.Working = true
	}
	if result.Working {
		result.Action = "SKIP"
	}

	if result.Reason == "(no reason provided)" && strings.TrimSpace(out) != "" {
		result.Reason = strings.Join(strings.Fields(out), " ")
	}
	return result
}

func normalizeChoiceCandidate(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	value = strings.Fields(value)[0]
	value = strings.TrimSuffix(strings.TrimSuffix(value, "."), ")")
	if isNumericChoice(value) {
		return value
	}

	numPrefix := 0
	for numPrefix < len(value) {
		ch := value[numPrefix]
		if ch < '0' || ch > '9' {
			break
		}
		numPrefix++
	}
	if numPrefix > 0 {
		return value[:numPrefix]
	}
	return value
}

func parseChoiceCandidateAndLabel(raw string) (string, string) {
	choice := normalizeChoiceCandidate(strings.TrimSpace(raw))
	if choice == "" || !isNumericChoice(choice) {
		return "", ""
	}
	label := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), strings.TrimSpace(choice)))
	label = strings.TrimLeft(label, ").:-")
	label = strings.TrimSpace(label)
	return choice, label
}

func parseTruthy(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

func isNumericChoice(raw string) bool {
	value := strings.TrimSpace(raw)
	if value == "" || strings.EqualFold(value, "none") {
		return false
	}
	value = strings.TrimSuffix(value, ".")
	value = strings.TrimSuffix(value, ")")
	_, err := strconv.Atoi(value)
	return err == nil
}

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

func normalizeCaptureForWorkingCheck(capture string) string {
	capture = ansiEscapePattern.ReplaceAllString(capture, "")
	return strings.TrimRight(strings.ReplaceAll(capture, "\r\n", "\n"), "\n")
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

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSICodesAll(value string) string {
	return ansiEscapePattern.ReplaceAllString(value, "")
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
