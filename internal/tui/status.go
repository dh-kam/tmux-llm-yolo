package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dh-kam/tmux-llm-yolo/internal/buildinfo"
	"github.com/mattn/go-isatty"
)

type Snapshot struct {
	Target      string
	State       string
	Mode        string
	Capture     string
	WaitPlan    string
	Continue    string
	Policy      string
	CurrentTask string
	CurrentDesc string
	NextTask    string
	NextDesc    string
	LLMPrimary  string
	LLMFallback string
	LLMActive   string
	SleepReason string
	SleepStart  time.Time
	SleepUntil  time.Time
	Deadline    time.Time
	LastEvent   string
	LastUpdated time.Time
	LogLines    []string
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
	width    int
	height   int
}

func Start(ctx context.Context) *UI {
	if !isatty.IsTerminal(os.Stderr.Fd()) && !isatty.IsCygwinTerminal(os.Stderr.Fd()) {
		return nil
	}
	m := model{now: time.Now()}
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr), tea.WithInput(nil), tea.WithAltScreen())
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
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
		return m, nil
	case stopMsg:
		return m, tea.Quit
	default:
		return m, nil
	}
}

func (m model) View() string {
	width := m.width
	height := m.height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}

	header := renderHeaderBlock(m.snapshot, m.now, width)
	cards := buildCardsForViewport(m.snapshot, m.now, width, height)
	grid := renderCardGrid(cards, width)
	usedHeight := lipgloss.Height(header)
	if grid != "" {
		usedHeight += 1 + lipgloss.Height(grid)
	}
	logHeight := height - usedHeight
	updatedText := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("updated " + lastUpdatedText(m.snapshot, m.now))
	logPanel := renderLogPanel(m.snapshot.LogLines, width, logHeight, updatedText)

	sections := []string{header}
	if grid != "" {
		sections = append(sections, grid)
	}
	if logPanel != "" {
		sections = append(sections, logPanel)
	}

	return fitViewHeight(strings.Join(sections, "\n"), width, height)
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

func visibleLogLines(lines []string, limit int) []string {
	if limit <= 0 || len(lines) == 0 {
		return nil
	}
	if len(lines) <= limit {
		return append([]string(nil), lines...)
	}
	return append([]string(nil), lines[len(lines)-limit:]...)
}

