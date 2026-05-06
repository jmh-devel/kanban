package state

import (
	"reflect"
	"testing"
)

func TestDefaultRunnerIsCodex(t *testing.T) {
	if DefaultRunner != "codex" {
		t.Fatalf("DefaultRunner = %q, want codex", DefaultRunner)
	}
}

func TestRunnerNamesIncludeDefaultAndManual(t *testing.T) {
	config := Config{
		Runners: map[string]RunnerConfig{
			"claude": {Kind: "local_cli", Command: "claude"},
		},
	}

	got := config.RunnerNames()
	want := []string{"claude", DefaultRunner, ManualRunner}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RunnerNames() = %v, want %v", got, want)
	}
}

func TestRunnerUsesBuiltInDefaultWhenMissing(t *testing.T) {
	config := Config{Runners: map[string]RunnerConfig{}}

	runner := config.Runner(DefaultRunner)
	if runner.Kind != "local_cli" || runner.Command != DefaultRunner {
		t.Fatalf("Runner(DefaultRunner) = %+v", runner)
	}

	manual := config.Runner(ManualRunner)
	if manual.Kind != ManualRunner {
		t.Fatalf("Runner(ManualRunner) = %+v", manual)
	}
}
