package initcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jmh-devel/kanban/internal/repo"
)

type Options struct {
	Path        string
	Owner       string
	Name        string
	Remote      string
	Visibility  string
	Apply       bool
	SetupLabels bool
	Stdout      io.Writer
}

type commandRunner func(ctx context.Context, name string, args ...string) (string, error)

func Run(ctx context.Context, options Options) error {
	return runWithRunner(ctx, options, defaultRunner)
}

func runWithRunner(ctx context.Context, options Options, runner commandRunner) error {
	if options.Stdout == nil {
		options.Stdout = io.Discard
	}
	if strings.TrimSpace(options.Path) == "" {
		options.Path = "."
	}
	if strings.TrimSpace(options.Remote) == "" {
		return errors.New("remote is required")
	}
	if options.Visibility == "" {
		options.Visibility = "public"
	}
	if options.Visibility != "public" && options.Visibility != "private" {
		return fmt.Errorf("invalid visibility %q, expected public or private", options.Visibility)
	}

	root, err := runner(ctx, "git", "-C", options.Path, "rev-parse", "--show-toplevel")
	if err != nil {
		// Fall back to the given path when git root detection fails (e.g. cross-mount
		// boundaries or a not-yet-initialized repository). An explicit --name is
		// required in that case because we cannot derive a name from the root.
		absPath, absErr := filepath.Abs(options.Path)
		if absErr != nil {
			return fmt.Errorf("detect git root: %w", err)
		}
		root = absPath
	}
	root = strings.TrimSpace(root)

	name := strings.TrimSpace(options.Name)
	if name == "" {
		name = filepath.Base(root)
	}
	if name == "" || name == "." || name == "/" {
		return errors.New("unable to determine repository name")
	}

	owner := strings.TrimSpace(options.Owner)
	if owner == "" {
		remoteURL, remoteErr := runner(ctx, "git", "-C", root, "remote", "get-url", options.Remote)
		if remoteErr == nil {
			details, parseErr := repo.ParseRemoteURL(strings.TrimSpace(remoteURL))
			if parseErr == nil {
				owner = details.Owner
			}
		}
	}
	if owner == "" {
		return errors.New("owner is required (use --owner, or ensure git remote points to github.com)")
	}

	slug := owner + "/" + name
	remotesOutput, err := runner(ctx, "git", "-C", root, "remote")
	if err != nil {
		// Not a git repo yet — skip remote check and fall through to publish.
		remotesOutput = ""
	} else {
		for _, remote := range strings.Fields(remotesOutput) {
			if remote == options.Remote {
				remoteURL, err := runner(ctx, "git", "-C", root, "remote", "get-url", options.Remote)
				if err != nil {
					return fmt.Errorf("read remote %q: %w", options.Remote, err)
				}
				_, _ = fmt.Fprintf(options.Stdout, "Remote %q already exists: %s\n", options.Remote, strings.TrimSpace(remoteURL))
				_, _ = fmt.Fprintln(options.Stdout, "No publish action required.")
				if options.SetupLabels {
					if err := setupLaneLabels(ctx, runner, slug, options.Stdout); err != nil {
						return err
					}
				}
				return nil
			}
		}
	}
	_ = remotesOutput

	args := []string{
		"repo", "create", slug,
		"--source", root,
		"--remote", options.Remote,
		"--" + options.Visibility,
	}
	if options.Apply {
		args = append(args, "--push")
	}

	_, _ = fmt.Fprintf(options.Stdout, "Publish command: gh %s\n", strings.Join(args, " "))
	if !options.Apply {
		_, _ = fmt.Fprintln(options.Stdout, "Dry-run only. Re-run with --apply to execute.")
		if options.SetupLabels {
			if err := setupLaneLabels(ctx, runner, slug, options.Stdout); err != nil {
				return err
			}
		}
		return nil
	}

	_, err = runner(ctx, "gh", args...)
	if err != nil {
		return fmt.Errorf("publish repository: %w", err)
	}
	_, _ = fmt.Fprintln(options.Stdout, "Publish completed.")
	if options.SetupLabels {
		if err := setupLaneLabels(ctx, runner, slug, options.Stdout); err != nil {
			return err
		}
	}
	return nil
}

type laneLabel struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

var laneLabels = []laneLabel{
	{Name: "kanban:in-progress", Color: "0075ca", Description: "Kanban lane: In Progress"},
	{Name: "kanban:review", Color: "e4e669", Description: "Kanban lane: Review"},
}

// EnsureLaneLabels checks that the kanban lane labels exist in the given repo
// (owner/name slug) and creates any that are missing. It writes a one-line
// progress note to stdout for each label it inspects. Errors are returned so
// callers can decide whether to treat them as fatal or just log a warning.
func EnsureLaneLabels(ctx context.Context, slug string, stdout io.Writer) error {
	return setupLaneLabels(ctx, defaultRunner, slug, stdout)
}

func setupLaneLabels(ctx context.Context, runner commandRunner, slug string, stdout io.Writer) error {
	out, err := runner(ctx, "gh", "label", "list", "--repo", slug, "--limit", "1000", "--json", "name")
	if err != nil {
		return fmt.Errorf("list labels: %w", err)
	}
	var existing []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(out), &existing); err != nil {
		return fmt.Errorf("decode labels: %w", err)
	}
	existingNames := make(map[string]bool, len(existing))
	for _, label := range existing {
		existingNames[label.Name] = true
	}

	for _, label := range laneLabels {
		if existingNames[label.Name] {
			_, _ = fmt.Fprintf(stdout, "Label %q already exists.\n", label.Name)
			continue
		}
		_, err := runner(ctx,
			"gh", "label", "create", label.Name,
			"--repo", slug,
			"--color", label.Color,
			"--description", label.Description,
		)
		if err != nil {
			return fmt.Errorf("create label %q: %w", label.Name, err)
		}
		_, _ = fmt.Fprintf(stdout, "Created label %q.\n", label.Name)
	}
	return nil
}

func defaultRunner(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
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
