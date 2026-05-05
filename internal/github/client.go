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
	"github.com/jmh-devel/kanban/internal/state"
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
	Agent     *AgentStatus  `json:"agent,omitempty"`
}

type AgentStatus struct {
	Runner       string    `json:"runner"`
	Mode         string    `json:"mode"`
	Status       string    `json:"status"`
	DispatchedAt time.Time `json:"dispatched_at"`
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
	dispatches, err := state.LoadDispatches()
	if err != nil {
		return Board{}, err
	}
	attachDispatches(issues, state.ActiveDispatchesByIssue(dispatches, details.Slug))

	sections := buildSections(milestones, issues)
	return Board{
		Repo:      details,
		Sections:  sections,
		UpdatedAt: time.Now().UTC(),
	}, nil
}

func attachDispatches(issues []Issue, dispatches map[int]state.Dispatch) {
	for i := range issues {
		dispatch, ok := dispatches[issues[i].Number]
		if !ok {
			continue
		}
		issues[i].Agent = &AgentStatus{
			Runner:       dispatch.Runner,
			Mode:         dispatch.Mode,
			Status:       dispatch.Status,
			DispatchedAt: dispatch.DispatchedAt,
		}
	}
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
		"--json", "number,title,url,labels,milestone",
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
