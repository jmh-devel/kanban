package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/jmh-devel/kanban/internal/github"
)

func RenderBoardText(board github.Board) string {
	var builder strings.Builder
	_, _ = fmt.Fprintf(&builder, "Repo: %s\n", board.Repo.Slug)
	if board.Repo.RootPath != "" {
		_, _ = fmt.Fprintf(&builder, "Path: %s\n", board.Repo.RootPath)
	}
	_, _ = fmt.Fprintf(&builder, "Updated: %s\n\n", board.UpdatedAt.Format("2006-01-02 15:04:05 MST"))

	for _, section := range board.Sections {
		_, _ = fmt.Fprintf(&builder, "[%s] (%d)\n", section.Title, len(section.Issues))
		if len(section.Issues) == 0 {
			_, _ = fmt.Fprintln(&builder, "  (no open issues)")
			_, _ = fmt.Fprintln(&builder)
			continue
		}
		for _, issue := range section.Issues {
			labels := make([]string, 0, len(issue.Labels))
			for _, label := range issue.Labels {
				labels = append(labels, label.Name)
			}
			labelSuffix := ""
			if len(labels) > 0 {
				labelSuffix = " [" + strings.Join(labels, ", ") + "]"
			}
			_, _ = fmt.Fprintf(&builder, "  #%d %s%s\n", issue.Number, issue.Title, labelSuffix)
			if issue.Agent != nil {
				_, _ = fmt.Fprintf(&builder, "    %s %s · %s · %s ago\n", agentSymbol(issue.Agent.Status), issue.Agent.Runner, issue.Agent.Mode, age(board.UpdatedAt.Sub(issue.Agent.DispatchedAt)))
			}
		}
		_, _ = fmt.Fprintln(&builder)
	}

	return strings.TrimSpace(builder.String()) + "\n"
}

func agentSymbol(status string) string {
	switch status {
	case "completed":
		return "○"
	default:
		return "●"
	}
}

func age(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	if duration < time.Minute {
		return "now"
	}
	if duration < time.Hour {
		return fmt.Sprintf("%dm", int(duration.Minutes()))
	}
	if duration < 24*time.Hour {
		return fmt.Sprintf("%dh", int(duration.Hours()))
	}
	return fmt.Sprintf("%dd", int(duration.Hours()/24))
}