func truncatePlain(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

type cardSpec struct {
	Title string
	Tone  lipgloss.Color
	Items []cardItem
	Full  bool
}

type cardItem struct {
	Label    string
	Value    string
	Styled   string
	Emphasis bool
	Wrap     bool
}

const cardLabelWidth = 7

func buildCardsForViewport(snapshot Snapshot, now time.Time, width int, height int) []cardSpec {
	if width < 52 || height < 20 {
		return []cardSpec{
			{
				Title: "Session",
				Tone:  lipgloss.Color("37"),
				Items: []cardItem{
					{Label: "mode", Value: zeroDash(snapshot.Mode)},
				},
			},
			{
				Title: "Flow",
				Tone:  lipgloss.Color("80"),
				Items: []cardItem{
					{Label: "state", Value: compactState(snapshot.State)},
					{Label: "now", Value: compactTask(snapshot.CurrentTask)},
					{Label: "queued", Value: zeroDash(snapshot.NextTask)},
				},
			},
			{
				Title: "Control",
				Tone:  lipgloss.Color("214"),
				Items: []cardItem{
					{Label: "policy", Value: zeroDash(snapshot.Policy)},
					{Label: "expire", Value: deadlineRemaining(snapshot, now)},
					{Label: "llm", Value: compactLLM(snapshot)},
				},
			},
		}
	}

	return []cardSpec{
		{
			Title: "Session",
			Tone:  lipgloss.Color("37"),
			Items: []cardItem{
				{Label: "mode", Value: zeroDash(snapshot.Mode)},
				{Label: "capture", Value: zeroDash(snapshot.Capture)},
			},
		},
		{
			Title: "Flow",
			Tone:  lipgloss.Color("80"),
			Items: []cardItem{
				{Label: "state", Value: compactState(snapshot.State)},
				{Label: "now", Value: compactTask(snapshot.CurrentTask)},
				{Label: "next", Value: compactTask(snapshot.NextTask)},
			},
		},
		{
			Title: "Timing",
			Tone:  lipgloss.Color("214"),
			Items: []cardItem{
				{Label: "wait", Value: zeroDash(snapshot.WaitPlan)},
				{Label: "sleep", Value: sleepStatus(snapshot, now)},
				{Label: "expire", Value: deadlineRemaining(snapshot, now)},
			},
		},
		{
			Title: "AI",
			Tone:  lipgloss.Color("75"),
			Items: []cardItem{
				{Label: "policy", Value: zeroDash(snapshot.Policy)},
				{Label: "llm", Value: compactLLM(snapshot)},
				{Label: "cont", Value: zeroDash(snapshot.Continue)},
			},
		},
		{
			Title: "Detail",
			Tone:  lipgloss.Color("111"),
			Full:  true,
			Items: []cardItem{
				{Label: "doing", Value: zeroDash(snapshot.CurrentDesc), Wrap: true},
				{Label: "next", Value: zeroDash(snapshot.NextDesc), Wrap: true},
				{Label: "event", Value: zeroDash(snapshot.LastEvent), Emphasis: true, Wrap: true},
			},
		},
	}
}

func renderHeaderBlock(snapshot Snapshot, now time.Time, width int) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230"))
	versionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("151"))

	title := buildinfo.AppName
	if version := strings.TrimSpace(buildinfo.Version); version != "" {
		title = titleStyle.Render(title) + " " + versionStyle.Render(version)
	} else {
		title = titleStyle.Render(title)
	}

	targetPill := renderTargetPill(snapshot.Target)
	statePill := renderStatePill(snapshot.State)
	waitText := renderWaitProgressBar(snapshot.State)

	innerWidth := maxInt(1, width)
	topLeft := title + " " + targetPill

	// Try to fit state and wait on the same line as title
	topRight := statePill
	if waitWidth := lipgloss.Width(waitText); waitWidth > 0 {
		if topRight != "" {
			topRight = topRight + " " + waitText
		} else {
			topRight = waitText
		}
	}

	topLine := topLeft
	if lipgloss.Width(topRight) > 0 && innerWidth > lipgloss.Width(topLeft)+2+lipgloss.Width(topRight) {
		topLine = joinWithSpacer(topLeft, topRight, innerWidth)
	} else {
		topLine = topLeft
	}

	lines := []string{topLine}
	// If state/wait didn't fit on first line, put them on second line
	if lipgloss.Width(topLine) == lipgloss.Width(topLeft) && topRight != "" {
		lines = append(lines, topRight)
	}
	return strings.Join(lines, "\n")
}

func renderStatePill(state string) string {
	state = zeroDash(state)
	style := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("16")).
		Background(lipgloss.Color("151")).
		Padding(0, 1)
	switch state {
	case "monitoring":
		style = style.Background(lipgloss.Color("109"))
	case "suspect_waiting_stage_1", "suspect_waiting_stage_2", "suspect_waiting_stage_3":
		style = style.Background(lipgloss.Color("221"))
	case "confident_waiting", "interpreting":
		style = style.Background(lipgloss.Color("80"))
	case "acting":
		style = style.Background(lipgloss.Color("178"))
	case "completed":
		style = style.Background(lipgloss.Color("107"))
	case "stopped":
		style = style.Background(lipgloss.Color("246"))
	}
	return style.Render(strings.ToUpper(state))
}

func renderTargetPill(target string) string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("61")).
		Padding(0, 1).
		Render(zeroDash(target))
}

func renderCardGrid(cards []cardSpec, width int) string {
	if len(cards) == 0 || width <= 0 {
		return ""
	}
	const gap = 1
	const minCardWidth = 26

	cols := gridColumnCount(width, len(cards), minCardWidth, gap)
	showTitles := cols > 1

	rows := make([]string, 0, len(cards))
	pending := make([]cardSpec, 0, cols)
	flushRow := func(rowSpecs []cardSpec) {
		if len(rowSpecs) == 0 {
			return
		}
		cardWidth := (width - gap*(len(rowSpecs)-1)) / len(rowSpecs)
		if cardWidth <= 0 {
			cardWidth = width
		}
		contentHeights := make([]int, len(rowSpecs))
		maxContentHeight := 0
		for i, spec := range rowSpecs {
			widthForCard := cardWidth
			// For the last card in a 2-column row, use remaining space
			if len(rowSpecs) == 2 && i == 1 {
				widthForCard = width - gap - cardWidth
			}
			contentHeights[i] = cardContentHeight(spec, widthForCard, showTitles)
			if contentHeights[i] > maxContentHeight {
				maxContentHeight = contentHeights[i]
			}
		}
		row := make([]string, 0, len(rowSpecs)*2)
		for i, spec := range rowSpecs {
			if i > 0 {
				row = append(row, strings.Repeat(" ", gap))
			}
			widthForCard := cardWidth
			// For the last card in a 2-column row, use remaining space
			if len(rowSpecs) == 2 && i == 1 {
				widthForCard = width - gap - cardWidth
			}
			row = append(row, renderCard(spec, widthForCard, maxContentHeight, showTitles))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, row...))
	}

	for _, spec := range cards {
		if spec.Full {
			flushRow(pending)
			pending = pending[:0]
			rows = append(rows, renderCard(spec, width, cardContentHeight(spec, width, showTitles), showTitles))
			continue
		}
		pending = append(pending, spec)
		if len(pending) >= cols {
			flushRow(pending)
			pending = pending[:0]
		}
	}
	flushRow(pending)
	return strings.Join(rows, "\n")
}

