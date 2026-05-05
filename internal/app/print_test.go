package app

import (
	"strings"
	"testing"
	"time"

	"github.com/jmh-devel/kanban/internal/github"
	"github.com/jmh-devel/kanban/internal/repo"
)

func TestRenderBoardText(t *testing.T) {
	board := github.Board{
		Repo:      repo.Details{Slug: "jmh-devel/example", RootPath: "/tmp/example"},
		UpdatedAt: time.Date(2026, 5, 5, 7, 0, 0, 0, time.UTC),
		Sections: []github.Section{
			{Title: "Sprint 1", Issues: []github.Issue{{Number: 12, Title: "Build server", Labels: []github.Label{{Name: "backend"}}}}},
			{Title: "Unscheduled", Issues: nil},
		},
	}

	out := RenderBoardText(board)
	if !strings.Contains(out, "Repo: jmh-devel/example") {
		t.Fatalf("missing repo line: %s", out)
	}
	if !strings.Contains(out, "#12 Build server [backend]") {
		t.Fatalf("missing issue line: %s", out)
	}
	if !strings.Contains(out, "(no open issues)") {
		t.Fatalf("missing empty-section marker: %s", out)
	}
}
