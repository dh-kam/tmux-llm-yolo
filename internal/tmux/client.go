package tmux

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const defaultTmuxCommand = "tmux"
const commandTimeout = 8 * time.Second

type client struct {
	command string
}

func New() (API, error) {
	return NewWithCommand(defaultTmuxCommand)
}

func NewWithCommand(command string) (API, error) {
	if strings.TrimSpace(command) == "" {
		command = defaultTmuxCommand
	}

	if _, err := exec.LookPath(command); err != nil {
		return nil, fmt.Errorf("tmux 바이너리를 찾을 수 없습니다: %s", command)
	}

	return &client{
		command: command,
	}, nil
}

func (c *client) ListSessions(ctx context.Context) ([]Session, error) {
	output, err := c.runCommand(ctx, "list-sessions", "-F", "#{session_name}")
	if err != nil {
		return nil, err
	}
	sessionNames := strings.Split(strings.TrimSpace(output), "\n")
	var sessions []Session
	for _, name := range sessionNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		sessions = append(sessions, Session{Name: name})
	}
	return sessions, nil
}

func (c *client) HasSession(ctx context.Context, session string) (bool, error) {
	err := c.runCommandIgnoreOutput(ctx, "has-session", "-t", session)
	if err == nil {
		return true, nil
	}
	if strings.Contains(err.Error(), "no session") || strings.Contains(err.Error(), "can't find session") {
		return false, nil
	}
	return false, err
}

func (c *client) CapturePane(ctx context.Context, target string, lines int, includeANSI bool) (string, error) {
	if lines <= 0 {
		lines = 200
	}
	args := []string{"capture-pane", "-p", "-J", "-t", target, "-S", fmt.Sprintf("-%d", lines)}
	if includeANSI {
		args = append(args, "-e")
	}
	return c.runCommand(ctx, args...)
}

func (c *client) SendKeys(ctx context.Context, target string, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	args := []string{"send-keys", "-t", target}
	args = append(args, keys...)
	return c.runCommandIgnoreOutput(ctx, args...)
}

func (c *client) PaneSize(ctx context.Context, target string) (int, int, error) {
	output, err := c.runCommand(ctx, "display-message", "-p", "-t", target, "#{pane_width} #{pane_height}")
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(strings.TrimSpace(output))
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("pane size output parse failed: %q", output)
	}
	width, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, err
	}
	height, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, err
	}
	return width, height, nil
}

func (c *client) IsPaneInMode(ctx context.Context, target string) (bool, error) {
	output, err := c.runCommand(ctx, "display-message", "-p", "-t", target, "#{pane_in_mode}")
	if err != nil {
		return false, err
	}
	value := strings.TrimSpace(output)
	return value == "1", nil
}

func (c *client) CreateSession(ctx context.Context, name string) error {
	return c.runCommandIgnoreOutput(ctx, "new-session", "-d", "-s", name)
}

func (c *client) AttachSession(ctx context.Context, name string) error {
	return c.runCommandIgnoreOutput(ctx, "attach-session", "-t", name)
}

func (c *client) KillSession(ctx context.Context, name string) error {
	return c.runCommandIgnoreOutput(ctx, "kill-session", "-t", name)
}

func (c *client) runCommand(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.command, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	var errOut bytes.Buffer
	cmd.Stderr = &errOut

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(errOut.String()))
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

func (c *client) runCommandIgnoreOutput(ctx context.Context, args ...string) error {
	_, err := c.runCommand(ctx, args...)
	return err
}
