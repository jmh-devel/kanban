package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jmh-devel/kanban/internal/agent"
	"github.com/jmh-devel/kanban/internal/app"
	"github.com/jmh-devel/kanban/internal/github"
	"github.com/jmh-devel/kanban/internal/state"
)

const (
	laneBacklog    = "Backlog"
	laneInProgress = "In Progress"
	laneReview     = "Review"
	laneDone       = "Done"
)

var laneNames = []string{laneBacklog, laneInProgress, laneReview, laneDone}

type Loader func(context.Context) (github.Board, error)
type Mover func(context.Context, github.Issue, string) error

type Options struct {
	Loader Loader
	Mover  Mover
	Stdout *os.File
	Stderr *os.File
}

func Run(ctx context.Context, options Options) error {
	if options.Stdout == nil {
		options.Stdout = os.Stdout
	}
	if options.Stderr == nil {
		options.Stderr = os.Stderr
	}
	if options.Loader == nil {
		return fmt.Errorf("tui loader is required")
	}

	board, err := options.Loader(ctx)
	if err != nil {
		return err
	}

	model := newModel(board, options.Loader, options.Mover)
	program := tea.NewProgram(model, tea.WithOutput(options.Stdout), tea.WithAltScreen())
	_, err = program.Run()
	return err
}

type model struct {
	board       github.Board
	columns     []column
	loader      Loader
	mover       Mover
	dispatcher  agent.Dispatcher
	config      state.Config
	configErr   error
	width       int
	height      int
	focusColumn int
	selected    []int
	mode        mode
	moveIndex   int
	dispatch    dispatchState
	expanded    viewport.Model
	status      string
	statusUntil time.Time
	refreshing  bool
	lastRefresh time.Time
	plain       bool
}

type mode int

const (
	modeBoard mode = iota
	modeExpand
	modeMove
	modeDispatch
	modeHelp
)

type column struct {
	title  string
	issues []github.Issue
}

type dispatchState struct {
	runners          []string
	modes            []string
	runnerIndex      int
	modeIndex        int
	confirmDuplicate bool
	pending          bool
	duplicate        *agent.Duplicate
}

type refreshMsg struct {
	board github.Board
	err   error
}

type moveMsg struct {
	issue github.Issue
	lane  string
	err   error
}

type dispatchMsg struct {
	result agent.Result
	err    error
}

type tickMsg time.Time

