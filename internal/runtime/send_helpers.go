package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dh-kam/tmux-llm-yolo/internal/tmux"
)

func SendContinueMessage(
	ctx context.Context,
	client tmux.API,
	target string,
	message string,
	submitKey string,
	fallbackSubmitKey string,
	fallbackDelay float64,
	clearBeforeTyping bool,
) error {
	if inMode, err := client.IsPaneInMode(ctx, target); err == nil && inMode {
		if err := client.SendKeys(ctx, target, "-X", "cancel"); err != nil {
			return err
		}
	}
	if clearBeforeTyping {
		if err := client.SendKeys(ctx, target, "C-u"); err != nil {
			return err
		}
	}
	if err := sendLiteralText(ctx, client, target, message); err != nil {
		return err
	}
	if err := client.SendKeys(ctx, target, submitKey); err != nil {
		return err
	}
	if fallbackSubmitKey != "" {
		if err := waitForFallbackDelay(ctx, fallbackDelay); err != nil {
			return err
		}
		if err := client.SendKeys(ctx, target, fallbackSubmitKey); err != nil {
			return err
		}
	}
	return nil
}

func SendChoiceMessage(
	ctx context.Context,
	client tmux.API,
	target string,
	choice string,
	submitKey string,
	fallbackSubmitKey string,
	fallbackDelay float64,
	clearBeforeTyping bool,
) error {
	if inMode, err := client.IsPaneInMode(ctx, target); err == nil && inMode {
		if err := client.SendKeys(ctx, target, "-X", "cancel"); err != nil {
			return err
		}
	}
	if strings.EqualFold(choice, "Enter") {
		key := submitKey
		if strings.TrimSpace(key) == "" {
			key = "C-m"
		}
		if err := client.SendKeys(ctx, target, key); err != nil {
			return err
		}
		return nil
	}
	if _, err := strconv.Atoi(strings.TrimSpace(choice)); err != nil {
		return fmt.Errorf("invalid choice: %s", choice)
	}
	if clearBeforeTyping {
		if err := client.SendKeys(ctx, target, "C-u"); err != nil {
			return err
		}
	}
	if err := sendLiteralText(ctx, client, target, choice); err != nil {
		return err
	}
	if err := client.SendKeys(ctx, target, submitKey); err != nil {
		return err
	}
	if fallbackSubmitKey != "" {
		if err := waitForFallbackDelay(ctx, fallbackDelay); err != nil {
			return err
		}
		if err := client.SendKeys(ctx, target, fallbackSubmitKey); err != nil {
			return err
		}
	}
	return nil
}

func SendInputMessage(
	ctx context.Context,
	client tmux.API,
	target string,
	input string,
	submitKey string,
	fallbackSubmitKey string,
	fallbackDelay float64,
	clearBeforeTyping bool,
) error {
	input = strings.TrimSpace(input)
	if input == "" {
		return fmt.Errorf("empty input")
	}
	if inMode, err := client.IsPaneInMode(ctx, target); err == nil && inMode {
		if err := client.SendKeys(ctx, target, "-X", "cancel"); err != nil {
			return err
		}
	}
	if clearBeforeTyping {
		if err := client.SendKeys(ctx, target, "C-u"); err != nil {
			return err
		}
	}
	if err := sendLiteralText(ctx, client, target, input); err != nil {
		return err
	}
	if err := client.SendKeys(ctx, target, submitKey); err != nil {
		return err
	}
	if fallbackSubmitKey != "" {
		if err := waitForFallbackDelay(ctx, fallbackDelay); err != nil {
			return err
		}
		if err := client.SendKeys(ctx, target, fallbackSubmitKey); err != nil {
			return err
		}
	}
	return nil
}

func SendSubmitOnly(
	ctx context.Context,
	client tmux.API,
	target string,
	submitKey string,
	fallbackSubmitKey string,
	fallbackDelay float64,
) error {
	if inMode, err := client.IsPaneInMode(ctx, target); err == nil && inMode {
		if err := client.SendKeys(ctx, target, "-X", "cancel"); err != nil {
			return err
		}
	}
	key := strings.TrimSpace(submitKey)
	if key == "" {
		key = "C-m"
	}
	if err := client.SendKeys(ctx, target, key); err != nil {
		return err
	}
	if strings.TrimSpace(fallbackSubmitKey) != "" {
		if err := waitForFallbackDelay(ctx, fallbackDelay); err != nil {
			return err
		}
		if err := client.SendKeys(ctx, target, fallbackSubmitKey); err != nil {
			return err
		}
	}
	return nil
}

func ClearPromptState(
	ctx context.Context,
	client tmux.API,
	target string,
) error {
	if inMode, err := client.IsPaneInMode(ctx, target); err == nil && inMode {
		if err := client.SendKeys(ctx, target, "-X", "cancel"); err != nil {
			return err
		}
	}
	if err := client.SendKeys(ctx, target, "Escape"); err != nil {
		return err
	}
	if err := waitForDuration(ctx, 120*time.Millisecond); err != nil {
		return err
	}
	if err := client.SendKeys(ctx, target, "C-u"); err != nil {
		return err
	}
	return nil
}

func waitForFallbackDelay(ctx context.Context, fallbackDelay float64) error {
	if fallbackDelay <= 0 {
		return nil
	}
	return waitForDuration(ctx, time.Duration(fallbackDelay*float64(time.Second)))
}

func waitForDuration(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func sendLiteralText(ctx context.Context, client tmux.API, target string, value string) error {
	// Use `--` so a user/LLM message starting with '-' is never parsed as a tmux option.
	return client.SendKeys(ctx, target, "-l", "--", value)
}
