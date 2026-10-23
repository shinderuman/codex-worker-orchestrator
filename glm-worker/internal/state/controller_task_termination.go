package state

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
)

type ControllerTaskTermination struct {
	Version            int        `json:"version"`
	TransitionID       string     `json:"transition_id"`
	SourceGeneration   uint64     `json:"source_generation"`
	TaskPath           string     `json:"task_path"`
	TaskContractDigest string     `json:"task_contract_digest"`
	AttemptID          string     `json:"attempt_id"`
	Reason             string     `json:"reason"`
	PreviousRepoRoot   string     `json:"previous_repo_root"`
	PrimaryRoot        string     `json:"primary_root"`
	PreviousStatus     TaskStatus `json:"previous_status"`
}

const controllerTaskTerminationFile = "controller-task-termination.json"

func (s *StateStore) RecordControllerTaskTermination(record ControllerTaskTermination) error {
	if !validControllerTaskTermination(record) {
		return fmt.Errorf("controller Task termination authority is incomplete")
	}
	if err := s.verifyControllerTaskTerminationSource(record); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err := s.verifyExistingControllerTaskTermination(record); err != nil {
		return err
	}
	if err := writeFileAtomic(s.Path(controllerTaskTerminationFile), append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := s.Write("repo-root", record.PrimaryRoot); err != nil {
		return err
	}
	return s.SetTaskStatus(TaskStatusUserTerminated)
}

func validControllerTaskTermination(record ControllerTaskTermination) bool {
	return record.Version == 1 && record.TransitionID != "" && record.SourceGeneration != 0 && record.Reason != "" && record.PrimaryRoot != "" && record.PreviousRepoRoot != "" && record.PreviousStatus == TaskStatusAwaitingParentCompletion
}

func (s *StateStore) verifyControllerTaskTerminationSource(record ControllerTaskTermination) error {
	binding, err := s.LoadControllerRuntimeBinding()
	if err != nil {
		return err
	}
	if binding.TaskPath != record.TaskPath || binding.TaskContractDigest != record.TaskContractDigest || binding.AttemptID != record.AttemptID {
		return fmt.Errorf("controller Task termination runtime binding differs")
	}
	if s.TaskStatus() != record.PreviousStatus && s.TaskStatus() != TaskStatusUserTerminated {
		return fmt.Errorf("controller Task termination runtime status changed")
	}
	root := s.ReadOr("repo-root", "")
	if root != record.PreviousRepoRoot && root != record.PrimaryRoot {
		return fmt.Errorf("controller Task termination runtime workspace changed")
	}
	return nil
}

func (s *StateStore) verifyExistingControllerTaskTermination(record ControllerTaskTermination) error {
	if existing, err := os.ReadFile(s.Path(controllerTaskTerminationFile)); err == nil {
		var previous ControllerTaskTermination
		if err := json.Unmarshal(existing, &previous); err != nil {
			return err
		}
		if !reflect.DeepEqual(previous, record) {
			return fmt.Errorf("controller Task termination record was replaced")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *StateStore) userTerminatedActionPlan() (ParentActionPlan, error) {
	if s.Exists(RuntimeDispositionFile) {
		return s.runtimeDispositionActionPlan()
	}
	data, err := os.ReadFile(s.Path(controllerTaskTerminationFile))
	if err != nil {
		return ParentActionPlan{}, err
	}
	var record ControllerTaskTermination
	if err := json.Unmarshal(data, &record); err != nil {
		return ParentActionPlan{}, err
	}
	if !validControllerTaskTermination(record) {
		return ParentActionPlan{}, fmt.Errorf("user terminated runtime authority is invalid")
	}
	if err := s.verifyControllerTaskTerminationSource(record); err != nil {
		return ParentActionPlan{}, err
	}
	return actionPlan(ParentActionNone, ""), nil
}
