package state

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
)

type RuntimeDisposition struct {
	Version        int        `json:"version"`
	TaskID         string     `json:"task_id"`
	TaskPath       string     `json:"task_path"`
	TransitionID   string     `json:"transition_id"`
	EvidenceDigest string     `json:"evidence_digest"`
	Reason         string     `json:"reason"`
	PreviousStatus TaskStatus `json:"previous_status"`
}

const RuntimeDispositionFile = "runtime-disposition.json"

func validRuntimeDisposition(record RuntimeDisposition) bool {
	return record.Version == 1 && record.TaskID != "" && record.TaskPath != "" && record.TransitionID != "" && record.EvidenceDigest != "" && strings.TrimSpace(record.Reason) != "" && record.PreviousStatus.Known() && record.PreviousStatus != TaskStatusNone && record.PreviousStatus != TaskStatusComplete && record.PreviousStatus != TaskStatusUserTerminated
}

func (s *StateStore) RecordRuntimeDisposition(record RuntimeDisposition) error {
	if !validRuntimeDisposition(record) || s.ReadOr("task.id", "") != record.TaskID {
		return fmt.Errorf("runtime disposition identity is invalid")
	}
	if s.TaskStatus() != record.PreviousStatus && s.TaskStatus() != TaskStatusUserTerminated {
		return fmt.Errorf("runtime disposition status changed")
	}
	if raw, err := os.ReadFile(s.Path(RuntimeDispositionFile)); err == nil {
		var existing RuntimeDisposition
		if err := json.Unmarshal(raw, &existing); err != nil {
			return err
		}
		if !reflect.DeepEqual(existing, record) {
			return fmt.Errorf("runtime disposition cannot be replaced")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(s.Path(RuntimeDispositionFile), raw, 0600); err != nil {
		return err
	}
	return s.SetTaskStatus(TaskStatusUserTerminated)
}

func (s *StateStore) runtimeDispositionActionPlan() (ParentActionPlan, error) {
	raw, err := os.ReadFile(s.Path(RuntimeDispositionFile))
	if err != nil {
		return ParentActionPlan{}, err
	}
	var record RuntimeDisposition
	if err := json.Unmarshal(raw, &record); err != nil {
		return ParentActionPlan{}, err
	}
	if !validRuntimeDisposition(record) || s.ReadOr("task.id", "") != record.TaskID || s.TaskStatus() != TaskStatusUserTerminated {
		return ParentActionPlan{}, fmt.Errorf("runtime disposition terminal authority differs")
	}
	return actionPlan(ParentActionNone, ""), nil
}
