package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	TypeDispatch = "dispatch"
	TypeReview   = "review"

	StatusDispatched = "dispatched"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
	StatusCancelled  = "cancelled"
	StatusSuperseded = "superseded"
)

type Dispatch struct {
	Repo         string    `json:"repo"`
	Issue        int       `json:"issue"`
	Type         string    `json:"type,omitempty"`
	Runner       string    `json:"runner"`
	Mode         string    `json:"mode"`
	DispatchedAt time.Time `json:"dispatched_at"`
	Command      string    `json:"command"`
	JobID        string    `json:"job_id,omitempty"`
	Status       string    `json:"status"`
}

func LoadDispatches() ([]Dispatch, error) {
	path, err := DispatchesPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read dispatches: %w", err)
	}
	var dispatches []Dispatch
	if err := json.Unmarshal(data, &dispatches); err != nil {
		return nil, fmt.Errorf("decode dispatches: %w", err)
	}
	return dispatches, nil
}

func SaveDispatches(dispatches []Dispatch) error {
	path, err := DispatchesPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create dispatch dir: %w", err)
	}
	data, err := json.MarshalIndent(dispatches, "", "  ")
	if err != nil {
		return fmt.Errorf("encode dispatches: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write dispatches: %w", err)
	}
	return nil
}

func ActiveDispatch(dispatches []Dispatch, repo string, issue int) (Dispatch, bool) {
	for i := len(dispatches) - 1; i >= 0; i-- {
		dispatch := dispatches[i]
		if dispatch.Repo == repo && dispatch.Issue == issue && IsActiveStatus(dispatch.Status) {
			return dispatch, true
		}
	}
	return Dispatch{}, false
}

func ActiveDispatchByType(dispatches []Dispatch, repo string, issue int, dispatchType string) (Dispatch, bool) {
	for i := len(dispatches) - 1; i >= 0; i-- {
		dispatch := dispatches[i]
		if dispatch.Repo == repo && dispatch.Issue == issue && dispatch.TypeName() == dispatchType && IsActiveStatus(dispatch.Status) {
			return dispatch, true
		}
	}
	return Dispatch{}, false
}

func ActiveDispatchesByIssue(dispatches []Dispatch, repo string) map[int]Dispatch {
	active := make(map[int]Dispatch)
	for _, dispatch := range dispatches {
		if dispatch.Repo != repo || !IsActiveStatus(dispatch.Status) {
			continue
		}
		active[dispatch.Issue] = dispatch
	}
	return active
}

func AppendDispatch(dispatches []Dispatch, dispatch Dispatch, supersedeExisting bool) []Dispatch {
	if supersedeExisting {
		for i := range dispatches {
			if dispatches[i].Repo == dispatch.Repo && dispatches[i].Issue == dispatch.Issue && dispatches[i].TypeName() == dispatch.TypeName() && IsActiveStatus(dispatches[i].Status) {
				dispatches[i].Status = StatusSuperseded
			}
		}
	}
	return append(dispatches, dispatch)
}

func (d Dispatch) TypeName() string {
	if strings.TrimSpace(d.Type) == "" {
		return TypeDispatch
	}
	return strings.TrimSpace(d.Type)
}

func MarkActiveDispatches(repo string, issue int, status string) (bool, error) {
	dispatches, err := LoadDispatches()
	if err != nil {
		return false, err
	}
	changed := false
	for i := range dispatches {
		if dispatches[i].Repo == repo && dispatches[i].Issue == issue && IsActiveStatus(dispatches[i].Status) {
			dispatches[i].Status = status
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	if err := SaveDispatches(dispatches); err != nil {
		return false, err
	}
	return true, nil
}

func IsActiveStatus(status string) bool {
	status = strings.TrimSpace(status)
	return status == "" || status == StatusDispatched
}
