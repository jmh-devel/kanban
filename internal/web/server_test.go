package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jmh-devel/kanban/internal/github"
	"github.com/jmh-devel/kanban/internal/repo"
)

func TestHandleMoveIssueUsesBoardRepoAndLane(t *testing.T) {
	server, err := NewServer(func(context.Context) (github.Board, error) {
		return github.Board{Repo: repo.Details{Slug: "jmh-devel/kanban"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var gotSlug string
	var gotIssue int
	var gotLane github.Lane
	server.mover = func(_ context.Context, slug string, issue int, lane github.Lane) error {
		gotSlug = slug
		gotIssue = issue
		gotLane = lane
		return nil
	}

	body, _ := json.Marshal(map[string]any{"issue": 11, "lane": "In Progress"})
	request := httptest.NewRequest(http.MethodPost, "/api/issues/move", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	server.handleMoveIssue(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	if gotSlug != "jmh-devel/kanban" || gotIssue != 11 || gotLane != github.LaneInProgress {
		t.Fatalf("move = slug %q issue %d lane %q", gotSlug, gotIssue, gotLane)
	}
}

func TestHandleMoveIssueRejectsUnknownLane(t *testing.T) {
	server, err := NewServer(func(context.Context) (github.Board, error) {
		return github.Board{Repo: repo.Details{Slug: "jmh-devel/kanban"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server.mover = func(context.Context, string, int, github.Lane) error {
		t.Fatal("mover should not be called")
		return nil
	}

	body, _ := json.Marshal(map[string]any{"issue": 11, "lane": "blocked"})
	request := httptest.NewRequest(http.MethodPost, "/api/issues/move", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	server.handleMoveIssue(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}
