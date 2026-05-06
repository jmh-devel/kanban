package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jmh-devel/kanban/internal/github"
	"github.com/jmh-devel/kanban/internal/repo"
	"github.com/jmh-devel/kanban/internal/state"
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

func TestHandleIssueMoveClearsActiveDispatchOutsideInProgress(t *testing.T) {
	t.Setenv("KANBAN_CONFIG_DIR", t.TempDir())
	config := state.DefaultConfig()
	config.ReviewAgent.Mode = "manual"
	if err := state.SaveConfig(config); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveDispatches([]state.Dispatch{{
		Repo:   "jmh-devel/example",
		Issue:  42,
		Runner: "codex",
		Mode:   "implement",
		Status: state.StatusDispatched,
	}}); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(func(context.Context) (github.Board, error) {
		return github.Board{Repo: repo.Details{Slug: "jmh-devel/example"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server.mover = func(context.Context, string, int, github.Lane) error {
		return nil
	}

	request := httptest.NewRequest(http.MethodPost, "http://example.com/api/issues/move", strings.NewReader(`{"issue":42,"lane":"review"}`))
	recorder := httptest.NewRecorder()
	server.newMux().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	dispatches, err := state.LoadDispatches()
	if err != nil {
		t.Fatal(err)
	}
	if len(dispatches) != 2 || dispatches[0].Status != state.StatusCompleted || dispatches[1].Type != state.TypeReview {
		t.Fatalf("dispatches = %+v", dispatches)
	}
}

func TestHandleIssueMoveToReviewRecordsReviewDispatch(t *testing.T) {
	t.Setenv("KANBAN_CONFIG_DIR", t.TempDir())
	config := state.DefaultConfig()
	config.ReviewAgent.Mode = "manual"
	if err := state.SaveConfig(config); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(func(context.Context) (github.Board, error) {
		return github.Board{Repo: repo.Details{Slug: "jmh-devel/example"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server.mover = func(context.Context, string, int, github.Lane) error {
		return nil
	}

	request := httptest.NewRequest(http.MethodPost, "http://example.com/api/issues/move", strings.NewReader(`{"issue":42,"lane":"review"}`))
	recorder := httptest.NewRecorder()
	server.newMux().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	var response struct {
		ReviewDispatched bool `json:"review_dispatched"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !response.ReviewDispatched {
		t.Fatalf("response = %+v", response)
	}
	dispatches, err := state.LoadDispatches()
	if err != nil {
		t.Fatal(err)
	}
	if len(dispatches) != 1 || dispatches[0].Type != state.TypeReview || dispatches[0].Mode != "manual" {
		t.Fatalf("dispatches = %+v", dispatches)
	}
}

func TestIndexRendersIssueDataForDetails(t *testing.T) {
	t.Setenv("KANBAN_CONFIG_DIR", t.TempDir())
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
	for _, want := range []string{"const boardData =", "detailBackdrop", "refreshBoard", "detailMergeDone", "Already in", "card not found after reload", "data-issue=\"12\"", "depends on #13"} {
		if !strings.Contains(body, want) {
			t.Fatalf("response missing %q:\n%s", want, body)
		}
	}
}

func TestDispatchOptionsDefaultToCodex(t *testing.T) {
	t.Setenv("KANBAN_CONFIG_DIR", t.TempDir())
	server, err := NewServer(func(context.Context) (github.Board, error) {
		return github.Board{Repo: repo.Details{Slug: "jmh-devel/example"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "http://example.com/api/dispatch/options?issue=42", nil)
	recorder := httptest.NewRecorder()
	server.newMux().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Runner  string   `json:"runner"`
		Runners []string `json:"runners"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Runner != state.DefaultRunner {
		t.Fatalf("runner = %q, want %q", response.Runner, state.DefaultRunner)
	}
	if state.DefaultRunner != "codex" {
		t.Fatalf("DefaultRunner = %q, want codex", state.DefaultRunner)
	}
	if !contains(response.Runners, state.ManualRunner) {
		t.Fatalf("runners = %v, missing manual", response.Runners)
	}
}

func TestDispatchOptionsUsePreferredRunner(t *testing.T) {
	t.Setenv("KANBAN_CONFIG_DIR", t.TempDir())
	config := state.DefaultConfig()
	config.Repos = map[string]state.RepoConfig{
		"jmh-devel/example": {PreferredRunner: "tsctl"},
	}
	if err := state.SaveConfig(config); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(func(context.Context) (github.Board, error) {
		return github.Board{Repo: repo.Details{Slug: "jmh-devel/example"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "http://example.com/api/dispatch/options?issue=42", nil)
	recorder := httptest.NewRecorder()
	server.newMux().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Runner string `json:"runner"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Runner != "tsctl" {
		t.Fatalf("runner = %q, want tsctl", response.Runner)
	}
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
