package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmh-devel/kanban/internal/state"
)

func TestBuildCommandTsctlRequiresRepoKey(t *testing.T) {
	config := state.Config{
		Repos: map[string]state.RepoConfig{},
		Runners: map[string]state.RunnerConfig{
			"tsctl": {Kind: "tsctl_dispatch"},
		},
	}

	if _, err := BuildCommand(config, "jmh-devel/example", 12, "tsctl", "implement"); err == nil {
		t.Fatal("expected missing repo key error")
	}

	config.Repos["jmh-devel/example"] = state.RepoConfig{RepoKey: "example"}
	command, err := BuildCommand(config, "jmh-devel/example", 12, "tsctl", "implement")
	if err != nil {
		t.Fatal(err)
	}
	want := "tsctl agent dispatch example --runner tsctl --issue 12 --mode implement"
	if command != want {
		t.Fatalf("command = %q, want %q", command, want)
	}
}

func TestManualDispatchDoesNotExecuteOrMove(t *testing.T) {
	t.Setenv("KANBAN_CONFIG_DIR", t.TempDir())
	config := state.Config{
		Repos:   map[string]state.RepoConfig{"jmh-devel/example": {RepoKey: "example"}},
		Runners: map[string]state.RunnerConfig{},
	}
	calledExec := false
	calledMove := false
	dispatcher := Dispatcher{
		ExecCommand: func(context.Context, string) error {
			calledExec = true
			return nil
		},
		MoveIssue: func(context.Context, string, int) error {
			calledMove = true
			return nil
		},
		Now: func() time.Time {
			return time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
		},
	}

	result, err := dispatcher.Dispatch(context.Background(), config, Request{
		Repo:   "jmh-devel/example",
		Issue:  12,
		Runner: state.DefaultRunner,
		Mode:   "implement",
	})
	if err != nil {
		t.Fatal(err)
	}
	if calledExec || calledMove {
		t.Fatalf("manual dispatch executed=%v moved=%v", calledExec, calledMove)
	}
	if !result.Manual || result.Moved {
		t.Fatalf("unexpected result: %+v", result)
	}
	dispatches, err := state.LoadDispatches()
	if err != nil {
		t.Fatal(err)
	}
	if len(dispatches) != 1 || dispatches[0].Issue != 12 {
		t.Fatalf("dispatches = %+v", dispatches)
	}
}

func TestDuplicateDispatchRequiresConfirmation(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("KANBAN_CONFIG_DIR", configDir)
	existing := state.Dispatch{
		Repo:         "jmh-devel/example",
		Issue:        12,
		Runner:       "claude",
		Mode:         "implement",
		DispatchedAt: time.Date(2026, 5, 5, 11, 0, 0, 0, time.UTC),
		Command:      "claude --repo jmh-devel/example --issue 12 --mode implement",
		Status:       state.StatusDispatched,
	}
	if err := state.SaveDispatches([]state.Dispatch{existing}); err != nil {
		t.Fatal(err)
	}
	config := state.Config{
		Repos: map[string]state.RepoConfig{},
		Runners: map[string]state.RunnerConfig{
			"claude": {Kind: "local_cli", Command: "claude"},
		},
	}
	dispatcher := Dispatcher{
		ExecCommand: func(context.Context, string) error {
			t.Fatal("should not execute before duplicate confirmation")
			return nil
		},
	}

	result, err := dispatcher.Dispatch(context.Background(), config, Request{
		Repo:   "jmh-devel/example",
		Issue:  12,
		Runner: "claude",
		Mode:   "implement",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Duplicate == nil || result.Duplicate.Runner != "claude" {
		t.Fatalf("duplicate = %+v", result.Duplicate)
	}

	executed := false
	moved := false
	dispatcher.ExecCommand = func(context.Context, string) error {
		executed = true
		return nil
	}
	dispatcher.MoveIssue = func(context.Context, string, int) error {
		moved = true
		return nil
	}
	dispatcher.Now = func() time.Time {
		return time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	}
	result, err = dispatcher.Dispatch(context.Background(), config, Request{
		Repo:             "jmh-devel/example",
		Issue:            12,
		Runner:           "claude",
		Mode:             "implement",
		ConfirmDuplicate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !executed || !moved || !result.Moved {
		t.Fatalf("executed=%v moved=%v result=%+v", executed, moved, result)
	}
	dispatches, err := state.LoadDispatches()
	if err != nil {
		t.Fatal(err)
	}
	if len(dispatches) != 2 {
		t.Fatalf("dispatches = %+v", dispatches)
	}
	if dispatches[0].Status != state.StatusSuperseded || dispatches[1].Status != state.StatusDispatched {
		t.Fatalf("dispatches = %+v", dispatches)
	}
	if _, err := os.Stat(filepath.Join(configDir, state.DispatchesFileName)); err != nil {
		t.Fatal(err)
	}
}
