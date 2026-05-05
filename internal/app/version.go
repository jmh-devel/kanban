package app

import (
	"fmt"
	"runtime"
	"strings"
)

type VersionInfo struct {
	Version string
	Commit  string
	Built   string
	Dirty   string
}

func RenderVersion(info VersionInfo) string {
	version := strings.TrimSpace(info.Version)
	if version == "" {
		version = "dev"
	}
	commit := strings.TrimSpace(info.Commit)
	if commit == "" {
		commit = "unknown"
	}
	built := strings.TrimSpace(info.Built)
	if built == "" {
		built = "unknown"
	}

	var builder strings.Builder
	_, _ = fmt.Fprintf(&builder, "kanban %s\n", version)
	_, _ = fmt.Fprintf(&builder, "commit: %s\n", commit)
	_, _ = fmt.Fprintf(&builder, "built: %s\n", built)
	_, _ = fmt.Fprintf(&builder, "go: %s\n", runtime.Version())
	_, _ = fmt.Fprintf(&builder, "platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	if dirty := strings.TrimSpace(info.Dirty); dirty != "" {
		_, _ = fmt.Fprintf(&builder, "dirty: %s\n", dirty)
	}
	return builder.String()
}
