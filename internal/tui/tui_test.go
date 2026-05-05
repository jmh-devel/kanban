package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jmh-devel/kanban/internal/agent"
	"github.com/jmh-devel/kanban/internal/github"
	"github.com/jmh-devel/kanban/internal/repo"
	"github.com/jmh-devel/kanban/internal/state"
)

func TestBuildColumnsAlwaysReturnsFourLanes(t *testing.T) {
	board := github.Board{
		Repo: repo.Details{Slug: "jmh-devel/example"},
		Sections: []github.Section{{
			Title: "Unscheduled",
			Issues: []github.Issue{
				{Number: 4, Title: "Backlog"},
				{Number: 3, Title: "Implement", Labels: []github.Label{{Name: "kanban:in-progress"}}},
				{Number: 2, Title: "Review", Labels: []github.Label{{Name: "kanban:review"}}},
			},
		}},
		ClosedIssues: []github.Issue{{Number: 1, Title: "Done", State: "CLOSED"}},
		UpdatedAt:    time.Now(),
	}

	columns := buildColumns(board)
	if len(columns) != 4 {
		t.Fatalf("got %d columns, want 4", len(columns))
	}
	wants := []struct {
		title string
		count int
	}{
		{"Backlog", 1},
		{"In Progress", 1},
		{"Review", 1},
		{"Done", 1},
	}
	for i, want := range wants {
		if columns[i].title != want.title || len(columns[i].issues) != want.count {
			t.Fatalf("column %d = %s/%d, want %s/%d", i, columns[i].title, len(columns[i].issues), want.title, want.count)
		}
	}
}

func TestViewRendersFourColumnBoardAtAcceptedWidths(t *testing.T) {
	board := github.Board{
		Repo:      repo.Details{Slug: "jmh-devel/example", Name: "example"},
		UpdatedAt: time.Now(),
		Sections: []github.Section{{Title: "Unscheduled", Issues: []github.Issue{
			{Number: 12, Title: "Build server", Labels: []github.Label{{Name: "priority:high"}}},
		}}},
	}
	for _, width := range []int{80, 120, 200} {
		model := newModel(board, nil, nil)
		model.width = width
		model.height = 32
		out := model.View()
		for _, lane := range laneNames {
			if !strings.Contains(out, lane) {
				t.Fatalf("width %d missing lane %q in:\n%s", width, lane, out)
			}
		}
		if !strings.Contains(out, "#12") {
			t.Fatalf("width %d missing card in:\n%s", width, out)
		}
	}
}

func TestNarrowViewFallsBackToTextBoard(t *testing.T) {
	board := github.Board{
		Repo:      repo.Details{Slug: "jmh-devel/example"},
		UpdatedAt: time.Date(2026, 5, 5, 7, 0, 0, 0, time.UTC),
		Sections:  []github.Section{{Title: "Unscheduled", Issues: []github.Issue{{Number: 12, Title: "Build server"}}}},
	}
	model := newModel(board, nil, nil)
	model.width = 79
	out := model.View()
	if !strings.Contains(out, "Repo: jmh-devel/example") || strings.Contains(out, "In Progress") {
		t.Fatalf("narrow view did not use print fallback:\n%s", out)
	}
}

func TestNarrowEnterRendersIssueDetails(t *testing.T) {
	board := github.Board{
		Repo:      repo.Details{Slug: "jmh-devel/example"},
		UpdatedAt: time.Date(2026, 5, 5, 7, 0, 0, 0, time.UTC),
		Sections: []github.Section{{Title: "Unscheduled", Issues: []github.Issue{{
			Number: 12,
			Title:  "Build server",
			Body:   "narrow body",
			URL:    "https://github.com/jmh-devel/example/issues/12",
		}}}},
	}
	m := newModel(board, nil, nil)
	m.width = 79

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)
	out := got.View()
	if got.mode != modeExpand || !strings.Contains(out, "narrow body") || !strings.Contains(out, "[Esc/q] back") {
		t.Fatalf("narrow expand did not render details:\n%s", out)
	}
}

