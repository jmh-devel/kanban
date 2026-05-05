package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jmh-devel/kanban/internal/repo"
)

type fakeGHRunner struct {
	responses map[string]string
	calls     []string
}

func (f *fakeGHRunner) run(_ context.Context, args ...string) (string, error) {
	call := strings.Join(args, " ")
	f.calls = append(f.calls, call)
	if out, ok := f.responses[call]; ok {
		return out, nil
	}
	return "", errors.New("unexpected gh command: " + call)
}

func TestBuildLaneSections(t *testing.T) {
	sections := buildLaneSections([]Issue{
		{Number: 1, Title: "old backlog"},
		{Number: 9, Title: "review", Labels: []Label{{Name: LabelReview}}},
		{Number: 3, Title: "started", Labels: []Label{{Name: LabelInProgress}}},
		{Number: 7, Title: "new backlog"},
		{Number: 5, Title: "done", State: "closed"},
	})

	wantTitles := []string{"Backlog", "In Progress", "Review", "Done"}
	for i, title := range wantTitles {
		if sections[i].Title != title {
			t.Fatalf("section %d title = %q, want %q", i, sections[i].Title, title)
		}
	}
	if got := issueNumbers(sections[0].Issues); strings.Join(got, ",") != "7,1" {
		t.Fatalf("backlog numbers = %v, want [7 1]", got)
	}
	if got := issueNumbers(sections[1].Issues); strings.Join(got, ",") != "3" {
		t.Fatalf("in-progress numbers = %v, want [3]", got)
	}
	if got := issueNumbers(sections[2].Issues); strings.Join(got, ",") != "9" {
		t.Fatalf("review numbers = %v, want [9]", got)
	}
	if got := issueNumbers(sections[3].Issues); strings.Join(got, ",") != "5" {
		t.Fatalf("done numbers = %v, want [5]", got)
	}
}

func TestLoadBoardUsesDoneWindow(t *testing.T) {
	runner := &fakeGHRunner{responses: map[string]string{
		"issue list --repo jmh-devel/kanban --state open --limit 500 --json number,title,url,labels,milestone,state,closedAt":                   `[{"number":2,"title":"Open","state":"OPEN","labels":[]}]`,
		"issue list --repo jmh-devel/kanban --state closed --limit 500 --json number,title,url,labels,milestone,state,closedAt --search closed:>=2026-04-21": `[{"number":1,"title":"Closed","state":"CLOSED","labels":[]}]`,
	}}
	client := &Client{runner: runner.run}

	board, err := client.LoadBoardWithOptions(context.Background(), testRepoDetails(), LoadOptions{
		DoneWindowDays: 14,
		Now:            time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("LoadBoardWithOptions returned error: %v", err)
	}
	if got := issueNumbers(board.Sections[0].Issues); strings.Join(got, ",") != "2" {
		t.Fatalf("backlog numbers = %v, want [2]", got)
	}
	if got := issueNumbers(board.Sections[3].Issues); strings.Join(got, ",") != "1" {
		t.Fatalf("done numbers = %v, want [1]", got)
	}
}

func TestMoveIssueToInProgress(t *testing.T) {
	runner := &fakeGHRunner{responses: map[string]string{
		"issue edit 12 --repo jmh-devel/kanban --remove-label kanban:review --add-label kanban:in-progress": "",
	}}
	client := &Client{runner: runner.run}

	if err := client.MoveIssue(context.Background(), "jmh-devel/kanban", 12, LaneInProgress); err != nil {
		t.Fatalf("MoveIssue returned error: %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 call, got %v", runner.calls)
	}
}

func TestMoveIssueToDoneClosesIssue(t *testing.T) {
	runner := &fakeGHRunner{responses: map[string]string{
		"issue edit 12 --repo jmh-devel/kanban --remove-label kanban:in-progress --remove-label kanban:review": "",
		"issue close 12 --repo jmh-devel/kanban": "",
	}}
	client := &Client{runner: runner.run}

	if err := client.MoveIssue(context.Background(), "jmh-devel/kanban", 12, LaneDone); err != nil {
		t.Fatalf("MoveIssue returned error: %v", err)
	}
	if got := strings.Join(runner.calls, "\n"); !strings.Contains(got, "issue close 12 --repo jmh-devel/kanban") {
		t.Fatalf("missing issue close call: %v", runner.calls)
	}
}

func issueNumbers(issues []Issue) []string {
	numbers := make([]string, 0, len(issues))
	for _, issue := range issues {
		numbers = append(numbers, fmt.Sprint(issue.Number))
	}
	return numbers
}

func testRepoDetails() repo.Details {
	return repo.Details{Slug: "jmh-devel/kanban"}
}