func newModel(board github.Board, loader Loader, mover Mover) model {
	columns := buildColumns(board)
	config, configErr := state.LoadConfig()
	return model{
		board:       board,
		columns:     columns,
		loader:      loader,
		mover:       mover,
		dispatcher:  agent.NewDispatcher(),
		config:      config,
		configErr:   configErr,
		width:       120,
		height:      32,
		selected:    make([]int, len(columns)),
		lastRefresh: board.UpdatedAt,
		plain:       noColor(),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Tick(refreshTTL(), func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.mode == modeExpand {
			m.expanded.Width = max(20, msg.Width-8)
			m.expanded.Height = max(6, msg.Height-8)
		}
		return m, nil
	case tickMsg:
		if m.refreshing {
			return m, tea.Tick(refreshTTL(), func(t time.Time) tea.Msg { return tickMsg(t) })
		}
		m.refreshing = true
		return m, tea.Batch(m.loadBoard(), tea.Tick(refreshTTL(), func(t time.Time) tea.Msg { return tickMsg(t) }))
	case refreshMsg:
		m.refreshing = false
		if msg.err != nil {
			m.flash("refresh failed: " + msg.err.Error())
			return m, nil
		}
		m.board = msg.board
		m.columns = buildColumns(msg.board)
		m.lastRefresh = msg.board.UpdatedAt
		m.clampSelection()
		m.flash("refreshed")
		return m, nil
	case moveMsg:
		if msg.err != nil {
			m.flash("move failed: " + msg.err.Error())
			return m, nil
		}
		m.board = moveIssueLocal(m.board, msg.issue, msg.lane)
		m.columns = buildColumns(m.board)
		m.clampSelection()
		m.mode = modeBoard
		m.flash(fmt.Sprintf("moved #%d to %s", msg.issue.Number, msg.lane))
		return m, nil
	case dispatchMsg:
		m.dispatch.pending = false
		if msg.err != nil {
			m.flash("dispatch failed: " + msg.err.Error())
			return m, nil
		}
		if msg.result.Duplicate != nil {
			m.dispatch.confirmDuplicate = true
			m.dispatch.duplicate = msg.result.Duplicate
			m.flash("already dispatched; press Enter again to re-dispatch")
			return m, nil
		}
		m.mode = modeBoard
		m.dispatch.confirmDuplicate = false
		m.dispatch.duplicate = nil
		if msg.result.Manual {
			m.flash("manual dispatch recorded")
		} else {
			m.flash("dispatched")
		}
		m.refreshing = true
		return m, m.loadBoard()
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch m.mode {
	case modeExpand:
		switch key {
		case "esc", "q":
			m.mode = modeBoard
			return m, nil
		default:
			var cmd tea.Cmd
			m.expanded, cmd = m.expanded.Update(msg)
			return m, cmd
		}
	case modeMove:
		switch key {
		case "esc", "q":
			m.mode = modeBoard
		case "tab", "right", "l", "j", "down":
			m.moveIndex = (m.moveIndex + 1) % len(laneNames)
		case "shift+tab", "left", "h", "k", "up":
			m.moveIndex = (m.moveIndex + len(laneNames) - 1) % len(laneNames)
		case "enter":
			issue, ok := m.currentIssue()
			if !ok {
				m.mode = modeBoard
				return m, nil
			}
			lane := laneNames[m.moveIndex]
			if m.mover == nil {
				m.board = moveIssueLocal(m.board, issue, lane)
				m.columns = buildColumns(m.board)
				m.mode = modeBoard
				m.flash(fmt.Sprintf("moved #%d to %s", issue.Number, lane))
				return m, nil
			}
			return m, m.moveIssue(issue, lane)
		}
		return m, nil
	case modeDispatch:
		switch key {
		case "esc", "q":
			m.mode = modeBoard
			m.dispatch.confirmDuplicate = false
			m.dispatch.duplicate = nil
		case "tab", "right", "l":
			m.dispatch.runnerIndex = (m.dispatch.runnerIndex + 1) % len(m.dispatchRunners())
			m.dispatch.confirmDuplicate = false
			m.dispatch.duplicate = nil
		case "shift+tab", "left", "h":
			m.dispatch.runnerIndex = (m.dispatch.runnerIndex + len(m.dispatchRunners()) - 1) % len(m.dispatchRunners())
			m.dispatch.confirmDuplicate = false
			m.dispatch.duplicate = nil
		case "j", "down":
			m.dispatch.modeIndex = (m.dispatch.modeIndex + 1) % len(m.dispatchModes())
			m.dispatch.confirmDuplicate = false
			m.dispatch.duplicate = nil
		case "k", "up":
			m.dispatch.modeIndex = (m.dispatch.modeIndex + len(m.dispatchModes()) - 1) % len(m.dispatchModes())
			m.dispatch.confirmDuplicate = false
			m.dispatch.duplicate = nil
		case "enter":
			if m.dispatch.pending {
				return m, nil
			}
			if m.configErr != nil {
				m.flash("dispatch config failed: " + m.configErr.Error())
				return m, nil
			}
			m.dispatch.pending = true
			return m, m.dispatchIssue()
		}
		return m, nil
	case modeHelp:
		if key == "esc" || key == "q" || key == "?" {
			m.mode = modeBoard
		}
		return m, nil
	}

	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.moveSelection(1)
	case "k", "up":
		m.moveSelection(-1)
	case "h", "left", "shift+tab":
		m.moveColumn(-1)
	case "l", "right", "tab":
		m.moveColumn(1)
	case "home", "g":
		m.selected[m.focusColumn] = 0
	case "end", "G":
		m.selected[m.focusColumn] = max(0, len(m.columns[m.focusColumn].issues)-1)
	case "enter":
		m.openExpand()
	case "d":
		if _, ok := m.currentIssue(); ok {
			m.prepareDispatch()
			m.mode = modeDispatch
		}
	case "m":
		if _, ok := m.currentIssue(); ok {
			m.moveIndex = m.focusColumn
			m.mode = modeMove
		}
	case "o":
		if issue, ok := m.currentIssue(); ok && issue.URL != "" {
			m.flash("opening #" + strconv.Itoa(issue.Number))
			return m, openBrowser(issue.URL)
		}
	case "c":
		if issue, ok := m.currentIssue(); ok {
			m.flash("issue #" + strconv.Itoa(issue.Number))
		}
	case "r":
		if !m.refreshing {
			m.refreshing = true
			return m, m.loadBoard()
		}
	case "?":
		m.mode = modeHelp
	}
	return m, nil
}

func (m model) View() string {
	if m.width > 0 && m.width < 80 {
		if m.mode == modeExpand {
			return m.renderNarrowExpand()
		}
		return app.RenderBoardText(m.board)
	}

	base := m.renderBoard()
	switch m.mode {
	case modeExpand:
		return overlay(base, m.renderExpand())
	case modeMove:
		return overlay(base, m.renderMove())
	case modeDispatch:
		return overlay(base, m.renderDispatch())
	case modeHelp:
		return overlay(base, m.renderHelp())
	default:
		return base
	}
}

func (m model) renderBoard() string {
	width := max(80, m.width)
	height := max(16, m.height)
	header := m.headerView(width)
	status := m.statusView(width)
	bodyHeight := max(4, height-lipgloss.Height(header)-lipgloss.Height(status)-2)
	columnGap := 1
	columnWidth := max(16, (width-(len(m.columns)-1)*columnGap)/len(m.columns))
	rendered := make([]string, 0, len(m.columns))
	for i, column := range m.columns {
		rendered = append(rendered, m.columnView(i, column, columnWidth, bodyHeight))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
	if m.plain {
		return strings.TrimRight(header+"\n"+body+"\n"+status, "\n") + "\n"
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, body, status) + "\n"
}

func (m model) headerView(width int) string {
	refresh := "updated " + since(m.lastRefresh) + " ago"
	if m.refreshing {
		refresh = "refreshing"
	}
	text := fmt.Sprintf("%s  |  %s  |  %s  |  [r] refresh", m.board.Repo.Slug, currentBranch(m.board.Repo.RootPath), refresh)
	return style(m.plain, lipgloss.NewStyle().Width(width).Padding(0, 1).Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("62"))).Render(truncate(text, width-2))
}

func (m model) statusView(width int) string {
	text := "[j/k] navigate  [h/l] column  [Enter] expand  [d] dispatch  [m] move  [?] help"
	if m.status != "" && time.Now().Before(m.statusUntil) {
		text = m.status
	}
	return style(m.plain, lipgloss.NewStyle().Width(width).Align(lipgloss.Right).Foreground(lipgloss.Color("245"))).Render(truncate(text, width))
}

func (m model) columnView(index int, column column, width int, height int) string {
	title := fmt.Sprintf("%s (%d)", column.title, len(column.issues))
	header := style(m.plain, lipgloss.NewStyle().Width(width).Bold(true).Foreground(lipgloss.Color("15"))).Render(truncate(title, width))
	rule := strings.Repeat("-", width)
	if !m.plain {
		rule = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(strings.Repeat("-", width))
	}

	lines := []string{header, rule}
	for i, issue := range column.issues {
		selected := index == m.focusColumn && i == m.selected[index]
		lines = append(lines, m.cardView(issue, width, selected))
	}
	if len(column.issues) == 0 {
		lines = append(lines, style(m.plain, lipgloss.NewStyle().Foreground(lipgloss.Color("244"))).Render("(empty)"))
	}
	content := strings.Join(lines, "\n")
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(content)
}

func (m model) cardView(issue github.Issue, width int, selected bool) string {
	innerWidth := max(8, width-4)
	lines := []string{
		truncate(fmt.Sprintf("#%d %s", issue.Number, issue.Title), innerWidth),
		truncate(labelsLine(issue.Labels, 4), innerWidth),
	}
	if priority := priorityLabel(issue.Labels); priority != "" {
		lines = append(lines, truncate("> "+priority, innerWidth))
	}
	if len(issue.Assignees) > 0 {
		lines = append(lines, truncate("@"+issue.Assignees[0].Login, innerWidth))
	}

	border := lipgloss.RoundedBorder()
	if m.plain {
		border = asciiBorder()
	}
	cardStyle := lipgloss.NewStyle().Width(innerWidth).Padding(0, 1).MarginBottom(1).Border(border)
	if !m.plain {
		cardStyle = cardStyle.BorderForeground(lipgloss.Color("238"))
		if selected {
			cardStyle = cardStyle.BorderForeground(lipgloss.Color("212")).Foreground(lipgloss.Color("15"))
		}
	}
	if m.plain && selected {
		lines[0] = "> " + truncate(lines[0], max(1, innerWidth-2))
	}
	return cardStyle.Render(strings.Join(lines, "\n"))
}

func (m *model) openExpand() {
	issue, ok := m.currentIssue()
	if !ok {
		return
	}
	if m.width > 0 && m.width < 80 {
		m.mode = modeExpand
		m.flash("expanded issue #" + strconv.Itoa(issue.Number))
		return
	}
	width := max(40, m.width-8)
	height := max(8, m.height-8)
	vp := viewport.New(width, height)
	vp.SetContent(expandContent(issue))
	m.expanded = vp
	m.mode = modeExpand
}

func (m model) renderExpand() string {
	return modalStyle(m.plain, m.width).Render(m.expanded.View())
}

func (m model) renderNarrowExpand() string {
	issue, ok := m.currentIssue()
	if !ok {
		return app.RenderBoardText(m.board)
	}
	return expandContent(issue) + "\n\n[Esc/q] back\n"
}

func (m model) renderMove() string {
	lines := []string{"Move card to lane", ""}
	for i, lane := range laneNames {
		prefix := "  "
		if i == m.moveIndex {
			prefix = "> "
		}
		lines = append(lines, prefix+lane)
	}
	lines = append(lines, "", "[Enter] confirm   [Esc] cancel")
	return modalStyle(m.plain, m.width).Render(strings.Join(lines, "\n"))
}

func (m model) renderDispatch() string {
	issue, _ := m.currentIssue()
	runners := m.dispatchRunners()
	modes := m.dispatchModes()
	lines := []string{
		fmt.Sprintf("Dispatch #%d to agent", issue.Number),
		"",
		"Runner:   " + segmented(runners, m.dispatch.runnerIndex),
		"Mode:     " + segmented(modes, m.dispatch.modeIndex),
		"Repo key: " + m.repoKey(),
		"",
		"Preview:",
		"> " + m.dispatchCommand(),
		"",
	}
	if m.dispatch.duplicate != nil {
		lines = append(lines,
			fmt.Sprintf("Already dispatched to %s %s ago.", m.dispatch.duplicate.Runner, since(m.dispatch.duplicate.DispatchedAt)),
			"Press Enter again to re-dispatch.",
			"",
		)
	}
	if m.dispatch.pending {
		lines = append(lines, "Dispatching...")
	} else {
		lines = append(lines, "[Enter] confirm   [Esc] cancel")
	}
	return modalStyle(m.plain, m.width).Render(strings.Join(lines, "\n"))
}

func (m model) renderHelp() string {
	return modalStyle(m.plain, m.width).Render(strings.Join([]string{
		"Keys",
		"",
		"j/k or arrows     navigate cards",
		"h/l or arrows     change columns",
		"Tab               cycle columns",
		"Enter             expand card",
		"d                 dispatch modal",
		"m                 move lane",
		"o                 open issue in browser",
		"r                 refresh",
		"q                 quit",
	}, "\n"))
}

func (m model) currentIssue() (github.Issue, bool) {
	if len(m.columns) == 0 || m.focusColumn >= len(m.columns) {
		return github.Issue{}, false
	}
	issues := m.columns[m.focusColumn].issues
	if len(issues) == 0 {
		return github.Issue{}, false
	}
	index := clamp(m.selected[m.focusColumn], 0, len(issues)-1)
	return issues[index], true
}

func (m *model) moveSelection(delta int) {
	if len(m.columns[m.focusColumn].issues) == 0 {
		m.selected[m.focusColumn] = 0
		return
	}
	m.selected[m.focusColumn] = clamp(m.selected[m.focusColumn]+delta, 0, len(m.columns[m.focusColumn].issues)-1)
}

func (m *model) moveColumn(delta int) {
	m.focusColumn = clamp(m.focusColumn+delta, 0, len(m.columns)-1)
	m.clampSelection()
}

func (m *model) clampSelection() {
	for i := range m.columns {
		if len(m.columns[i].issues) == 0 {
			m.selected[i] = 0
			continue
		}
		m.selected[i] = clamp(m.selected[i], 0, len(m.columns[i].issues)-1)
	}
}

func (m *model) flash(message string) {
	m.status = message
	m.statusUntil = time.Now().Add(2 * time.Second)
}

func (m model) loadBoard() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		board, err := m.loader(ctx)
		return refreshMsg{board: board, err: err}
	}
}

