package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/jmh-devel/kanban/internal/github"
	"github.com/jmh-devel/kanban/internal/repo"
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
	got := buildDispatchCommand(board, 318, "codex", "implement")
	want := "tsctl agent dispatch example --runner codex --issue 318 --mode implement"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
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
