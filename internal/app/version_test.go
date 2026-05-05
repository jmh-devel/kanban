package app

import (
	"runtime"
	"strings"
	"testing"
)

func TestRenderVersion(t *testing.T) {
	out := RenderVersion(VersionInfo{
		Version: "0.1.0",
		Commit:  "93abf7c",
		Built:   "2026-05-05T22:32:51Z",
		Dirty:   "dirty",
	})

	for _, want := range []string{
		"kanban 0.1.0\n",
		"commit: 93abf7c\n",
		"built: 2026-05-05T22:32:51Z\n",
		"go: " + runtime.Version() + "\n",
		"platform: " + runtime.GOOS + "/" + runtime.GOARCH + "\n",
		"dirty: dirty\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderVersionFallbacks(t *testing.T) {
	out := RenderVersion(VersionInfo{})

	for _, want := range []string{
		"kanban dev\n",
		"commit: unknown\n",
		"built: unknown\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "dirty:") {
		t.Fatalf("unexpected dirty line in:\n%s", out)
	}
}
