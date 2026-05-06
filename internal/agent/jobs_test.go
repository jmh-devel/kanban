package agent

import (
	"context"
	"testing"
	"time"

	"github.com/jmh-devel/kanban/internal/state"
)

func TestListJobsMergesTSCTLStatusByIssue(t *testing.T) {
	t.Setenv("KANBAN_CONFIG_DIR", t.TempDir())
	started := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	if err := state.SaveDispatches([]state.Dispatch{{
		Repo:         "jmh-devel/example",
		Issue:        24,
		Runner:       "tsctl",
		Mode:         "implement",
		DispatchedAt: started,
		Status:       state.StatusDispatched,
	}}); err != nil {
		t.Fatal(err)
	}

	jobs, err := ListJobs(context.Background(), "jmh-devel/example", func(context.Context) ([]byte, error) {
		return []byte(`[{"job":"job-24","runner":"tsctl","repo":"jmh-devel/example","issue":24,"status":"running","started":"2026-05-05T12:00:00Z"}]`), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs = %+v", jobs)
	}
	if jobs[0].Job != "job-24" || jobs[0].Status != "running" || jobs[0].Mode != "implement" {
		t.Fatalf("job = %+v", jobs[0])
	}
}
