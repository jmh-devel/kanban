package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jmh-devel/kanban/internal/state"
)

type Request struct {
	Repo             string
	Issue            int
	Runner           string
	Mode             string
	ConfirmDuplicate bool
}

type Result struct {
	Dispatch  state.Dispatch `json:"dispatch"`
	Command   string         `json:"command"`
	Manual    bool           `json:"manual"`
	Duplicate *Duplicate     `json:"duplicate,omitempty"`
	Moved     bool           `json:"moved"`
}

type Duplicate struct {
	Runner       string    `json:"runner"`
	Mode         string    `json:"mode"`
	DispatchedAt time.Time `json:"dispatched_at"`
}

type Dispatcher struct {
	ExecCommand func(context.Context, string) error
	MoveIssue   func(context.Context, string, int) error
	Now         func() time.Time
}

func NewDispatcher() Dispatcher {
	return Dispatcher{
		ExecCommand: runShell,
		MoveIssue:   moveIssueToInProgress,
		Now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func Preview(config state.Config, repo string, issue int, runnerName string, mode string) (string, error) {
	runnerName, mode = defaults(config, repo, runnerName, mode)
	return BuildCommand(config, repo, issue, runnerName, mode)
}

func (d Dispatcher) Dispatch(ctx context.Context, config state.Config, request Request) (Result, error) {
	if request.Issue <= 0 {
		return Result{}, errors.New("issue must be greater than zero")
	}
	request.Runner, request.Mode = defaults(config, request.Repo, request.Runner, request.Mode)
	command, err := BuildCommand(config, request.Repo, request.Issue, request.Runner, request.Mode)
	if err != nil {
		return Result{}, err
	}

	dispatches, err := state.LoadDispatches()
	if err != nil {
		return Result{}, err
	}
	if existing, ok := state.ActiveDispatch(dispatches, request.Repo, request.Issue); ok && !request.ConfirmDuplicate {
		return Result{
			Command: command,
			Manual:  request.Runner == state.DefaultRunner,
			Duplicate: &Duplicate{
				Runner:       existing.Runner,
				Mode:         existing.Mode,
				DispatchedAt: existing.DispatchedAt,
			},
		}, nil
	}

	manual := request.Runner == state.DefaultRunner
	if !manual {
		if d.ExecCommand == nil {
			d.ExecCommand = runShell
		}
		if err := d.ExecCommand(ctx, command); err != nil {
			return Result{}, err
		}
	}

	moved := false
	if !manual && config.AutoMoveOnDispatch() {
		if d.MoveIssue == nil {
			d.MoveIssue = moveIssueToInProgress
		}
		if err := d.MoveIssue(ctx, request.Repo, request.Issue); err != nil {
			return Result{}, err
		}
		moved = true
	}

	now := time.Now().UTC()
	if d.Now != nil {
		now = d.Now().UTC()
	}
	record := state.Dispatch{
		Repo:         request.Repo,
		Issue:        request.Issue,
		Runner:       request.Runner,
		Mode:         request.Mode,
		DispatchedAt: now,
		Command:      command,
		Status:       state.StatusDispatched,
	}
	dispatches = state.AppendDispatch(dispatches, record, request.ConfirmDuplicate)
	if err := state.SaveDispatches(dispatches); err != nil {
		return Result{}, err
	}
	return Result{Dispatch: record, Command: command, Manual: manual, Moved: moved}, nil
}

func BuildCommand(config state.Config, repo string, issue int, runnerName string, mode string) (string, error) {
	runner := config.Runner(runnerName)
	if runnerName != state.DefaultRunner && runner.Kind == "" {
		return "", fmt.Errorf("runner %q is not configured", runnerName)
	}
	repoConfig := config.Repos[repo]
	reposFile := ResolveReposFile(repoConfig.ReposFile)

	switch runner.Kind {
	case state.DefaultRunner:
		target := repoConfig.RepoKey
		if target == "" {
			target = repo
		}
		args := []string{"tsctl", "agent", "dispatch", target, "--runner", runnerName, "--issue", strconv.Itoa(issue), "--mode", mode}
		if reposFile != "" {
			args = append(args, "--repos-file", reposFile)
		}
		return shellJoin(args...), nil
	case "tsctl_dispatch":
		if repoConfig.RepoKey == "" {
			return "", fmt.Errorf("repo %s has no repo_key; run: kanban config set-repo-key --repo-key NAME", repo)
		}
		args := []string{"tsctl", "agent", "dispatch", repoConfig.RepoKey, "--runner", runnerName, "--issue", strconv.Itoa(issue), "--mode", mode}
		if reposFile != "" {
			args = append(args, "--repos-file", reposFile)
		}
		return shellJoin(args...), nil
	case "local_cli":
		command := runner.Command
		if command == "" {
			command = runnerName
		}
		return shellJoin(command, "--repo", repo, "--issue", strconv.Itoa(issue), "--mode", mode), nil
	default:
		return "", fmt.Errorf("runner %q has unsupported kind %q", runnerName, runner.Kind)
	}
}

func defaults(config state.Config, repo string, runnerName string, mode string) (string, string) {
	repoConfig := config.Repos[repo]
	if runnerName == "" {
		runnerName = repoConfig.PreferredRunner
	}
	if runnerName == "" {
		runnerName = state.DefaultRunner
	}
	if mode == "" {
		mode = repoConfig.PreferredMode
	}
	if mode == "" {
		mode = state.DefaultMode
	}
	return runnerName, mode
}

func runShell(ctx context.Context, command string) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return errors.New(message)
	}
	return nil
}

func moveIssueToInProgress(ctx context.Context, repo string, issue int) error {
	args := []string{"issue", "edit", strconv.Itoa(issue), "--repo", repo, "--remove-label", "kanban:review", "--add-label", "kanban:in-progress"}
	cmd := exec.CommandContext(ctx, "gh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("move issue to In Progress: %w", errors.New(message))
	}
	return nil
}

func ResolveReposFile(configured string) string {
	if value := strings.TrimSpace(configured); value != "" {
		if fileExists(value) {
			return value
		}
	}
	if value := strings.TrimSpace(os.Getenv("KANBAN_REPOS_FILE")); value != "" {
		if fileExists(value) {
			return value
		}
	}
	if value := strings.TrimSpace(os.Getenv("REPOS_FILE")); value != "" {
		if fileExists(value) {
			return value
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	candidates := []string{
		filepath.Join(cwd, "repos.yaml"),
		filepath.Join(cwd, "..", "tsctl", "repos.yaml"),
	}
	for _, path := range candidates {
		if fileExists(path) {
			return path
		}
	}
	for parent := cwd; parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
		candidate := filepath.Join(parent, "tsctl", "repos.yaml")
		if fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func shellJoin(parts ...string) string {
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, shellQuote(part))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.ContainsAny(value, " \t\n'\"\\$`!*?[]{}();&|<>") {
		return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
	}
	return value
}
