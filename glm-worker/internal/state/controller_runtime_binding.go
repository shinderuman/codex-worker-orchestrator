package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type ControllerRuntimeBinding struct {
	AttemptID          string
	TaskPath           string
	TaskContractDigest string
}

type controllerRuntimeBindingWire struct {
	Version            int    `json:"version"`
	AttemptID          string `json:"attempt_id"`
	TaskPath           string `json:"task_path"`
	TaskContractDigest string `json:"task_contract_digest"`
}

const (
	controllerRuntimeBindingStateFile = "controller-runtime-binding.json"
	controllerRuntimeBindingVersion   = 1
)

func (s *StateStore) SaveControllerRuntimeBinding(binding ControllerRuntimeBinding) error {
	if err := validateControllerRuntimeBinding(binding); err != nil {
		return err
	}
	wire := controllerRuntimeBindingWire{
		Version:            controllerRuntimeBindingVersion,
		AttemptID:          binding.AttemptID,
		TaskPath:           binding.TaskPath,
		TaskContractDigest: binding.TaskContractDigest,
	}
	data, err := json.Marshal(wire)
	if err != nil {
		return fmt.Errorf("controller runtime bindingをJSON化できません: %w", err)
	}
	if err := writeFileAtomic(s.Path(controllerRuntimeBindingStateFile), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("controller runtime bindingを書き込めません: %w", err)
	}
	return nil
}

func (s *StateStore) LoadControllerRuntimeBinding() (ControllerRuntimeBinding, error) {
	data, err := os.ReadFile(s.Path(controllerRuntimeBindingStateFile))
	if err != nil {
		return ControllerRuntimeBinding{}, err
	}
	var wire controllerRuntimeBindingWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return ControllerRuntimeBinding{}, fmt.Errorf("controller runtime bindingを読めません: %w", err)
	}
	if wire.Version != controllerRuntimeBindingVersion {
		return ControllerRuntimeBinding{}, fmt.Errorf("unsupported controller runtime binding version: %d", wire.Version)
	}
	binding := ControllerRuntimeBinding{
		AttemptID:          wire.AttemptID,
		TaskPath:           wire.TaskPath,
		TaskContractDigest: wire.TaskContractDigest,
	}
	if err := validateControllerRuntimeBinding(binding); err != nil {
		return ControllerRuntimeBinding{}, err
	}
	return binding, nil
}

func (s *StateStore) CurrentWorkflowTaskPath() (string, error) {
	binding, err := s.LoadControllerRuntimeBinding()
	if err == nil {
		return binding.TaskPath, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return s.ReadOr("active-task", ""), nil
}

func (s *StateStore) ClearControllerRuntimeBinding() error {
	return s.Remove(controllerRuntimeBindingStateFile)
}

func validateControllerRuntimeBinding(binding ControllerRuntimeBinding) error {
	if binding.AttemptID == "" {
		return fmt.Errorf("controller runtime binding attempt ID is empty")
	}
	if binding.TaskPath == "" || binding.TaskContractDigest == "" {
		return fmt.Errorf("controller runtime binding semantic task identity is incomplete")
	}
	return nil
}