func TestNoColorUsesAccessibleASCII(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	board := github.Board{
		Repo:      repo.Details{Slug: "jmh-devel/example"},
		UpdatedAt: time.Now(),
		Sections:  []github.Section{{Title: "Unscheduled", Issues: []github.Issue{{Number: 12, Title: "Build server"}}}},
	}
	model := newModel(board, nil, nil)
	model.width = 120
	model.height = 24
	out := model.View()
	if strings.ContainsAny(out, "┌┐└┘╭╮╰╯─│") {
		t.Fatalf("NO_COLOR output contains box-drawing characters:\n%s", out)
	}
	if !strings.Contains(out, "+") || !strings.Contains(out, "|") {
		t.Fatalf("NO_COLOR output missing ASCII card borders:\n%s", out)
	}
}

func TestExpandContentIncludesFullBody(t *testing.T) {
	content := expandContent(github.Issue{
		Number: 12,
		Title:  "Build server",
		Body:   "line one\nline two",
		URL:    "https://github.com/jmh-devel/example/issues/12",
	})
	if !strings.Contains(content, "line one\nline two") {
		t.Fatalf("expand content missing body:\n%s", content)
	}
	if !strings.Contains(content, "https://github.com/jmh-devel/example/issues/12") {
		t.Fatalf("expand content missing link:\n%s", content)
	}
}

func TestBuildDispatchCommand(t *testing.T) {
	board := github.Board{Repo: repo.Details{Slug: "jmh-devel/example", Name: "example"}}
	config := state.Config{
		Repos: map[string]state.RepoConfig{"jmh-devel/example": {RepoKey: "example"}},
		Runners: map[string]state.RunnerConfig{
			"tsctl": {Kind: "tsctl_dispatch"},
		},
	}
	got := buildDispatchCommand(config, board, 318, "tsctl", "implement")
	want := "tsctl agent dispatch example --runner tsctl --issue 318 --mode implement"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDispatchEnterExecutesDispatcherAndRefreshes(t *testing.T) {
	t.Setenv("KANBAN_CONFIG_DIR", t.TempDir())
	if err := state.SaveConfig(dispatchTestConfig()); err != nil {
		t.Fatal(err)
	}
	loaderCalls := 0
	board := github.Board{
		Repo: repo.Details{Slug: "jmh-devel/example"},
		Sections: []github.Section{{Title: "Unscheduled", Issues: []github.Issue{{
			Number: 12,
			Title:  "Dispatch me",
		}}}},
		UpdatedAt: time.Date(2026, 5, 5, 7, 0, 0, 0, time.UTC),
	}
	m := newModel(board, func(context.Context) (github.Board, error) {
		loaderCalls++
		board.UpdatedAt = board.UpdatedAt.Add(time.Minute)
		return board, nil
	}, nil)
	m.dispatcher = agent.Dispatcher{
		ExecCommand: func(_ context.Context, command string) error {
			want := "tsctl agent dispatch example --runner tsctl --issue 12 --mode implement"
			if command != want {
				t.Fatalf("command = %q, want %q", command, want)
			}
			return nil
		},
		MoveIssue: func(context.Context, string, int) error {
			t.Fatal("auto move should be disabled in this test")
			return nil
		},
		Now: func() time.Time {
			return time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC)
		},
	}
	m.prepareDispatch()
	m.mode = modeDispatch

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected dispatch command")
	}
	msg := cmd()
	updated, cmd = updated.(model).Update(msg)
	if cmd == nil {
		t.Fatal("expected refresh command after dispatch")
	}
	updated, _ = updated.(model).Update(cmd())
	got := updated.(model)

	if got.mode != modeBoard || loaderCalls != 1 {
		t.Fatalf("mode=%v loaderCalls=%d", got.mode, loaderCalls)
	}
	dispatches, err := state.LoadDispatches()
	if err != nil {
		t.Fatal(err)
	}
	if len(dispatches) != 1 || dispatches[0].Runner != "tsctl" || dispatches[0].Issue != 12 {
		t.Fatalf("dispatches = %+v", dispatches)
	}
}

