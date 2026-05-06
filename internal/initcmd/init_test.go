package initcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmh-devel/kanban/internal/state"
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

func TestRunWithRunnerSetupLabels(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResult{
		"git -C . rev-parse --show-toplevel":                             {output: "/tmp/kanban\n"},
		"git -C /tmp/kanban remote":                                      {output: "origin\n"},
		"git -C /tmp/kanban remote get-url origin":                       {output: "git@github.com:jmh-devel/kanban.git\n"},
		"gh label list --repo jmh-devel/kanban --limit 1000 --json name": {output: `[{"name":"kanban:review"}]`},
		"gh label create kanban:in-progress --repo jmh-devel/kanban --color 0075ca --description Kanban lane: In Progress": {output: ""},
	}}

	var output bytes.Buffer
	err := runWithRunner(context.Background(), Options{
		Path:        ".",
		Owner:       "jmh-devel",
		Remote:      "origin",
		Visibility:  "public",
		SetupLabels: true,
		Stdout:      &output,
	}, runner.run)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	got := output.String()
	if !strings.Contains(got, "Created label \"kanban:in-progress\"") {
		t.Fatalf("missing created-label output: %q", got)
	}
	if !strings.Contains(got, "Label \"kanban:review\" already exists") {
		t.Fatalf("missing existing-label output: %q", got)
	}
}

func TestRunWithRunnerSetupLabelsCreatesBothWhenMissing(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResult{
		"git -C . rev-parse --show-toplevel":                             {output: "/tmp/kanban\n"},
		"git -C /tmp/kanban remote":                                      {output: "\n"},
		"gh label list --repo jmh-devel/kanban --limit 1000 --json name": {output: `[]`},
		"gh label create kanban:in-progress --repo jmh-devel/kanban --color 0075ca --description Kanban lane: In Progress": {output: ""},
		"gh label create kanban:review --repo jmh-devel/kanban --color e4e669 --description Kanban lane: Review":           {output: ""},
	}}

	var output bytes.Buffer
	err := runWithRunner(context.Background(), Options{
		Path:        ".",
		Owner:       "jmh-devel",
		Remote:      "origin",
		Visibility:  "public",
		SetupLabels: true,
		Stdout:      &output,
	}, runner.run)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	got := output.String()
	for _, label := range []string{"kanban:in-progress", "kanban:review"} {
		if !strings.Contains(got, "Created label \""+label+"\"") {
			t.Fatalf("missing created-label output for %s: %q", label, got)
		}
	}
}

func TestEnsureRepoKeyAutoDiscoversFromTSCTLReposFile(t *testing.T) {
	configDir := t.TempDir()
	reposFile := filepath.Join(t.TempDir(), "repos.yaml")
	t.Setenv("KANBAN_CONFIG_DIR", configDir)
	t.Setenv("TSCTL_REPOS_FILE", reposFile)

	if err := os.WriteFile(reposFile, []byte(`
repos:
  - key: homeringai
    github: jmh-devel/homeringai
`), 0o600); err != nil {
		t.Fatalf("write repos file: %v", err)
	}

	var output bytes.Buffer
	key, err := EnsureRepoKey(context.Background(), "jmh-devel/homeringai", &output)
	if err != nil {
		t.Fatalf("EnsureRepoKey returned error: %v", err)
	}
	if key != "homeringai" {
		t.Fatalf("key = %q, want homeringai", key)
	}
	if !strings.Contains(output.String(), "kanban: auto-set repo key for jmh-devel/homeringai → homeringai") {
		t.Fatalf("missing auto-set output: %q", output.String())
	}

	config, err := state.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	repoConfig := config.Repos["jmh-devel/homeringai"]
	if repoConfig.RepoKey != "homeringai" {
		t.Fatalf("saved repo_key = %q, want homeringai", repoConfig.RepoKey)
	}
	if repoConfig.ReposFile != reposFile {
		t.Fatalf("saved repos_file = %q, want %q", repoConfig.ReposFile, reposFile)
	}
}

func TestEnsureRepoKeyLeavesExistingConfigUnchanged(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("KANBAN_CONFIG_DIR", configDir)
	t.Setenv("TSCTL_REPOS_FILE", filepath.Join(t.TempDir(), "missing.yaml"))

	config := state.DefaultConfig()
	config.Repos["jmh-devel/example"] = state.RepoConfig{RepoKey: "existing"}
	if err := state.SaveConfig(config); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	var output bytes.Buffer
	key, err := EnsureRepoKey(context.Background(), "jmh-devel/example", &output)
	if err != nil {
		t.Fatalf("EnsureRepoKey returned error: %v", err)
	}
	if key != "existing" {
		t.Fatalf("key = %q, want existing", key)
	}
	if output.Len() != 0 {
		t.Fatalf("expected no output, got %q", output.String())
	}
}

func TestEnsureRepoKeyFallsBackToConfiguredReposFile(t *testing.T) {
	configDir := t.TempDir()
	reposFile := filepath.Join(t.TempDir(), "repos.yaml")
	t.Setenv("KANBAN_CONFIG_DIR", configDir)
	t.Setenv("TSCTL_REPOS_FILE", filepath.Join(t.TempDir(), "missing.yaml"))

	if err := os.WriteFile(reposFile, []byte(`
repos:
  - key: example
    github: jmh-devel/example
`), 0o600); err != nil {
		t.Fatalf("write repos file: %v", err)
	}
	config := state.DefaultConfig()
	config.Repos["jmh-devel/example"] = state.RepoConfig{ReposFile: reposFile}
	if err := state.SaveConfig(config); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	key, err := EnsureRepoKey(context.Background(), "jmh-devel/example", ioDiscard{})
	if err != nil {
		t.Fatalf("EnsureRepoKey returned error: %v", err)
	}
	if key != "example" {
		t.Fatalf("key = %q, want example", key)
	}
}

func TestEnsureRepoKeyReturnsEmptyWhenNoMatch(t *testing.T) {
	configDir := t.TempDir()
	reposFile := filepath.Join(t.TempDir(), "repos.yaml")
	t.Setenv("KANBAN_CONFIG_DIR", configDir)
	t.Setenv("TSCTL_REPOS_FILE", reposFile)

	if err := os.WriteFile(reposFile, []byte(`
repos:
  - key: other
    github: jmh-devel/other
`), 0o600); err != nil {
		t.Fatalf("write repos file: %v", err)
	}

	key, err := EnsureRepoKey(context.Background(), "jmh-devel/example", ioDiscard{})
	if err != nil {
		t.Fatalf("EnsureRepoKey returned error: %v", err)
	}
	if key != "" {
		t.Fatalf("key = %q, want empty", key)
	}

	data, err := os.ReadFile(filepath.Join(configDir, state.ConfigFileName))
	if err == nil {
		var config state.Config
		if unmarshalErr := json.Unmarshal(data, &config); unmarshalErr != nil {
			t.Fatalf("decode saved config: %v", unmarshalErr)
		}
		if _, ok := config.Repos["jmh-devel/example"]; ok {
			t.Fatalf("config should not contain unmatched repo: %s", data)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read config: %v", err)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}
