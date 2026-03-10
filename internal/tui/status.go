package tui

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

type Snapshot struct {
	State       string
	Scope       string
	Policy      string
	CurrentTask string
	CurrentDesc string
	NextTask    string
	LLMStatus   string
	SleepReason string
	SleepUntil  time.Time
	Deadline    time.Time
	LastEvent   string
	LastUpdated time.Time
}

type UI struct {
	program *tea.Program
}

type snapshotMsg Snapshot
type stopMsg struct{}
type tickMsg time.Time

type model struct {
	snapshot Snapshot
	now      time.Time
}

func Start(ctx context.Context) *UI {
	if !isatty.IsTerminal(os.Stderr.Fd()) && !isatty.IsCygwinTerminal(os.Stderr.Fd()) {
		return nil
	}
	m := model{now: time.Now()}
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr), tea.WithInput(nil))
	ui := &UI{program: p}
	go func() {
		_, _ = p.Run()
	}()
	go func() {
		<-ctx.Done()
		p.Send(stopMsg{})
	}()
	return ui
}

func (ui *UI) Update(snapshot Snapshot) {
	if ui == nil || ui.program == nil {
		return
	}
	if snapshot.LastUpdated.IsZero() {
		snapshot.LastUpdated = time.Now()
	}
	ui.program.Send(snapshotMsg(snapshot))
}

func (ui *UI) Stop() {
	if ui == nil || ui.program == nil {
		return
	}
	ui.program.Send(stopMsg{})
}

func (m model) Init() tea.Cmd {
	return tickCmd()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case snapshotMsg:
		m.snapshot = Snapshot(typed)
		return m, nil
	case tickMsg:
		m.now = time.Time(typed)
		return m, tickCmd()
	case stopMsg:
		return m, tea.Quit
	default:
		return m, nil
	}
}

func (m model) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).Render("r8-watcher")
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	value := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	event := lipgloss.NewStyle().Foreground(lipgloss.Color("220"))

	sleepRemaining := "-"
	if !m.snapshot.SleepUntil.IsZero() {
		remaining := time.Until(m.snapshot.SleepUntil).Round(time.Second)
		if remaining < 0 {
			remaining = 0
		}
		sleepRemaining = remaining.String()
	}

	deadlineRemaining := "-"
	if !m.snapshot.Deadline.IsZero() {
		remaining := time.Until(m.snapshot.Deadline).Round(time.Second)
		if remaining < 0 {
			remaining = 0
		}
		deadlineRemaining = remaining.String()
	}

	body := fmt.Sprintf(
		"%s %s\n%s %s\n%s %s\n%s %s\n%s %s\n%s %s\n%s %s\n%s %s\n%s %s\n%s %s",
		label.Render("state:"), value.Render(zeroDash(m.snapshot.State)),
		label.Render("scope:"), value.Render(zeroDash(m.snapshot.Scope)),
		label.Render("policy:"), value.Render(zeroDash(m.snapshot.Policy)),
		label.Render("task:"), value.Render(zeroDash(m.snapshot.CurrentTask)),
		label.Render("detail:"), value.Render(zeroDash(m.snapshot.CurrentDesc)),
		label.Render("next:"), value.Render(zeroDash(m.snapshot.NextTask)),
		label.Render("llm:"), value.Render(zeroDash(m.snapshot.LLMStatus)),
		label.Render("sleep:"), value.Render(sleepRemaining),
		label.Render("deadline:"), value.Render(deadlineRemaining),
		label.Render("event:"), event.Render(zeroDash(m.snapshot.LastEvent)),
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		Render(title+"\n"+body) + "\n"
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func zeroDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