func TestDispatchEnterRequiresSecondEnterForDuplicate(t *testing.T) {
	t.Setenv("KANBAN_CONFIG_DIR", t.TempDir())
	if err := state.SaveConfig(dispatchTestConfig()); err != nil {
		t.Fatal(err)
	}
	existing := state.Dispatch{
		Repo:         "jmh-devel/example",
		Issue:        12,
		Runner:       "tsctl",
		Mode:         "implement",
		DispatchedAt: time.Date(2026, 5, 5, 7, 0, 0, 0, time.UTC),
		Command:      "tsctl agent dispatch example --runner tsctl --issue 12 --mode implement",
		Status:       state.StatusDispatched,
	}
	if err := state.SaveDispatches([]state.Dispatch{existing}); err != nil {
		t.Fatal(err)
	}
	board := github.Board{
		Repo:      repo.Details{Slug: "jmh-devel/example"},
		UpdatedAt: time.Date(2026, 5, 5, 7, 0, 0, 0, time.UTC),
		Sections:  []github.Section{{Title: "Unscheduled", Issues: []github.Issue{{Number: 12, Title: "Dispatch me"}}}},
	}
	executed := 0
	m := newModel(board, func(context.Context) (github.Board, error) { return board, nil }, nil)
	m.dispatcher = agent.Dispatcher{
		ExecCommand: func(context.Context, string) error {
			executed++
			return nil
		},
		Now: func() time.Time {
			return time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC)
		},
	}
	m.prepareDispatch()
	m.mode = modeDispatch

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = updated.(model).Update(cmd())
	got := updated.(model)
	if executed != 0 || !got.dispatch.confirmDuplicate || got.dispatch.duplicate == nil || got.mode != modeDispatch {
		t.Fatalf("executed=%d duplicate=%+v mode=%v", executed, got.dispatch.duplicate, got.mode)
	}

	updated, cmd = got.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	updated, cmd = updated.(model).Update(cmd())
	if cmd == nil {
		t.Fatal("expected refresh command after confirmed duplicate")
	}
	updated, _ = updated.(model).Update(cmd())
	got = updated.(model)
	if executed != 1 || got.mode != modeBoard {
		t.Fatalf("executed=%d mode=%v", executed, got.mode)
	}
}

func dispatchTestConfig() state.Config {
	autoMove := false
	return state.Config{
		Repos: map[string]state.RepoConfig{
			"jmh-devel/example": {
				RepoKey:         "example",
				PreferredRunner: "tsctl",
				PreferredMode:   "implement",
			},
		},
		Runners: map[string]state.RunnerConfig{
			"tsctl": {Kind: "tsctl_dispatch"},
		},
		Agent: state.AgentConfig{AutoMoveOnDispatch: &autoMove},
	}
}

func TestMoveIssueLocalAppliesLaneLabel(t *testing.T) {
	board := github.Board{
		Sections: []github.Section{{Title: "Unscheduled", Issues: []github.Issue{{
			Number: 12,
			Title:  "Build server",
			Labels: []github.Label{{Name: "kanban:review"}, {Name: "backend"}},
		}}}},
	}
	moved := moveIssueLocal(board, board.Sections[0].Issues[0], laneInProgress)
	columns := buildColumns(moved)
	if len(columns[1].issues) != 1 || !hasLabel(columns[1].issues[0], "kanban:in-progress") {
		t.Fatalf("issue was not moved to in-progress: %#v", columns)
	}
	if hasLabel(columns[1].issues[0], "kanban:review") {
		t.Fatalf("conflicting review label remained: %#v", columns[1].issues[0].Labels)
	}
}

func TestBuildColumnsDoneSectionDoesNotLeakIntoBacklog(t *testing.T) {
	board := github.Board{
		Sections: []github.Section{
			{Title: "Backlog", Issues: nil},
			{Title: "In Progress", Issues: nil},
			{Title: "Review", Issues: nil},
			{Title: "Done", Issues: []github.Issue{{Number: 3, Title: "Closed", State: "CLOSED"}}},
		},
		ClosedIssues: []github.Issue{{Number: 3, Title: "Closed", State: "CLOSED"}},
	}

	columns := buildColumns(board)
	if len(columns[0].issues) != 0 {
		t.Fatalf("backlog should be empty, got %d issues", len(columns[0].issues))
	}
	if len(columns[3].issues) != 1 || columns[3].issues[0].Number != 3 {
		t.Fatalf("done should contain only issue #3, got %#v", columns[3].issues)
	}
}