func gridColumnCount(width int, cardCount int, minCardWidth int, gap int) int {
	if cardCount <= 1 || width <= minCardWidth {
		return 1
	}
	maxCols := cardCount
	if maxCols > 2 {
		maxCols = 2
	}
	for cols := maxCols; cols >= 1; cols-- {
		usable := width - gap*(cols-1)
		if usable <= 0 {
			continue
		}
		if usable/cols >= minCardWidth {
			return cols
		}
	}
	return 1
}

func renderCard(spec cardSpec, width int, contentHeight int, showTitle bool) string {
	innerWidth := maxInt(1, width-2)
	cardStyle := lipgloss.NewStyle().
		Width(width).
		Background(lipgloss.Color("236")).
		Padding(0, 1)
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("16")).
		Background(spec.Tone).
		Padding(0, 1)

	lines := make([]string, 0, len(spec.Items)+1)
	if showTitle {
		lines = append(lines, titleStyle.Render(spec.Title))
	}

	if !spec.Full {
		const colGap = 1
		colWidth := (innerWidth - colGap) / 2
		// Style for padding and gap - resets to card background
		cardBgStyle := lipgloss.NewStyle().Background(lipgloss.Color("236"))
		for i := 0; i < len(spec.Items); i += 2 {
			leftLines := renderCardItemLines(spec.Items[i], colWidth)
			var rightLines []string
			if i+1 < len(spec.Items) {
				rightLines = renderCardItemLines(spec.Items[i+1], colWidth)
			}
			maxRows := maxInt(len(leftLines), len(rightLines))
			for row := 0; row < maxRows; row++ {
				leftCell := ""
				if row < len(leftLines) {
					leftCell = leftLines[row]
				} else {
					leftCell = strings.Repeat(" ", colWidth)
				}
				// Pad leftCell to colWidth with card background style
				leftWidth := lipgloss.Width(leftCell)
				if leftWidth < colWidth {
					leftCell += cardBgStyle.Render(strings.Repeat(" ", colWidth-leftWidth))
				}
				rightCell := ""
				if row < len(rightLines) {
					rightCell = rightLines[row]
				} else if len(rightLines) > 0 {
					rightCell = strings.Repeat(" ", colWidth)
				}
				// Pad rightCell to colWidth with card background style
				rightWidth := lipgloss.Width(rightCell)
				if rightWidth < colWidth {
					rightCell += cardBgStyle.Render(strings.Repeat(" ", colWidth-rightWidth))
				}
				if rightCell != "" {
					gapStr := cardBgStyle.Render(strings.Repeat(" ", colGap))
					lines = append(lines, leftCell+gapStr+rightCell)
				} else {
					lines = append(lines, leftCell)
				}
			}
		}
	} else {
		for _, item := range spec.Items {
			lines = append(lines, renderCardItemLines(item, innerWidth)...)
		}
	}

	content := strings.Join(lines, "\n")
	return cardStyle.Height(contentHeight).Render(content)
}

func cardContentHeight(spec cardSpec, width int, showTitle bool) int {
	innerWidth := maxInt(1, width-2)
	lines := 0
	if showTitle {
		lines = 1
	}

	if !spec.Full {
		const colGap = 1
		colWidth := (innerWidth - colGap) / 2
		for i := 0; i < len(spec.Items); i += 2 {
			leftLines := len(renderCardItemLines(spec.Items[i], colWidth))
			rightLines := 0
			if i+1 < len(spec.Items) {
				rightLines = len(renderCardItemLines(spec.Items[i+1], colWidth))
			}
			lines += maxInt(leftLines, rightLines)
		}
	} else {
		for _, item := range spec.Items {
			lines += len(renderCardItemLines(item, innerWidth))
		}
	}
	return lines
}

