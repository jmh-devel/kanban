package main

import (
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestVersionRoutesShareOutput(t *testing.T) {
	originalVersion := version
	originalCommit := commit
	originalBuildDate := buildDate
	originalDirty := dirty
	t.Cleanup(func() {
		version = originalVersion
		commit = originalCommit
		buildDate = originalBuildDate
		dirty = originalDirty
	})

	version = "0.1.0"
	commit = "93abf7c"
	buildDate = "2026-05-05T22:32:51Z"
	dirty = "true"

	commands := [][]string{
		{"version"},
		{"--version"},
		{"-v"},
	}
	var want string
	for _, args := range commands {
		code, out := captureRun(t, args)
		if code != 0 {
			t.Fatalf("run(%v) exit code = %d, want 0", args, code)
		}
		if want == "" {
			want = out
			continue
		}
		if out != want {
			t.Fatalf("run(%v) output mismatch\nwant:\n%s\ngot:\n%s", args, want, out)
		}
	}

	for _, field := range []string{
		"kanban 0.1.0\n",
		"commit: 93abf7c\n",
		"built: 2026-05-05T22:32:51Z\n",
		"go: " + runtime.Version() + "\n",
		"platform: " + runtime.GOOS + "/" + runtime.GOARCH + "\n",
		"dirty: true\n",
	} {
		if !strings.Contains(want, field) {
			t.Fatalf("version output missing %q:\n%s", field, want)
		}
	}
}

func TestVersionFallbacks(t *testing.T) {
	originalVersion := version
	originalCommit := commit
	originalBuildDate := buildDate
	originalDirty := dirty
	t.Cleanup(func() {
		version = originalVersion
		commit = originalCommit
		buildDate = originalBuildDate
		dirty = originalDirty
	})

	version = ""
	commit = ""
	buildDate = ""
	dirty = ""

	out := formatVersion()
	for _, field := range []string{
		"kanban dev\n",
		"commit: unknown\n",
		"built: unknown\n",
	} {
		if !strings.Contains(out, field) {
			t.Fatalf("version output missing fallback %q:\n%s", field, out)
		}
	}
	if strings.Contains(out, "dirty:") {
		t.Fatalf("version output should omit empty dirty state:\n%s", out)
	}
}

func captureRun(t *testing.T, args []string) (int, string) {
	t.Helper()

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = originalStdout
	}()

	code := run(args)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}

	return code, string(output)
}
