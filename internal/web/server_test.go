package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jmh-devel/kanban/internal/github"
	"github.com/jmh-devel/kanban/internal/repo"
)

func TestHandleIssueMoveCallsMover(t *testing.T) {
	server, err := NewServer(func(context.Context) (github.Board, error) {
		return github.Board{Repo: repo.Details{Slug: "jmh-devel/example"}}, nil
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

	request := httptest.NewRequest(http.MethodPost, "http://example.com/api/issues/move", strings.NewReader(`{"issue":42,"lane":"in-progress"}`))
	recorder := httptest.NewRecorder()
	server.newMux().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	if gotSlug != "jmh-devel/example" || gotIssue != 42 || gotLane != github.LaneInProgress {
		t.Fatalf("move call = %q %d %q", gotSlug, gotIssue, gotLane)
	}
}

func TestHandleIssueMoveRejectsUnknownLane(t *testing.T) {
	server, err := NewServer(func(context.Context) (github.Board, error) {
		return github.Board{Repo: repo.Details{Slug: "jmh-devel/example"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	server.mover = func(context.Context, string, int, github.Lane) error {
		called = true
		return nil
	}

	request := httptest.NewRequest(http.MethodPost, "http://example.com/api/issues/move", strings.NewReader(`{"issue":42,"lane":"blocked"}`))
	recorder := httptest.NewRecorder()
	server.newMux().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	if called {
		t.Fatal("mover was called for invalid lane")
	}
}

func TestIndexRendersIssueDataForDetails(t *testing.T) {
	server, err := NewServer(func(context.Context) (github.Board, error) {
		return github.Board{
			Repo:      repo.Details{Slug: "jmh-devel/example"},
			UpdatedAt: time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
			Sections: []github.Section{{
				Title: "Backlog",
				Issues: []github.Issue{{
					Number: 12,
					Title:  "Build WUI",
					Body:   "depends on #13",
					URL:    "https://github.com/jmh-devel/example/issues/12",
				}},
			}},
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	recorder := httptest.NewRecorder()
	server.newMux().ServeHTTP(recorder, request)

	body := recorder.Body.String()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, body)
	}
	for _, want := range []string{"const boardData =", "detailBackdrop", "data-issue=\"12\"", "depends on #13"} {
		if !strings.Contains(body, want) {
			t.Fatalf("response missing %q:\n%s", want, body)
		}
	}
}
