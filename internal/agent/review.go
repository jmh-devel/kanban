package agent

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jmh-devel/kanban/internal/state"
)

type ReviewRequest struct {
	Repo   string
	Issue  int
	Runner string
}

type ReviewResult struct {
	Dispatch state.Dispatch `json:"dispatch"`
	Command  string         `json:"command"`
	Manual   bool           `json:"manual"`
}

type Reviewer struct {
	ExecCommand func(context.Context, string) error
	Now         func() time.Time
}

func NewReviewer() Reviewer {
	return Reviewer{
		ExecCommand: runShell,
		Now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (r Reviewer) Dispatch(ctx context.Context, config state.Config, request ReviewRequest) (ReviewResult, error) {
	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now().UTC()
	}
	result, err := RecordReviewDispatch(config, request, now)
	if err != nil {
		return ReviewResult{}, err
	}

	if result.Manual {
		return result, nil
	}

	if r.ExecCommand == nil {
		r.ExecCommand = runShell
	}
	if err := r.ExecCommand(ctx, result.Command); err != nil {
		if markErr := markDispatch(result.Dispatch, state.StatusFailed); markErr != nil {
			return ReviewResult{}, markErr
		}
		return ReviewResult{}, err
	}
	if err := markDispatch(result.Dispatch, state.StatusCompleted); err != nil {
		return ReviewResult{}, err
	}
	result.Dispatch.Status = state.StatusCompleted
	return result, nil
}

func RecordReviewDispatch(config state.Config, request ReviewRequest, now time.Time) (ReviewResult, error) {
	if strings.TrimSpace(request.Repo) == "" {
		return ReviewResult{}, errors.New("repo is required")
	}
	if request.Issue <= 0 {
		return ReviewResult{}, errors.New("issue must be greater than zero")
	}
	if request.Runner == "" {
		request.Runner = config.ReviewRunner()
	}
	command, err := BuildReviewCommand(config, request.Repo, request.Issue, request.Runner)
	if err != nil {
		return ReviewResult{}, err
	}
	dispatches, err := state.LoadDispatches()
	if err != nil {
		return ReviewResult{}, err
	}
	record := state.Dispatch{
		Repo:         request.Repo,
		Issue:        request.Issue,
		Type:         state.TypeReview,
		Runner:       request.Runner,
		Mode:         config.ReviewMode(),
		DispatchedAt: now.UTC(),
		Command:      command,
		Status:       state.StatusDispatched,
	}
	dispatches = state.AppendDispatch(dispatches, record, true)
	if err := state.SaveDispatches(dispatches); err != nil {
		return ReviewResult{}, err
	}
	return ReviewResult{
		Dispatch: record,
		Command:  command,
		Manual:   config.ReviewMode() == "manual" || request.Runner == state.ManualRunner,
	}, nil
}

func BuildReviewCommand(config state.Config, repo string, issue int, runnerName string) (string, error) {
	if runnerName == "" {
		runnerName = config.ReviewRunner()
	}
	runner := config.Runner(runnerName)
	if runnerName != state.ManualRunner && runner.Kind == "" {
		return "", fmt.Errorf("runner %q is not configured", runnerName)
	}
	prSearch := fmt.Sprintf("closes #%d OR fixes #%d OR resolves #%d", issue, issue, issue)
	findPR := shellJoin("gh", "pr", "list", "--repo", repo, "--state", "open", "--search", prSearch, "--json", "number", "--jq", ".[0].number")
	reviewCommand, err := reviewAgentCommand(config, repo, issue, runnerName)
	if err != nil {
		return "", err
	}
	parts := []string{
		"pr=$(" + findPR + ")",
		"test -n \"$pr\"",
		reviewCommand,
		"gh pr review \"$pr\" --repo " + shellJoin(repo) + " --approve",
	}
	if config.ReviewAutoMerge() {
		mergeArgs := []string{"gh", "pr", "merge", "$pr", "--repo", repo, "--squash"}
		if config.ReviewDeleteBranch() {
			mergeArgs = append(mergeArgs, "--delete-branch")
		}
		parts = append(parts, shellJoinWithRaw(mergeArgs, "$pr"))
		parts = append(parts, "gh issue edit "+strconv.Itoa(issue)+" --repo "+shellJoin(repo)+" --remove-label kanban:review")
		parts = append(parts, "gh issue close "+strconv.Itoa(issue)+" --repo "+shellJoin(repo))
	}
	return strings.Join(parts, " && "), nil
}

func reviewAgentCommand(config state.Config, repo string, issue int, runnerName string) (string, error) {
	if runnerName == state.ManualRunner {
		return "true", nil
	}
	runner := config.Runner(runnerName)
	switch runner.Kind {
	case "local_cli":
		command := runner.Command
		if command == "" {
			command = runnerName
		}
		return shellJoin(command, "--repo", repo, "--issue", strconv.Itoa(issue), "--mode", "review"), nil
	case "tsctl_dispatch":
		repoConfig := config.Repos[repo]
		if repoConfig.RepoKey == "" {
			return "", fmt.Errorf("repo %s has no repo_key; run: kanban config set-repo-key --repo-key NAME", repo)
		}
		args := []string{"tsctl", "agent", "dispatch", repoConfig.RepoKey, "--runner", runnerName, "--issue", strconv.Itoa(issue), "--mode", "review"}
		if reposFile := ResolveReposFile(repoConfig.ReposFile); reposFile != "" {
			args = append(args, "--repos-file", reposFile)
		}
		return shellJoin(args...), nil
	default:
		return "", fmt.Errorf("runner %q has unsupported kind %q", runnerName, runner.Kind)
	}
}

func markDispatch(target state.Dispatch, status string) error {
	dispatches, err := state.LoadDispatches()
	if err != nil {
		return err
	}
	changed := false
	for i := range dispatches {
		dispatch := dispatches[i]
		if dispatch.Repo == target.Repo && dispatch.Issue == target.Issue && dispatch.TypeName() == target.TypeName() && dispatch.DispatchedAt.Equal(target.DispatchedAt) {
			dispatches[i].Status = status
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return state.SaveDispatches(dispatches)
}

func shellJoinWithRaw(args []string, raw string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == raw {
			quoted = append(quoted, arg)
			continue
		}
		quoted = append(quoted, shellJoin(arg))
	}
	return strings.Join(quoted, " ")
}
