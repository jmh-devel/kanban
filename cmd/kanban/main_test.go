package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestVersionArgumentsShareOutput(t *testing.T) {
	oldVersion, oldCommit, oldBuilt, oldDirty := version, commit, built, dirty
	version = "0.1.0"
	commit = "93abf7c"
	built = "2026-05-05T22:32:51Z"
	dirty = ""
	t.Cleanup(func() {
		version = oldVersion
		commit = oldCommit
		built = oldBuilt
		dirty = oldDirty
	})

	commandOut, commandCode := captureStdout(t, func() int { return run([]string{"version"}) })
	longOut, longCode := captureStdout(t, func() int { return run([]string{"--version"}) })
	shortOut, shortCode := captureStdout(t, func() int { return run([]string{"-v"}) })
	extraOut, extraCode := captureStdout(t, func() int { return run([]string{"--version", "--repo", "owner/repo"}) })

	if commandCode != 0 || longCode != 0 || shortCode != 0 || extraCode != 0 {
		t.Fatalf(
			"unexpected exit codes: version=%d --version=%d -v=%d --version extra=%d",
			commandCode,
			longCode,
			shortCode,
			extraCode,
		)
	}
	if commandOut != longOut || commandOut != shortOut || commandOut != extraOut {
		t.Fatalf(
			"version outputs differ:\nversion:\n%s\n--version:\n%s\n-v:\n%s\n--version extra:\n%s",
			commandOut,
			longOut,
			shortOut,
			extraOut,
		)
	}
	for _, want := range []string{
		"kanban 0.1.0\n",
		"commit: 93abf7c\n",
		"built: 2026-05-05T22:32:51Z\n",
		"go: ",
		"platform: ",
	} {
		if !strings.Contains(commandOut, want) {
			t.Fatalf("missing %q in:\n%s", want, commandOut)
		}
	}
}

func captureStdout(t *testing.T, fn func() int) (string, int) {
	t.Helper()

	oldStdout := os.Stdout
	readFile, writeFile, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writeFile
	defer func() {
		os.Stdout = oldStdout
	}()

	code := fn()
	if err := writeFile.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(readFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := readFile.Close(); err != nil {
		t.Fatal(err)
	}
	return string(out), code
}
