package app

import (
	"fmt"
	"strings"

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
			_, _ = fmt.Fprintln(&builder, "  (no issues)")
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
		}
		_, _ = fmt.Fprintln(&builder)
	}

	return strings.TrimSpace(builder.String()) + "\n"
}