func (m model) moveIssue(issue github.Issue, lane string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := m.mover(ctx, issue, lane)
		return moveMsg{issue: issue, lane: lane, err: err}
	}
}

func (m model) dispatchIssue() tea.Cmd {
	issue, _ := m.currentIssue()
	request := agent.Request{
		Repo:             m.board.Repo.Slug,
		Issue:            issue.Number,
		Runner:           m.selectedRunner(),
		Mode:             m.selectedMode(),
		ConfirmDuplicate: m.dispatch.confirmDuplicate,
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		result, err := m.dispatcher.Dispatch(ctx, m.config, request)
		return dispatchMsg{result: result, err: err}
	}
}

func buildColumns(board github.Board) []column {
	columns := []column{
		{title: laneBacklog},
		{title: laneInProgress},
		{title: laneReview},
		{title: laneDone, issues: append([]github.Issue(nil), board.ClosedIssues...)},
	}
	for _, section := range board.Sections {
		if index, ok := laneIndexForSection(section.Title); ok {
			columns[index].issues = append(columns[index].issues, section.Issues...)
			continue
		}
		for _, issue := range section.Issues {
			switch {
			case strings.EqualFold(issue.State, "closed") || strings.TrimSpace(issue.ClosedAt) != "":
				columns[3].issues = append(columns[3].issues, issue)
			case hasLabel(issue, "kanban:in-progress"):
				columns[1].issues = append(columns[1].issues, issue)
			case hasLabel(issue, "kanban:review"):
				columns[2].issues = append(columns[2].issues, issue)
			default:
				columns[0].issues = append(columns[0].issues, issue)
			}
		}
	}
	for i := range columns {
		columns[i].issues = dedupeIssues(columns[i].issues)
	}
	return columns
}