func renderCardItemLines(item cardItem, width int) []string {
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	emphasisStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("221"))

	labelCell := paddedCardLabel(item.Label)

	if item.Styled != "" {
		return []string{
			labelStyle.Render(labelCell) + item.Styled,
		}
	}

	valueWidth := maxInt(1, width-len([]rune(labelCell)))
	var wrapped []string
	if !item.Wrap || width < 24 {
		wrapped = []string{truncatePlain(zeroDash(item.Value), valueWidth)}
	} else {
		wrapped = wrapText(zeroDash(item.Value), valueWidth)
	}
	if len(wrapped) == 0 {
		wrapped = []string{"-"}
	}
	lines := make([]string, 0, len(wrapped))
	for i, line := range wrapped {
		prefix := labelCell
		if i > 0 {
			prefix = strings.Repeat(" ", len([]rune(labelCell)))
		}
		style := valueStyle
		if item.Emphasis {
			style = emphasisStyle
		}
		lines = append(lines, labelStyle.Render(prefix)+style.Render(line))
	}
	return lines
}

func compactState(state string) string {
	switch strings.TrimSpace(state) {
	case "monitoring":
		return "monitor"
	case "suspect_waiting_stage_1":
		return "wait-1"
	case "suspect_waiting_stage_2":
		return "wait-2"
	case "suspect_waiting_stage_3":
		return "wait-3"
	case "confident_waiting":
		return "waiting"
	case "interpreting":
		return "interpret"
	case "acting":
		return "acting"
	case "completed":
		return "done"
	case "stopped":
		return "stopped"
	default:
		return zeroDash(state)
	}
}

func compactTask(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "-"
	}
	name = strings.TrimSuffix(name, "Task")
	switch name {
	case "Capture":
		return "capture"
	case "CompareCapture":
		return "compare"
	case "InterpretWaitingState":
		return "interpret"
	case "CheckDeadline":
		return "deadline"
	case "Sleep":
		return "sleep"
	default:
		return strings.ToLower(name)
	}
}

func compactLLM(snapshot Snapshot) string {
	parts := []string{}
	if primary := strings.TrimSpace(snapshot.LLMPrimary); primary != "" {
		parts = append(parts, primary)
	}
	if fallback := strings.TrimSpace(snapshot.LLMFallback); fallback != "" {
		parts = append(parts, "+"+fallback)
	}
	if active := strings.TrimSpace(snapshot.LLMActive); active != "" {
		parts = append(parts, "@"+active)
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

func paddedCardLabel(label string) string {
	label = strings.ToUpper(strings.TrimSpace(label))
	label = truncatePlain(label, cardLabelWidth)
	labelWidth := len([]rune(label))
	if labelWidth < cardLabelWidth {
		label += strings.Repeat(" ", cardLabelWidth-labelWidth)
	}
	return label + " "
}

func renderLogPanel(logLines []string, width int, height int, updatedText string) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if height < 3 {
		if updatedText != "" {
			return fitViewHeight(joinWithSpacer("Activity Log", updatedText, width), width, height)
		}
		return fitViewHeight(lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Render("Activity Log"), width, height)
	}
	innerWidth := maxInt(1, width-2)
	panelStyle := lipgloss.NewStyle().
		Width(width).
		Background(lipgloss.Color("235")).
		Padding(0, 1)
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("16")).
		Background(lipgloss.Color("246")).
		Padding(0, 1)
	logStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("248"))
	innerHeight := maxInt(1, height)

	titleLine := joinWithSpacer(titleStyle.Render("Activity Log"), updatedText, innerWidth)
	contentLines := []string{titleLine}
	logSlots := innerHeight - 1
	if logSlots < 0 {
		logSlots = 0
	}
	for _, line := range visibleLogLines(logLines, logSlots) {
		contentLines = append(contentLines, logStyle.Render(truncatePlain(line, innerWidth)))
	}
	for len(contentLines) < innerHeight {
		contentLines = append(contentLines, "")
	}
	if len(contentLines) > innerHeight {
		contentLines = contentLines[:innerHeight]
	}
	return panelStyle.Height(innerHeight).Render(strings.Join(contentLines, "\n"))
}

