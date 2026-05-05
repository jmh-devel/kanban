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
		m := newModel(board, nil, nil)
		m.width = width
		m.height = 32
		out := m.View()
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
	m := newModel(board, nil, nil)
	m.width = 79
	out := m.View()
	if !strings.Contains(out, "Repo: jmh-devel/example") || strings.Contains(out, "In Progress") {
		t.Fatalf("narrow view did not use print fallback:\n%s", out)
	}
}

func TestNoColorUsesAccessibleASCII(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	board := github.Board{
		Repo:      repo.Details{Slug: "jmh-devel/example"},
		UpdatedAt: time.Now(),
		Sections:  []github.Section{{Title: "Unscheduled", Issues: []github.Issue{{Number: 12, Title: "Build server"}}}},
	}
	m := newModel(board, nil, nil)
	m.width = 120
	m.height = 24
	out := m.View()
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
	config := state.DefaultConfig()
	config.Repos = map[string]state.RepoConfig{"jmh-devel/example": {RepoKey: "example"}}
	got, err := buildDispatchCommand(config, board, 318, "tsctl", "implement")
	if err != nil {
		t.Fatalf("buildDispatchCommand returned error: %v", err)
	}
	wantPrefix := "tsctl agent dispatch example --runner tsctl --issue 318 --mode implement"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("got %q, want prefix %q", got, wantPrefix)
	}
}

func TestDispatchEnterCallsBackend(t *testing.T) {
	board := github.Board{
		Repo:      repo.Details{Slug: "jmh-devel/example"},
		UpdatedAt: time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
		Sections:  []github.Section{{Title: "Backlog", Issues: []github.Issue{{Number: 318, Title: "Build dispatch"}}}},
	}
	config := state.DefaultConfig()
	config.Repos = map[string]state.RepoConfig{
		"jmh-devel/example": {RepoKey: "example", PreferredRunner: "tsctl", PreferredMode: "implement"},
	}
	called := false
	dispatcher := func(_ context.Context, _ state.Config, request agent.Request) (agent.Result, error) {
		called = true
		if request.Repo != "jmh-devel/example" || request.Issue != 318 || request.Runner != "tsctl" || request.Mode != "implement" || request.ConfirmDuplicate {
			t.Fatalf("request = %#v", request)
		}
		return agent.Result{Dispatch: state.Dispatch{
			Repo:         request.Repo,
			Issue:        request.Issue,
			Runner:       request.Runner,
			Mode:         request.Mode,
			DispatchedAt: time.Date(2026, 5, 5, 12, 1, 0, 0, time.UTC),
			Status:       state.StatusDispatched,
		}}, nil
	}
	m := newModelWithConfig(board, nil, nil, dispatcher, config)
	m.mode = modeDispatch

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("dispatch enter returned nil command")
	}
	updated, _ = updated.Update(cmd())
	got := updated.(model)
	if !called {
		t.Fatal("dispatcher was not called")
	}
	if got.mode != modeBoard {
		t.Fatalf("mode = %v, want board", got.mode)
	}
	issue := got.columns[0].issues[0]
	if issue.Agent == nil || issue.Agent.Runner != "tsctl" || issue.Agent.Mode != "implement" {
		t.Fatalf("issue agent status not updated: %#v", issue.Agent)
	}
}

func TestDispatchDuplicateRequiresSecondEnter(t *testing.T) {
	board := github.Board{
		Repo:      repo.Details{Slug: "jmh-devel/example"},
		UpdatedAt: time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
		Sections:  []github.Section{{Title: "Backlog", Issues: []github.Issue{{Number: 318, Title: "Build dispatch"}}}},
	}
	config := state.DefaultConfig()
	config.Repos = map[string]state.RepoConfig{
		"jmh-devel/example": {RepoKey: "example", PreferredRunner: "tsctl", PreferredMode: "implement"},
	}
	calls := 0
	dispatcher := func(_ context.Context, _ state.Config, request agent.Request) (agent.Result, error) {
		calls++
		if calls == 1 {
			if request.ConfirmDuplicate {
				t.Fatal("first dispatch unexpectedly confirmed duplicate")
			}
			return agent.Result{Duplicate: &agent.Duplicate{Runner: "tsctl", Mode: "implement", DispatchedAt: time.Date(2026, 5, 5, 11, 0, 0, 0, time.UTC)}}, nil
		}
		if !request.ConfirmDuplicate {
			t.Fatal("second dispatch did not confirm duplicate")
		}
		return agent.Result{Dispatch: state.Dispatch{
			Repo:         request.Repo,
			Issue:        request.Issue,
			Runner:       request.Runner,
			Mode:         request.Mode,
			DispatchedAt: time.Date(2026, 5, 5, 12, 1, 0, 0, time.UTC),
			Status:       state.StatusDispatched,
		}}, nil
	}
	m := newModelWithConfig(board, nil, nil, dispatcher, config)
	m.mode = modeDispatch

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = updated.Update(cmd())
	afterDuplicate := updated.(model)
	if !afterDuplicate.dispatch.confirmDuplicate || afterDuplicate.mode != modeDispatch {
		t.Fatalf("duplicate state not retained: %#v", afterDuplicate.dispatch)
	}
	if !strings.Contains(afterDuplicate.renderDispatch(), "Press Enter again") {
		t.Fatalf("duplicate prompt missing:\n%s", afterDuplicate.renderDispatch())
	}

	updated, cmd = afterDuplicate.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = updated.Update(cmd())
	afterConfirm := updated.(model)
	if afterConfirm.mode != modeBoard || calls != 2 {
		t.Fatalf("duplicate confirmation failed: mode=%v calls=%d", afterConfirm.mode, calls)
	}
}

func TestNarrowEnterRendersIssueDetail(t *testing.T) {
	board := github.Board{
		Repo:      repo.Details{Slug: "jmh-devel/example"},
		UpdatedAt: time.Date(2026, 5, 5, 7, 0, 0, 0, time.UTC),
		Sections:  []github.Section{{Title: "Backlog", Issues: []github.Issue{{Number: 12, Title: "Build server", Body: "full body"}}}},
	}
	m := newModel(board, nil, nil)
	m.width = 79
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	out := updated.(model).View()
	if !strings.Contains(out, "full body") || !strings.Contains(out, "[Esc] back") {
		t.Fatalf("narrow expand did not render detail view:\n%s", out)
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