func laneIndexForSection(title string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(title)) {
	case "backlog":
		return 0, true
	case "in progress", "in-progress":
		return 1, true
	case "review":
		return 2, true
	case "done":
		return 3, true
	default:
		return 0, false
	}
}

func dedupeIssues(issues []github.Issue) []github.Issue {
	seen := make(map[int]struct{}, len(issues))
	deduped := make([]github.Issue, 0, len(issues))
	for _, issue := range issues {
		if _, exists := seen[issue.Number]; exists {
			continue
		}
		seen[issue.Number] = struct{}{}
		deduped = append(deduped, issue)
	}
	return deduped
}

func moveIssueLocal(board github.Board, issue github.Issue, lane string) github.Board {
	for sectionIndex := range board.Sections {
		filtered := board.Sections[sectionIndex].Issues[:0]
		for _, existing := range board.Sections[sectionIndex].Issues {
			if existing.Number != issue.Number {
				filtered = append(filtered, existing)
			}
		}
		board.Sections[sectionIndex].Issues = filtered
	}
	closed := board.ClosedIssues[:0]
	for _, existing := range board.ClosedIssues {
		if existing.Number != issue.Number {
			closed = append(closed, existing)
		}
	}
	board.ClosedIssues = closed

	issue.Labels = withoutLaneLabels(issue.Labels)
	issue.State = "OPEN"
	if lane == laneInProgress {
		issue.Labels = append(issue.Labels, github.Label{Name: "kanban:in-progress"})
	} else if lane == laneReview {
		issue.Labels = append(issue.Labels, github.Label{Name: "kanban:review"})
	} else if lane == laneDone {
		issue.State = "CLOSED"
		issue.ClosedAt = time.Now().UTC().Format(time.RFC3339)
		board.ClosedIssues = append([]github.Issue{issue}, board.ClosedIssues...)
		return board
	}

	if len(board.Sections) == 0 {
		board.Sections = []github.Section{{Title: "Unscheduled"}}
	}
	board.Sections[len(board.Sections)-1].Issues = append(board.Sections[len(board.Sections)-1].Issues, issue)
	return board
}

