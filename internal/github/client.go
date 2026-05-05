package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/jmh-devel/kanban/internal/repo"
)

type Label struct {
	Name        string `json:"name"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
}

type Milestone struct {
	Number       int    `json:"number"`
	Title        string `json:"title"`
	Description  string `json:"description,omitempty"`
	DueOn        string `json:"due_on,omitempty"`
	OpenIssues   int    `json:"open_issues,omitempty"`
	ClosedIssues int    `json:"closed_issues,omitempty"`
}

type MilestoneRef struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

type Issue struct {
	Number    int           `json:"number"`
	Title     string        `json:"title"`
	URL       string        `json:"url"`
	Labels    []Label       `json:"labels"`
	Milestone *MilestoneRef `json:"milestone,omitempty"`
	State     string        `json:"state,omitempty"`
	ClosedAt  string        `json:"closedAt,omitempty"`
}

type Section struct {
	Milestone *Milestone `json:"milestone,omitempty"`
	Title     string     `json:"title"`
	Issues    []Issue    `json:"issues"`
}

type Board struct {
	Repo      repo.Details `json:"repo"`
	Sections  []Section    `json:"sections"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type LoadOptions struct {
	DoneWindowDays int
	Now            time.Time
}

type Lane string

const (
	LaneBacklog    Lane = "Backlog"
	LaneInProgress Lane = "In Progress"
	LaneReview     Lane = "Review"
	LaneDone       Lane = "Done"

	LabelInProgress = "kanban:in-progress"
	LabelReview     = "kanban:review"

	DefaultDoneWindowDays = 14
)

type ghRunner func(ctx context.Context, args ...string) (string, error)

type Client struct {
	runner ghRunner
}

func NewClient() *Client {
	return &Client{runner: runGH}
}

func (c *Client) LoadBoard(ctx context.Context, details repo.Details) (Board, error) {
	return c.LoadBoardWithOptions(ctx, details, LoadOptions{})
}

func (c *Client) LoadBoardWithOptions(ctx context.Context, details repo.Details, options LoadOptions) (Board, error) {
	issues, err := c.listBoardIssues(ctx, details.Slug, options)
	if err != nil {
		return Board{}, err
	}

	sections := buildLaneSections(issues)
	return Board{
		Repo:      details,
		Sections:  sections,
		UpdatedAt: time.Now().UTC(),
	}, nil
}

func buildLaneSections(issues []Issue) []Section {
	sections := []Section{
		{Title: string(LaneBacklog)},
		{Title: string(LaneInProgress)},
		{Title: string(LaneReview)},
		{Title: string(LaneDone)},
	}
	byLane := map[Lane]*[]Issue{
		LaneBacklog:    &sections[0].Issues,
		LaneInProgress: &sections[1].Issues,
		LaneReview:     &sections[2].Issues,
		LaneDone:       &sections[3].Issues,
	}

	for _, issue := range issues {
		lane := issueLane(issue)
		*byLane[lane] = append(*byLane[lane], issue)
	}

	for i := range sections {
		sort.Slice(sections[i].Issues, func(a, b int) bool {
			return sections[i].Issues[a].Number > sections[i].Issues[b].Number
		})
	}
	return sections
}

func issueLane(issue Issue) Lane {
	if strings.EqualFold(issue.State, "closed") {
		return LaneDone
	}
	for _, label := range issue.Labels {
		switch label.Name {
		case LabelInProgress:
			return LaneInProgress
		case LabelReview:
			return LaneReview
		}
	}
	return LaneBacklog
}

func (c *Client) listBoardIssues(ctx context.Context, slug string, options LoadOptions) ([]Issue, error) {
	openIssues, err := c.listIssues(ctx, slug, "open", "")
	if err != nil {
		return nil, err
	}
	doneWindowDays := options.DoneWindowDays
	if doneWindowDays <= 0 {
		doneWindowDays = DefaultDoneWindowDays
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	closedSince := now.UTC().AddDate(0, 0, -doneWindowDays).Format("2006-01-02")
	closedIssues, err := c.listIssues(ctx, slug, "closed", "closed:>="+closedSince)
	if err != nil {
		return nil, err
	}
	return append(openIssues, closedIssues...), nil
}

func (c *Client) listIssues(ctx context.Context, slug string, state string, search string) ([]Issue, error) {
	args := []string{
		"issue", "list",
		"--repo", slug,
		"--state", state,
		"--limit", "500",
		"--json", "number,title,url,labels,milestone,state,closedAt",
	}
	if search != "" {
		args = append(args, "--search", search)
	}
	out, err := c.runner(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("load issues: %w", err)
	}
	var issues []Issue
	if err := json.Unmarshal([]byte(out), &issues); err != nil {
		return nil, fmt.Errorf("decode issues: %w", err)
	}
	return issues, nil
}

func (c *Client) MoveIssue(ctx context.Context, slug string, number int, lane Lane) error {
	switch lane {
	case LaneBacklog:
		return c.editIssueLabels(ctx, slug, number, nil, []string{LabelInProgress, LabelReview})
	case LaneInProgress:
		return c.editIssueLabels(ctx, slug, number, []string{LabelInProgress}, []string{LabelReview})
	case LaneReview:
		return c.editIssueLabels(ctx, slug, number, []string{LabelReview}, []string{LabelInProgress})
	case LaneDone:
		if err := c.editIssueLabels(ctx, slug, number, nil, []string{LabelInProgress, LabelReview}); err != nil {
			return err
		}
		if _, err := c.runner(ctx, "issue", "close", fmt.Sprint(number), "--repo", slug); err != nil {
			return fmt.Errorf("close issue #%d: %w", number, err)
		}
		return nil
	default:
		return fmt.Errorf("unknown lane %q", lane)
	}
}

func (c *Client) editIssueLabels(ctx context.Context, slug string, number int, addLabels []string, removeLabels []string) error {
	args := []string{"issue", "edit", fmt.Sprint(number), "--repo", slug}
	for _, label := range removeLabels {
		args = append(args, "--remove-label", label)
	}
	for _, label := range addLabels {
		args = append(args, "--add-label", label)
	}
	if _, err := c.runner(ctx, args...); err != nil {
		for _, label := range addLabels {
			if strings.Contains(strings.ToLower(err.Error()), "not found") {
				return fmt.Errorf("Label %q not found. Run: kanban init --setup-labels", label)
			}
		}
		return fmt.Errorf("edit issue #%d labels: %w", number, err)
	}
	return nil
}

func runGH(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", errors.New(message)
	}
	return string(out), nil
}
