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

type Assignee struct {
	Login string `json:"login"`
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
	Body      string        `json:"body,omitempty"`
	URL       string        `json:"url"`
	State     string        `json:"state,omitempty"`
	ClosedAt  string        `json:"closedAt,omitempty"`
	Labels    []Label       `json:"labels"`
	Milestone *MilestoneRef `json:"milestone,omitempty"`
	Assignees []Assignee    `json:"assignees,omitempty"`
}

type Section struct {
	Milestone *Milestone `json:"milestone,omitempty"`
	Title     string     `json:"title"`
	Issues    []Issue    `json:"issues"`
}

type Board struct {
	Repo         repo.Details `json:"repo"`
	Sections     []Section    `json:"sections"`
	ClosedIssues []Issue      `json:"closed_issues,omitempty"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

type Client struct{}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) LoadBoard(ctx context.Context, details repo.Details) (Board, error) {
	milestones, err := c.listMilestones(ctx, details.Slug)
	if err != nil {
		return Board{}, err
	}
	issues, err := c.listIssues(ctx, details.Slug)
	if err != nil {
		return Board{}, err
	}
	closedIssues, err := c.listRecentlyClosedIssues(ctx, details.Slug, 14)
	if err != nil {
		return Board{}, err
	}

	sections := buildSections(milestones, issues)
	return Board{
		Repo:         details,
		Sections:     sections,
		ClosedIssues: closedIssues,
		UpdatedAt:    time.Now().UTC(),
	}, nil
}

func buildSections(milestones []Milestone, issues []Issue) []Section {
	sections := make([]Section, 0, len(milestones)+1)
	byMilestone := make(map[string][]Issue, len(milestones))
	var unscheduled []Issue

	for _, issue := range issues {
		if issue.Milestone == nil || strings.TrimSpace(issue.Milestone.Title) == "" {
			unscheduled = append(unscheduled, issue)
			continue
		}
		byMilestone[issue.Milestone.Title] = append(byMilestone[issue.Milestone.Title], issue)
	}

	for _, milestone := range milestones {
		milestoneCopy := milestone
		issuesForMilestone := append([]Issue(nil), byMilestone[milestone.Title]...)
		sort.Slice(issuesForMilestone, func(i, j int) bool {
			return issuesForMilestone[i].Number < issuesForMilestone[j].Number
		})
		sections = append(sections, Section{
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
		sort.Slice(issuesForMilestone, func(i, j int) bool {
			return issuesForMilestone[i].Number < issuesForMilestone[j].Number
		})
		sections = append(sections, Section{Title: title, Issues: issuesForMilestone})
	}

	sort.Slice(unscheduled, func(i, j int) bool {
		return unscheduled[i].Number < unscheduled[j].Number
	})
	sections = append(sections, Section{Title: "Unscheduled", Issues: unscheduled})
	return sections
}

func (c *Client) listMilestones(ctx context.Context, slug string) ([]Milestone, error) {
	path := fmt.Sprintf("repos/%s/milestones?state=open&per_page=100", slug)
	out, err := runGH(ctx, "api", path)
	if err != nil {
		return nil, fmt.Errorf("load milestones: %w", err)
	}
	var milestones []Milestone
	if err := json.Unmarshal([]byte(out), &milestones); err != nil {
		return nil, fmt.Errorf("decode milestones: %w", err)
	}
	return milestones, nil
}

func (c *Client) listIssues(ctx context.Context, slug string) ([]Issue, error) {
	out, err := runGH(ctx,
		"issue", "list",
		"--repo", slug,
		"--state", "open",
		"--limit", "500",
		"--json", "number,title,body,url,state,labels,milestone,assignees",
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

func (c *Client) listRecentlyClosedIssues(ctx context.Context, slug string, days int) ([]Issue, error) {
	out, err := runGH(ctx,
		"issue", "list",
		"--repo", slug,
		"--state", "closed",
		"--limit", "500",
		"--json", "number,title,body,url,state,closedAt,labels,milestone,assignees",
	)
	if err != nil {
		return nil, fmt.Errorf("load closed issues: %w", err)
	}
	var issues []Issue
	if err := json.Unmarshal([]byte(out), &issues); err != nil {
		return nil, fmt.Errorf("decode closed issues: %w", err)
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	recent := make([]Issue, 0, len(issues))
	for _, issue := range issues {
		closedAt, err := time.Parse(time.RFC3339, issue.ClosedAt)
		if err != nil || closedAt.Before(cutoff) {
			continue
		}
		recent = append(recent, issue)
	}
	sort.Slice(recent, func(i, j int) bool {
		return recent[i].Number > recent[j].Number
	})
	return recent, nil
}

func (c *Client) MoveIssue(ctx context.Context, slug string, issue Issue, lane string) error {
	currentLabels := make(map[string]bool, len(issue.Labels))
	for _, label := range issue.Labels {
		currentLabels[label.Name] = true
	}

	if strings.EqualFold(issue.State, "closed") && lane != "Done" {
		if _, err := runGH(ctx, "issue", "reopen", fmt.Sprint(issue.Number), "--repo", slug); err != nil {
			return fmt.Errorf("reopen issue: %w", err)
		}
	}

	for _, label := range []string{"kanban:in-progress", "kanban:review"} {
		if currentLabels[label] {
			if _, err := runGH(ctx, "issue", "edit", fmt.Sprint(issue.Number), "--repo", slug, "--remove-label", label); err != nil {
				return fmt.Errorf("remove label %q: %w", label, err)
			}
		}
	}

	switch lane {
	case "Backlog":
		return nil
	case "In Progress":
		if _, err := runGH(ctx, "issue", "edit", fmt.Sprint(issue.Number), "--repo", slug, "--add-label", "kanban:in-progress"); err != nil {
			return fmt.Errorf("add label 'kanban:in-progress' (run: kanban init --setup-labels): %w", err)
		}
	case "Review":
		if _, err := runGH(ctx, "issue", "edit", fmt.Sprint(issue.Number), "--repo", slug, "--add-label", "kanban:review"); err != nil {
			return fmt.Errorf("add label 'kanban:review' (run: kanban init --setup-labels): %w", err)
		}
	case "Done":
		if _, err := runGH(ctx, "issue", "close", fmt.Sprint(issue.Number), "--repo", slug); err != nil {
			return fmt.Errorf("close issue: %w", err)
		}
	default:
		return fmt.Errorf("unknown lane %q", lane)
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