func expandContent(issue github.Issue) string {
	lines := []string{
		fmt.Sprintf("#%d %s", issue.Number, issue.Title),
		"",
		"Labels: " + labelsLine(issue.Labels, len(issue.Labels)),
	}
	if issue.Milestone != nil {
		lines = append(lines, "Milestone: "+issue.Milestone.Title)
	}
	if len(issue.Assignees) > 0 {
		assignees := make([]string, 0, len(issue.Assignees))
		for _, assignee := range issue.Assignees {
			assignees = append(assignees, assignee.Login)
		}
		lines = append(lines, "Assignees: "+strings.Join(assignees, ", "))
	}
	if issue.URL != "" {
		lines = append(lines, "Link: "+issue.URL)
	}
	lines = append(lines, "", "Body:")
	bodyLines := strings.Split(strings.TrimSpace(issue.Body), "\n")
	if len(bodyLines) == 1 && bodyLines[0] == "" {
		bodyLines[0] = "(no body)"
	}
	if len(bodyLines) > 40 {
		bodyLines = bodyLines[:40]
	}
	lines = append(lines, bodyLines...)
	return strings.Join(lines, "\n")
}

func labelsLine(labels []github.Label, limit int) string {
	if len(labels) == 0 {
		return ""
	}
	parts := make([]string, 0, min(len(labels), limit)+1)
	for i, label := range labels {
		if i >= limit {
			parts = append(parts, fmt.Sprintf("+%d", len(labels)-limit))
			break
		}
		parts = append(parts, "["+label.Name+"]")
	}
	return strings.Join(parts, " ")
}

