package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/jmh-devel/kanban/internal/state"
)

type JobRecord struct {
	Job     string `json:"job"`
	Runner  string `json:"runner"`
	Repo    string `json:"repo"`
	Issue   int    `json:"issue,omitempty"`
	Mode    string `json:"mode,omitempty"`
	Status  string `json:"status"`
	Started string `json:"started"`
	Ended   string `json:"ended"`
	Elapsed string `json:"elapsed"`
}

type JobsRunner func(context.Context) ([]byte, error)

func ListJobs(ctx context.Context, repo string, runner JobsRunner) ([]JobRecord, error) {
	dispatches, err := state.LoadDispatches()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	jobs := localJobs(dispatches, repo, now)

	if runner == nil {
		runner = runTSCTLJobs
	}
	output, err := runner(ctx)
	if err == nil && len(strings.TrimSpace(string(output))) > 0 {
		clusterJobs := parseTSCTLJobs(output, now)
		jobs = mergeJobs(jobs, clusterJobs, repo)
	}

	if repo != "" {
		filtered := jobs[:0]
		for _, job := range jobs {
			if job.Repo == repo {
				filtered = append(filtered, job)
			}
		}
		jobs = filtered
	}
	return jobs, nil
}

func runTSCTLJobs(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "tsctl", "agent", "jobs", "--output", "json").Output()
}

func localJobs(dispatches []state.Dispatch, repo string, now time.Time) []JobRecord {
	jobs := make([]JobRecord, 0, len(dispatches))
	for i := len(dispatches) - 1; i >= 0; i-- {
		dispatch := dispatches[i]
		if repo != "" && dispatch.Repo != repo {
			continue
		}
		jobs = append(jobs, JobRecord{
			Job:     dispatch.JobID,
			Runner:  dispatch.Runner,
			Repo:    dispatch.Repo,
			Issue:   dispatch.Issue,
			Mode:    dispatch.Mode,
			Status:  jobStatus(dispatch.Status),
			Started: formatJobTime(dispatch.DispatchedAt),
			Elapsed: elapsedSince(dispatch.DispatchedAt, now),
		})
	}
	return jobs
}

func parseTSCTLJobs(data []byte, now time.Time) []JobRecord {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	switch value := raw.(type) {
	case []any:
		return parseJobList(value, now)
	case map[string]any:
		for _, key := range []string{"jobs", "items", "data"} {
			if list, ok := value[key].([]any); ok {
				return parseJobList(list, now)
			}
		}
		return []JobRecord{parseJobMap(value, now)}
	default:
		return nil
	}
}

func parseJobList(values []any, now time.Time) []JobRecord {
	jobs := make([]JobRecord, 0, len(values))
	for _, value := range values {
		jobMap, ok := value.(map[string]any)
		if !ok {
			continue
		}
		jobs = append(jobs, parseJobMap(jobMap, now))
	}
	return jobs
}

func parseJobMap(value map[string]any, now time.Time) JobRecord {
	started := firstString(value, "started", "started_at", "start_time", "created_at", "created")
	ended := firstString(value, "ended", "ended_at", "end_time", "completed_at", "finished_at", "finished")
	elapsed := firstString(value, "elapsed", "duration")
	if elapsed == "" {
		elapsed = elapsedBetween(started, ended, now)
	}
	return JobRecord{
		Job:     firstString(value, "job", "job_id", "id", "name"),
		Runner:  firstString(value, "runner", "agent", "worker"),
		Repo:    firstString(value, "repo", "repository", "repo_slug"),
		Issue:   firstInt(value, "issue", "issue_number"),
		Mode:    firstString(value, "mode"),
		Status:  jobStatus(firstString(value, "status", "state", "phase")),
		Started: started,
		Ended:   ended,
		Elapsed: elapsed,
	}
}

func mergeJobs(local []JobRecord, cluster []JobRecord, repo string) []JobRecord {
	jobs := append([]JobRecord(nil), local...)
	for _, clusterJob := range cluster {
		if repo != "" && clusterJob.Repo != "" && clusterJob.Repo != repo {
			continue
		}
		matched := false
		for i := range jobs {
			if sameJob(jobs[i], clusterJob) {
				jobs[i] = mergeJob(jobs[i], clusterJob)
				matched = true
				break
			}
		}
		if !matched {
			jobs = append(jobs, clusterJob)
		}
	}
	return jobs
}

func sameJob(a JobRecord, b JobRecord) bool {
	if a.Job != "" && b.Job != "" {
		return a.Job == b.Job
	}
	return a.Repo != "" && a.Repo == b.Repo && a.Issue > 0 && a.Issue == b.Issue && (a.Runner == "" || b.Runner == "" || a.Runner == b.Runner)
}

func mergeJob(local JobRecord, cluster JobRecord) JobRecord {
	if cluster.Job != "" {
		local.Job = cluster.Job
	}
	if cluster.Runner != "" {
		local.Runner = cluster.Runner
	}
	if cluster.Repo != "" {
		local.Repo = cluster.Repo
	}
	if cluster.Issue > 0 {
		local.Issue = cluster.Issue
	}
	if cluster.Mode != "" {
		local.Mode = cluster.Mode
	}
	if cluster.Status != "" {
		local.Status = cluster.Status
	}
	if cluster.Started != "" {
		local.Started = cluster.Started
	}
	if cluster.Ended != "" {
		local.Ended = cluster.Ended
	}
	if cluster.Elapsed != "" {
		local.Elapsed = cluster.Elapsed
	}
	return local
}

func jobStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "", state.StatusDispatched, "queued", "created":
		return "pending"
	case "running", "active", "in_progress", "in-progress":
		return "running"
	case state.StatusCompleted, "complete", "success", "succeeded", "passed", "pass":
		return "passed"
	case state.StatusFailed, state.StatusSuperseded, "error", "errored", "failure", "fail":
		return "failed"
	case state.StatusCancelled, "canceled":
		return "failed"
	default:
		return status
	}
}

func firstString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := value[key]
		if !ok || raw == nil {
			continue
		}
		switch typed := raw.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case float64:
			return strconv.FormatFloat(typed, 'f', -1, 64)
		case bool:
			return strconv.FormatBool(typed)
		default:
			text := strings.TrimSpace(fmt.Sprint(typed))
			if text != "" {
				return text
			}
		}
	}
	return ""
}

func firstInt(value map[string]any, keys ...string) int {
	for _, key := range keys {
		raw, ok := value[key]
		if !ok || raw == nil {
			continue
		}
		switch typed := raw.(type) {
		case float64:
			return int(typed)
		case int:
			return typed
		case string:
			number, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(typed), "#"))
			if err == nil {
				return number
			}
		}
	}
	return 0
}

func elapsedBetween(started string, ended string, now time.Time) string {
	start, ok := parseJobTime(started)
	if !ok {
		return ""
	}
	end := now
	if ended != "" {
		if parsed, ok := parseJobTime(ended); ok {
			end = parsed
		}
	}
	return formatDuration(end.Sub(start))
}

func elapsedSince(started time.Time, now time.Time) string {
	if started.IsZero() {
		return ""
	}
	return formatDuration(now.Sub(started))
}

func parseJobTime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05 MST", "2006-01-02 15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func formatJobTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func formatDuration(value time.Duration) string {
	if value < 0 {
		value = 0
	}
	value = value.Round(time.Second)
	hours := int(value / time.Hour)
	value -= time.Duration(hours) * time.Hour
	minutes := int(value / time.Minute)
	value -= time.Duration(minutes) * time.Minute
	seconds := int(value / time.Second)
	if hours > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm%02ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}