func sleepStatus(snapshot Snapshot, now time.Time) string {
	if snapshot.SleepUntil.IsZero() {
		return "-"
	}
	remaining := time.Until(snapshot.SleepUntil).Round(time.Second)
	if remaining < 0 {
		remaining = 0
	}
	if snapshot.SleepStart.IsZero() {
		return remaining.String()
	}
	slept := now.Sub(snapshot.SleepStart).Round(time.Second)
	if slept < 0 {
		slept = 0
	}
	total := snapshot.SleepUntil.Sub(snapshot.SleepStart).Round(time.Second)
	if total < 0 {
		total = 0
	}
	return fmt.Sprintf("%s / %s (%s left)", slept, total, remaining)
}

func deadlineRemaining(snapshot Snapshot, _ time.Time) string {
	if snapshot.Deadline.IsZero() {
		return "-"
	}
	remaining := time.Until(snapshot.Deadline).Round(time.Second)
	if remaining < 0 {
		remaining = 0
	}
	return remaining.String()
}

func lastUpdatedText(snapshot Snapshot, now time.Time) string {
	if snapshot.LastUpdated.IsZero() {
		return now.Format("15:04:05")
	}
	ago := now.Sub(snapshot.LastUpdated).Round(time.Second)
	if ago < 0 {
		ago = 0
	}
	if ago <= time.Second {
		return snapshot.LastUpdated.Format("15:04:05") + " just now"
	}
	return fmt.Sprintf("%s (%s ago)", snapshot.LastUpdated.Format("15:04:05"), ago)
}

func joinWithSpacer(left string, right string, width int) string {
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	if leftWidth+rightWidth >= width {
		return left + "\n" + right
	}
	return left + strings.Repeat(" ", width-leftWidth-rightWidth) + right
}

func fitViewHeight(view string, width int, height int) string {
	lines := strings.Split(view, "\n")
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	if height > 0 {
		for len(lines) < height {
			lines = append(lines, strings.Repeat(" ", maxInt(0, width)))
		}
	}
	return strings.Join(lines, "\n")
}

func wrapText(value string, width int) []string {
	value = strings.TrimSpace(value)
	if width <= 0 {
		return []string{""}
	}
	if value == "" {
		return []string{""}
	}

	words := strings.Fields(value)
	if len(words) == 0 {
		return []string{""}
	}

	lines := make([]string, 0, 4)
	current := words[0]
	for _, word := range words[1:] {
		candidate := current + " " + word
		if len([]rune(candidate)) <= width {
			current = candidate
			continue
		}
		if len([]rune(current)) > width {
			lines = append(lines, breakWord(current, width)...)
		} else {
			lines = append(lines, current)
		}
		current = word
	}
	if len([]rune(current)) > width {
		lines = append(lines, breakWord(current, width)...)
	} else {
		lines = append(lines, current)
	}
	return lines
}

func breakWord(value string, width int) []string {
	runes := []rune(value)
	if width <= 0 {
		return []string{""}
	}
	lines := make([]string, 0, (len(runes)+width-1)/width)
	for len(runes) > 0 {
		end := width
		if end > len(runes) {
			end = len(runes)
		}
		lines = append(lines, string(runes[:end]))
		runes = runes[end:]
	}
	return lines
}

func renderWaitProgressBar(state string) string {
	segments := []struct {
		label string
		state string
	}{
		{label: "MON", state: "monitoring"},
		{label: "S1", state: "suspect_waiting_stage_1"},
		{label: "S2", state: "suspect_waiting_stage_2"},
		{label: "S3", state: "suspect_waiting_stage_3"},
		{label: "WAIT", state: "confident_waiting"},
	}
	activeIndex := waitProgressIndex(state)
	doneStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("34")).Padding(0, 1)
	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")).Background(lipgloss.Color("39")).Padding(0, 1)
	pendingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Background(lipgloss.Color("236")).Padding(0, 1)

	parts := make([]string, 0, len(segments))
	for i, segment := range segments {
		style := pendingStyle
		switch {
		case activeIndex >= 0 && i < activeIndex:
			style = doneStyle
		case activeIndex == i:
			style = activeStyle
		}
		parts = append(parts, style.Render(segment.label))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func waitProgressIndex(state string) int {
	switch state {
	case "monitoring":
		return 0
	case "suspect_waiting_stage_1":
		return 1
	case "suspect_waiting_stage_2":
		return 2
	case "suspect_waiting_stage_3":
		return 3
	case "confident_waiting", "interpreting", "acting", "completed", "stopped":
		return 4
	default:
		return -1
	}
}