func priorityLabel(labels []github.Label) string {
	for _, label := range labels {
		if strings.HasPrefix(label.Name, "priority:") {
			return label.Name
		}
	}
	return ""
}

func hasLabel(issue github.Issue, name string) bool {
	for _, label := range issue.Labels {
		if label.Name == name {
			return true
		}
	}
	return false
}

func withoutLaneLabels(labels []github.Label) []github.Label {
	filtered := labels[:0]
	for _, label := range labels {
		if label.Name == "kanban:in-progress" || label.Name == "kanban:review" {
			continue
		}
		filtered = append(filtered, label)
	}
	return filtered
}

func (m model) dispatchCommand() string {
	issue, ok := m.currentIssue()
	if !ok {
		return ""
	}
	command, err := agent.Preview(m.config, m.board.Repo.Slug, issue.Number, m.selectedRunner(), m.selectedMode())
	if err != nil {
		return err.Error()
	}
	return command
}

func buildDispatchCommand(config state.Config, board github.Board, issueNumber int, runner string, mode string) string {
	command, err := agent.Preview(config, board.Repo.Slug, issueNumber, runner, mode)
	if err != nil {
		return err.Error()
	}
	return command
}

func (m *model) prepareDispatch() {
	config, err := state.LoadConfig()
	m.config = config
	m.configErr = err
	m.dispatch.runners = m.config.RunnerNames()
	if len(m.dispatch.runners) == 0 {
		m.dispatch.runners = []string{state.DefaultRunner}
	}
	m.dispatch.modes = dispatchModes()
	repoConfig := m.config.Repos[m.board.Repo.Slug]
	m.dispatch.runnerIndex = indexOf(m.dispatch.runners, repoConfig.PreferredRunner)
	if m.dispatch.runnerIndex < 0 {
		m.dispatch.runnerIndex = indexOf(m.dispatch.runners, state.DefaultRunner)
	}
	if m.dispatch.runnerIndex < 0 {
		m.dispatch.runnerIndex = 0
	}
	m.dispatch.modeIndex = indexOf(m.dispatch.modes, repoConfig.PreferredMode)
	if m.dispatch.modeIndex < 0 {
		m.dispatch.modeIndex = indexOf(m.dispatch.modes, state.DefaultMode)
	}
	if m.dispatch.modeIndex < 0 {
		m.dispatch.modeIndex = 0
	}
	m.dispatch.confirmDuplicate = false
	m.dispatch.pending = false
	m.dispatch.duplicate = nil
}

