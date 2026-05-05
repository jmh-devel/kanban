package github

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeGH struct {
	calls []string
	fn    func(args []string) (string, error)
}

func (f *fakeGH) run(_ context.Context, args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	if f.fn == nil {
		return "[]", nil
	}
	return f.fn(args)
}

func TestBuildLaneSections(t *testing.T) {
	sections := buildLaneSections(nil, []Issue{
		{Number: 1, Title: "Backlog"},
		{Number: 2, Title: "Started", Labels: []Label{{Name: InProgressLabel}}},
		{Number: 3, Title: "Review", Labels: []Label{{Name: ReviewLabel}}},
		{Number: 4, Title: "Done", State: "closed"},
	}, false)

	gotTitles := make([]string, 0, len(sections))
	gotNumbers := make([][]int, 0, len(sections))
	for _, section := range sections {
		gotTitles = append(gotTitles, section.Title)
		var numbers []int
		for _, issue := range section.Issues {
			numbers = append(numbers, issue.Number)
		}
		gotNumbers = append(gotNumbers, numbers)
	}

	wantTitles := []string{"Backlog", "In Progress", "Review", "Done"}
	if !reflect.DeepEqual(gotTitles, wantTitles) {
		t.Fatalf("unexpected section titles: got %v want %v", gotTitles, wantTitles)
	}
	wantNumbers := [][]int{{1}, {2}, {3}, {4}}
	if !reflect.DeepEqual(gotNumbers, wantNumbers) {
		t.Fatalf("unexpected issue grouping: got %v want %v", gotNumbers, wantNumbers)
	}
}

func TestBuildLaneSectionsWithMilestoneGroups(t *testing.T) {
	sections := buildLaneSections(
		[]Milestone{{Title: "Sprint 1"}},
		[]Issue{
			{Number: 3, Title: "Later", Labels: []Label{{Name: InProgressLabel}}, Milestone: &MilestoneRef{Title: "Sprint 1"}},
			{Number: 2, Title: "Unscheduled", Labels: []Label{{Name: InProgressLabel}}},
		},
		true,
	)

	inProgress := sections[1]
	if len(inProgress.Issues) != 2 {
		t.Fatalf("lane should keep flat issue list for counts, got %d", len(inProgress.Issues))
	}
	if len(inProgress.Sections) != 2 {
		t.Fatalf("expected milestone and unscheduled groups, got %d", len(inProgress.Sections))
	}
	gotTitles := []string{inProgress.Sections[0].Title, inProgress.Sections[1].Title}
	wantTitles := []string{"Sprint 1", "Unscheduled"}
	if !reflect.DeepEqual(gotTitles, wantTitles) {
		t.Fatalf("unexpected subgroup titles: got %v want %v", gotTitles, wantTitles)
	}
}

func TestMoveIssueToInProgress(t *testing.T) {
	fake := &fakeGH{}
	client := &Client{runner: fake.run}

	if err := client.MoveIssue(context.Background(), "jmh-devel/kanban", 42, LaneInProgress); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	want := []string{"issue edit 42 --repo jmh-devel/kanban --remove-label kanban:review --add-label kanban:in-progress"}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("unexpected calls: got %v want %v", fake.calls, want)
	}
}

func TestMoveIssueToDoneClosesIssue(t *testing.T) {
	fake := &fakeGH{}
	client := &Client{runner: fake.run}

	if err := client.MoveIssue(context.Background(), "jmh-devel/kanban", 42, LaneDone); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	want := []string{
		"issue edit 42 --repo jmh-devel/kanban --remove-label kanban:in-progress --remove-label kanban:review",
		"issue close 42 --repo jmh-devel/kanban",
	}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("unexpected calls: got %v want %v", fake.calls, want)
	}
}

func TestMoveIssueMissingLaneLabelMessage(t *testing.T) {
	fake := &fakeGH{fn: func(_ []string) (string, error) {
		return "", errors.New("label kanban:in-progress not found")
	}}
	client := &Client{runner: fake.run}

	err := client.MoveIssue(context.Background(), "jmh-devel/kanban", 42, LaneInProgress)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "kanban init --setup-labels") {
		t.Fatalf("missing setup hint: %v", err)
	}
}
