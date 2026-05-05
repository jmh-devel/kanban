package initcmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeResult struct {
	output string
	err    error
}

type fakeRunner struct {
	responses map[string]fakeResult
	calls     []string
}

func (f *fakeRunner) run(_ context.Context, name string, args ...string) (string, error) {
	call := strings.TrimSpace(name + " " + strings.Join(args, " "))
	f.calls = append(f.calls, call)
	if result, ok := f.responses[call]; ok {
		return result.output, result.err
	}
	return "", errors.New("unexpected command: " + call)
}

func TestRunWithRunnerDryRun(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResult{
		"git -C . rev-parse --show-toplevel": {output: "/tmp/kanban\n"},
		"git -C /tmp/kanban remote":          {output: "\n"},
	}}

	var output bytes.Buffer
	err := runWithRunner(context.Background(), Options{
		Path:       ".",
		Owner:      "jmh-devel",
		Remote:     "origin",
		Visibility: "public",
		Stdout:     &output,
	}, runner.run)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	got := output.String()
	if !strings.Contains(got, "Publish command: gh repo create jmh-devel/kanban --source /tmp/kanban --remote origin --public") {
		t.Fatalf("missing publish command in output: %q", got)
	}
	if !strings.Contains(got, "Dry-run only") {
		t.Fatalf("missing dry-run hint in output: %q", got)
	}
}

func TestRunWithRunnerRemoteExists(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResult{
		"git -C . rev-parse --show-toplevel":       {output: "/tmp/kanban\n"},
		"git -C /tmp/kanban remote":                {output: "origin\nupstream\n"},
		"git -C /tmp/kanban remote get-url origin": {output: "git@github.com:jmh-devel/kanban.git\n"},
	}}

	var output bytes.Buffer
	err := runWithRunner(context.Background(), Options{
		Path:       ".",
		Owner:      "jmh-devel",
		Remote:     "origin",
		Visibility: "public",
		Stdout:     &output,
	}, runner.run)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	got := output.String()
	if !strings.Contains(got, "Remote \"origin\" already exists") {
		t.Fatalf("missing existing-remote message: %q", got)
	}
	if strings.Contains(got, "Publish command:") {
		t.Fatalf("should not print publish command when remote exists: %q", got)
	}
}

func TestRunWithRunnerApply(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResult{
		"git -C . rev-parse --show-toplevel": {output: "/tmp/kanban\n"},
		"git -C /tmp/kanban remote":          {output: "\n"},
		"gh repo create jmh-devel/kanban --source /tmp/kanban --remote origin --public --push": {output: "created\n"},
	}}

	var output bytes.Buffer
	err := runWithRunner(context.Background(), Options{
		Path:       ".",
		Owner:      "jmh-devel",
		Remote:     "origin",
		Visibility: "public",
		Apply:      true,
		Stdout:     &output,
	}, runner.run)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if len(runner.calls) != 3 {
		t.Fatalf("expected 3 command calls, got %d (%v)", len(runner.calls), runner.calls)
	}
	if !strings.Contains(output.String(), "Publish completed.") {
		t.Fatalf("missing completion output: %q", output.String())
	}
}

func TestRunWithRunnerSetupLabelsCreatesMissingLabels(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResult{
		"git -C . rev-parse --show-toplevel":       {output: "/tmp/kanban\n"},
		"git -C /tmp/kanban remote":                {output: "origin\n"},
		"git -C /tmp/kanban remote get-url origin": {output: "git@github.com:jmh-devel/kanban.git\n"},
		"gh label list --repo jmh-devel/kanban --json name": {output: `[{"name":"kanban:review"}]`},
		"gh label create kanban:in-progress --repo jmh-devel/kanban --color 0075ca --description Kanban lane: in progress": {output: "created\n"},
	}}

	var output bytes.Buffer
	err := runWithRunner(context.Background(), Options{
		Path:        ".",
		Remote:      "origin",
		Visibility:  "public",
		SetupLabels: true,
		Stdout:      &output,
	}, runner.run)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	got := output.String()
	if !strings.Contains(got, "Created label \"kanban:in-progress\".") {
		t.Fatalf("missing created-label output: %q", got)
	}
	if !strings.Contains(got, "Label \"kanban:review\" already exists.") {
		t.Fatalf("missing existing-label output: %q", got)
	}
}