func dispatchModes() []string {
	return []string{"implement", "plan", "review", "audit"}
}

func (m model) dispatchRunners() []string {
	if len(m.dispatch.runners) > 0 {
		return m.dispatch.runners
	}
	names := m.config.RunnerNames()
	if len(names) == 0 {
		return []string{state.DefaultRunner}
	}
	return names
}

func (m model) dispatchModes() []string {
	if len(m.dispatch.modes) > 0 {
		return m.dispatch.modes
	}
	return dispatchModes()
}

func (m model) selectedRunner() string {
	return selectedValue(m.dispatchRunners(), m.dispatch.runnerIndex, state.DefaultRunner)
}

func (m model) selectedMode() string {
	return selectedValue(m.dispatchModes(), m.dispatch.modeIndex, state.DefaultMode)
}

func (m model) repoKey() string {
	if repoConfig, ok := m.config.Repos[m.board.Repo.Slug]; ok && repoConfig.RepoKey != "" {
		return repoConfig.RepoKey
	}
	if m.board.Repo.Name != "" {
		return m.board.Repo.Name
	}
	return m.board.Repo.Slug
}

func selectedValue(values []string, selected int, fallback string) string {
	if selected >= 0 && selected < len(values) {
		return values[selected]
	}
	return fallback
}

func indexOf(values []string, needle string) int {
	for i, value := range values {
		if value == needle {
			return i
		}
	}
	return -1
}

func segmented(values []string, selected int) string {
	parts := make([]string, 0, len(values))
	for i, value := range values {
		if i == selected {
			parts = append(parts, "[ "+value+" ]")
		} else {
			parts = append(parts, "  "+value+"  ")
		}
	}
	return strings.Join(parts, " ")
}

func overlay(_ string, modal string) string {
	return modal + "\n"
}

func modalStyle(plain bool, terminalWidth int) lipgloss.Style {
	width := clamp(terminalWidth-8, 40, 96)
	border := lipgloss.RoundedBorder()
	if plain {
		border = asciiBorder()
	}
	st := lipgloss.NewStyle().Width(width).Padding(1, 2).Border(border)
	if !plain {
		st = st.BorderForeground(lipgloss.Color("212")).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("235"))
	}
	return st
}

func style(plain bool, st lipgloss.Style) lipgloss.Style {
	if plain {
		return lipgloss.NewStyle().Width(st.GetWidth()).Align(st.GetAlign())
	}
	return st
}

func asciiBorder() lipgloss.Border {
	return lipgloss.Border{
		Top:         "-",
		Bottom:      "-",
		Left:        "|",
		Right:       "|",
		TopLeft:     "+",
		TopRight:    "+",
		BottomLeft:  "+",
		BottomRight: "+",
	}
}

func openBrowser(url string) tea.Cmd {
	return func() tea.Msg {
		var command string
		var args []string
		if runtime.GOOS == "darwin" {
			command = "open"
			args = []string{url}
		} else {
			command = "xdg-open"
			args = []string{url}
		}
		_ = exec.Command(command, args...).Start()
		return nil
	}
}

func currentBranch(path string) string {
	if path == "" {
		return "unknown"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", path, "branch", "--show-current").Output()
	if err != nil {
		return "unknown"
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" {
		return "detached"
	}
	return branch
}

func refreshTTL() time.Duration {
	raw := strings.TrimSpace(os.Getenv("KANBAN_REFRESH_TTL"))
	if raw == "" {
		return 60 * time.Second
	}
	if duration, err := time.ParseDuration(raw); err == nil && duration > 0 {
		return duration
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return 60 * time.Second
}

func noColor() bool {
	return os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb"
}

func since(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

func truncate(value string, width int) string {
	value = strings.TrimSpace(value)
	if width <= 0 || len(value) <= width {
		return value
	}
	if width <= 1 {
		return value[:width]
	}
	if width <= 3 {
		return value[:width]
	}
	return value[:width-3] + "..."
}

func clamp(value int, low int, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
