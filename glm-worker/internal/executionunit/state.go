package executionunit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type Disposition struct {
	Version        int       `json:"version"`
	TaskID         string    `json:"task_id"`
	ActiveTaskPath string    `json:"active_task_path"`
	ExecutionUnit  string    `json:"execution_unit"`
	UpdatedAt      time.Time `json:"updated_at"`
}

const (
	DispositionVersion = 1
	DispositionEnv     = "GLM_EXECUTION_UNIT_DISPOSITION"
)

func RecordDisposition(st *state.StateStore, activeTaskPath, executionUnit string, now time.Time) error {
	if executionUnit != ExecutionUnitSingle && executionUnit != ExecutionUnitMilestones {
		return fmt.Errorf("unsupported execution-unit disposition %q", executionUnit)
	}
	taskID, err := st.TaskID()
	if err != nil {
		return err
	}
	activeTaskPath = strings.TrimSpace(activeTaskPath)
	if activeTaskPath == "" {
		return fmt.Errorf("execution-unit disposition requires an ACTIVE task")
	}
	disposition := Disposition{
		Version:        DispositionVersion,
		TaskID:         taskID,
		ActiveTaskPath: activeTaskPath,
		ExecutionUnit:  executionUnit,
		UpdatedAt:      now.UTC(),
	}
	data, err := json.Marshal(disposition)
	if err != nil {
		return fmt.Errorf("encode execution-unit disposition: %w", err)
	}
	return st.Write(state.ExecutionUnitDispositionStateFile, string(data))
}

func LoadDisposition(st *state.StateStore) (*Disposition, error) {
	data, err := os.ReadFile(st.Path(state.ExecutionUnitDispositionStateFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var disposition Disposition
	if err := json.Unmarshal(data, &disposition); err != nil {
		return nil, fmt.Errorf("read execution-unit disposition: %w", err)
	}
	if disposition.Version != DispositionVersion {
		return nil, fmt.Errorf("unsupported execution-unit disposition version: %d", disposition.Version)
	}
	if disposition.TaskID == "" || strings.TrimSpace(disposition.ActiveTaskPath) == "" {
		return nil, fmt.Errorf("execution-unit disposition authority is incomplete")
	}
	if disposition.ExecutionUnit != ExecutionUnitSingle && disposition.ExecutionUnit != ExecutionUnitMilestones {
		return nil, fmt.Errorf("unsupported execution-unit disposition %q", disposition.ExecutionUnit)
	}
	return &disposition, nil
}

func CurrentDisposition(st *state.StateStore) (*Disposition, error) {
	disposition, err := LoadDisposition(st)
	if err != nil || disposition == nil {
		return disposition, err
	}
	taskID, err := st.TaskID()
	if err != nil {
		return nil, err
	}
	if disposition.TaskID != taskID {
		return nil, fmt.Errorf("execution-unit disposition task identity changed: disposition=%q current=%q", disposition.TaskID, taskID)
	}
	activeTaskPath, err := st.CurrentWorkflowTaskPath()
	if err != nil {
		return nil, fmt.Errorf("execution-unit disposition task binding is unavailable: %w", err)
	}
	activeTaskPath = strings.TrimSpace(activeTaskPath)
	if activeTaskPath == "" || disposition.ActiveTaskPath != activeTaskPath {
		return nil, fmt.Errorf("execution-unit disposition ACTIVE task changed: disposition=%q current=%q", disposition.ActiveTaskPath, activeTaskPath)
	}
	return disposition, nil
}
