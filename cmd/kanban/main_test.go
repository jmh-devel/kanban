package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestVersionRoutingUsesSharedOutput(t *testing.T) {
	restore := setVersionFields("1.2.3", "abc1234", "2026-05-05T22:32:51Z", "true")
	defer restore()

	var expected string
	for _, args := range [][]string{
		{"version"},
		{"--version"},
		{"-v"},
	} {
		args := args
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, stdout := captureRun(t, args)
			if code != 0 {
				t.Fatalf("run(%v) exit code = %d, want 0", args, code)
			}
			if expected == "" {
				expected = stdout
			}
			if stdout != expected {
				t.Fatalf("run(%v) output differs\n got:\n%s\nwant:\n%s", args, stdout, expected)
			}
		})
	}

	for _, want := range []string{
		"kanban 1.2.3\n",
		"commit: abc1234\n",
		"built: 2026-05-05T22:32:51Z\n",
		"go: ",
		"platform: ",
		"dirty: true\n",
	} {
		if !strings.Contains(expected, want) {
			t.Fatalf("version output missing %q:\n%s", want, expected)
		}
	}
}

func TestVersionInfoUsesFallbackBuildMetadata(t *testing.T) {
	restore := setVersionFields("dev", "unknown", "", "")
	defer restore()

	got := versionInfo()
	for _, want := range []string{
		"kanban dev\n",
		"commit: unknown\n",
		"built: unknown\n",
		"go: ",
		"platform: ",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("version output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "dirty:") {
		t.Fatalf("version output should omit dirty state when unavailable:\n%s", got)
	}
}

func TestVersionFlagDoesNotFallThroughToPrintFlags(t *testing.T) {
	restore := setVersionFields("dev", "unknown", "", "")
	defer restore()

	code, stdout := captureRun(t, []string{"--version"})
	if code != 0 {
		t.Fatalf("run(--version) exit code = %d, want 0", code)
	}
	if strings.Contains(stdout, "Usage of print") {
		t.Fatalf("--version fell through to print flags:\n%s", stdout)
	}
	if !strings.HasPrefix(stdout, "kanban dev\n") {
		t.Fatalf("unexpected --version output:\n%s", stdout)
	}
}

func setVersionFields(nextVersion, nextCommit, nextBuildDate, nextDirty string) func() {
	previousVersion := version
	previousCommit := commit
	previousBuildDate := buildDate
	previousDirty := dirty

	version = nextVersion
	commit = nextCommit
	buildDate = nextBuildDate
	dirty = nextDirty

	return func() {
		version = previousVersion
		commit = previousCommit
		buildDate = previousBuildDate
		dirty = previousDirty
	}
}

func captureRun(t *testing.T, args []string) (int, string) {
	t.Helper()

	previousStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = previousStdout
	}()

	code := run(args)

	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout pipe writer: %v", err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close stdout pipe reader: %v", err)
	}

	return code, string(output)
}
