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
	State     string        `json:"state,omitempty"`
	ClosedAt  string        `json:"closedAt,omitempty"`
	Labels    []Label       `json:"labels"`
	Milestone *MilestoneRef `json:"milestone,omitempty"`
}

type Section struct {
	Milestone *Milestone `json:"milestone,omitempty"`
	Title     string     `json:"title"`
	Issues    []Issue    `json:"issues"`
	Sections  []Section  `json:"sections,omitempty"`
}

type Board struct {
	Repo      repo.Details `json:"repo"`
	Sections  []Section    `json:"sections"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type Lane string

const (
	LaneBacklog    Lane = "backlog"
	LaneInProgress Lane = "in-progress"
	LaneReview     Lane = "review"
	LaneDone       Lane = "done"

	InProgressLabel = "kanban:in-progress"
	ReviewLabel     = "kanban:review"
	DefaultDoneDays  = 14
)

type BoardOptions struct {
	DoneWindowDays   int
	GroupByMilestone bool
}

type Client struct {
	runner ghRunner
}

type ghRunner func(ctx context.Context, args ...string) (string, error)

func NewClient() *Client {
	return &Client{runner: runGH}
}

func (c *Client) LoadBoard(ctx context.Context, details repo.Details) (Board, error) {
	return c.LoadBoardWithOptions(ctx, details, BoardOptions{})
}

func (c *Client) LoadBoardWithOptions(ctx context.Context, details repo.Details, options BoardOptions) (Board, error) {
	milestones, err := c.listMilestones(ctx, details.Slug)
	if err != nil {
		return Board{}, err
	}
	issues, err := c.listOpenIssues(ctx, details.Slug)
	if err != nil {
		return Board{}, err
	}
	doneIssues, err := c.listDoneIssues(ctx, details.Slug, doneWindow(options.DoneWindowDays))
	if err != nil {
		return Board{}, err
	}
	issues = append(issues, doneIssues...)

	sections := buildLaneSections(milestones, issues, options.GroupByMilestone)
	return Board{
		Repo:      details,
		Sections:  sections,
		UpdatedAt: time.Now().UTC(),
	}, nil
}

func (c *Client) MoveIssue(ctx context.Context, slug string, number int, lane Lane) error {
	if number <= 0 {
		return errors.New("issue number must be positive")
	}

	switch lane {
	case LaneBacklog:
		return c.editIssueLabels(ctx, slug, number, nil, []string{InProgressLabel, ReviewLabel})
	case LaneInProgress:
		return c.editIssueLabels(ctx, slug, number, []string{InProgressLabel}, []string{ReviewLabel})
	case LaneReview:
		return c.editIssueLabels(ctx, slug, number, []string{ReviewLabel}, []string{InProgressLabel})
	case LaneDone:
		if err := c.editIssueLabels(ctx, slug, number, nil, []string{InProgressLabel, ReviewLabel}); err != nil {
			return err
		}
		_, err := c.run(ctx, "issue", "close", fmt.Sprint(number), "--repo", slug)
		if err != nil {
			return fmt.Errorf("close issue: %w", err)
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
	_, err := c.run(ctx, args...)
	if err != nil {
		return laneLabelError(err)
	}
	return nil
}

func laneLabelError(err error) error {
	message := err.Error()
	for _, label := range []string{InProgressLabel, ReviewLabel} {
		if strings.Contains(message, label) && strings.Contains(strings.ToLower(message), "not found") {
			return fmt.Errorf("Label %q not found. Run: kanban init --setup-labels", label)
		}
	}
	return err
}

func buildLaneSections(milestones []Milestone, issues []Issue, groupByMilestone bool) []Section {
	laneIssues := map[Lane][]Issue{
		LaneBacklog:    nil,
		LaneInProgress: nil,
		LaneReview:     nil,
		LaneDone:       nil,
	}
	for _, issue := range issues {
		lane := issueLane(issue)
		laneIssues[lane] = append(laneIssues[lane], issue)
	}

	order := []struct {
		lane  Lane
		title string
	}{
		{LaneBacklog, "Backlog"},
		{LaneInProgress, "In Progress"},
		{LaneReview, "Review"},
		{LaneDone, "Done"},
	}
	sections := make([]Section, 0, len(order))
	for _, item := range order {
		issuesForLane := append([]Issue(nil), laneIssues[item.lane]...)
		sortIssuesDescending(issuesForLane)
		section := Section{Title: item.title, Issues: issuesForLane}
		if groupByMilestone {
			section.Sections = buildMilestoneGroups(milestones, issuesForLane)
		}
		sections = append(sections, section)
	}
	return sections
}

func buildMilestoneGroups(milestones []Milestone, issues []Issue) []Section {
	byMilestone := make(map[string][]Issue, len(milestones))
	var unscheduled []Issue
	for _, issue := range issues {
		if issue.Milestone == nil || strings.TrimSpace(issue.Milestone.Title) == "" {
			unscheduled = append(unscheduled, issue)
			continue
		}
		byMilestone[issue.Milestone.Title] = append(byMilestone[issue.Milestone.Title], issue)
	}

	groups := make([]Section, 0, len(byMilestone)+1)
	for _, milestone := range milestones {
		issuesForMilestone := append([]Issue(nil), byMilestone[milestone.Title]...)
		if len(issuesForMilestone) == 0 {
			continue
		}
		sortIssuesDescending(issuesForMilestone)
		milestoneCopy := milestone
		groups = append(groups, Section{
			Milestone: &milestoneCopy,
			Title:     milestone.Title,
			Issues:    issuesForMilestone,
		})
		delete(byMilestone, milestone.Title)
	}

	leftoverTitles := make([]string, 0, len(byMilestone))
	for title := range byMilestone {
		leftoverTitles = append(leftoverTitles, title)
	}
	sort.Strings(leftoverTitles)
	for _, title := range leftoverTitles {
		issuesForMilestone := append([]Issue(nil), byMilestone[title]...)
		sortIssuesDescending(issuesForMilestone)
		groups = append(groups, Section{Title: title, Issues: issuesForMilestone})
	}

	if len(unscheduled) > 0 {
		sortIssuesDescending(unscheduled)
		groups = append(groups, Section{Title: "Unscheduled", Issues: unscheduled})
	}
	return groups
}

func sortIssuesDescending(issues []Issue) {
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].Number > issues[j].Number
	})
}

func issueLane(issue Issue) Lane {
	if strings.EqualFold(issue.State, "closed") || strings.TrimSpace(issue.ClosedAt) != "" {
		return LaneDone
	}
	if hasLabel(issue, ReviewLabel) {
		return LaneReview
	}
	if hasLabel(issue, InProgressLabel) {
		return LaneInProgress
	}
	return LaneBacklog
}

func hasLabel(issue Issue, name string) bool {
	for _, label := range issue.Labels {
		if label.Name == name {
			return true
		}
	}
	return false
}

func (c *Client) listMilestones(ctx context.Context, slug string) ([]Milestone, error) {
	path := fmt.Sprintf("repos/%s/milestones?state=open&per_page=100", slug)
	out, err := c.run(ctx, "api", path)
	if err != nil {
		return nil, fmt.Errorf("load milestones: %w", err)
	}
	var milestones []Milestone
	if err := json.Unmarshal([]byte(out), &milestones); err != nil {
		return nil, fmt.Errorf("decode milestones: %w", err)
	}
	return milestones, nil
}

func (c *Client) listOpenIssues(ctx context.Context, slug string) ([]Issue, error) {
	out, err := c.run(ctx,
		"issue", "list",
		"--repo", slug,
		"--state", "open",
		"--limit", "500",
		"--json", "number,title,url,state,labels,milestone",
	)
	if err != nil {
		return nil, fmt.Errorf("load issues: %w", err)
	}
	var issues []Issue
	if err := json.Unmarshal([]byte(out), &issues); err != nil {
		return nil, fmt.Errorf("decode issues: %w", err)
	}
	return issues, nil
}

func (c *Client) listDoneIssues(ctx context.Context, slug string, window time.Duration) ([]Issue, error) {
	since := time.Now().UTC().Add(-window).Format("2006-01-02")
	search := fmt.Sprintf("repo:%s is:issue is:closed closed:>=%s", slug, since)
	out, err := c.run(ctx,
		"issue", "list",
		"--repo", slug,
		"--state", "closed",
		"--search", search,
		"--limit", "500",
		"--json", "number,title,url,state,closedAt,labels,milestone",
	)
	if err != nil {
		return nil, fmt.Errorf("load done issues: %w", err)
	}
	var issues []Issue
	if err := json.Unmarshal([]byte(out), &issues); err != nil {
		return nil, fmt.Errorf("decode done issues: %w", err)
	}
	return issues, nil
}

func doneWindow(days int) time.Duration {
	if days <= 0 {
		days = DefaultDoneDays
	}
	return time.Duration(days) * 24 * time.Hour
}

func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	if c.runner == nil {
		c.runner = runGH
	}
	return c.runner(ctx, args...)
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
