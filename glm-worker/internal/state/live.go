package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func WarnTaskLiveSkip(err error) {
	writeStatsWarningEvent("live_status", "live status snapshotの書込みに失敗したため以後のlive表示更新をskipします（task本体へ影響しません）", err)
}

type TaskLiveTool struct {
	ToolID     string `json:"tool_id"`
	Command    string `json:"command,omitempty"`
	Purpose    string `json:"purpose,omitempty"`
	Background bool   `json:"background,omitempty"`
	WaitTaskID string `json:"wait_task_id,omitempty"`
}

type TaskLiveStatus struct {
	UpdatedAt   time.Time `json:"updated_at"`
	LastEventAt time.Time `json:"last_event_at"`

	LastModelActivityAt time.Time      `json:"last_model_activity_at"`
	Tools               []TaskLiveTool `json:"tools,omitempty"`
}

func IsModelActivityEvent(record TaskEventRecord) bool {
	if record.Kind == "system" {
		return record.Subtype == "thinking_tokens"
	}
	if record.Kind != "assistant" {
		return false
	}
	for _, block := range record.Blocks {
		switch block.Type {
		case "thinking", "text", "tool_use":
			return true
		}
	}
	return false
}

func (s *StateStore) TaskLiveStatusPath(taskID string) string {
	return s.Path(filepath.Join("events", taskID+".live.json"))
}

func (s *StateStore) WriteTaskLiveStatus(taskID string, status TaskLiveStatus) error {
	if status.UpdatedAt.IsZero() {
		status.UpdatedAt = time.Now().UTC()
	}
	data, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("task live statusをJSON化できません: %w", err)
	}
	return writeFileAtomic(s.TaskLiveStatusPath(taskID), append(data, '\n'), 0o600)
}

func (s *StateStore) ReadTaskLiveStatus(taskID string) (TaskLiveStatus, error) {
	data, err := os.ReadFile(s.TaskLiveStatusPath(taskID))
	if err != nil {
		return TaskLiveStatus{}, err
	}
	var status TaskLiveStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return TaskLiveStatus{}, fmt.Errorf("task live statusを読めません: %w", err)
	}
	return status, nil
}
