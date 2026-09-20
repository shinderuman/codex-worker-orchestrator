package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
)

type PendingDefectRegistration struct {
	TaskID           string `json:"task_id"`
	SourceActiveTask string `json:"source_active_task"`
	TaskPath         string `json:"task_path"`
}

type pendingDefectRegistrationState struct {
	Version       int                         `json:"version"`
	Registrations []PendingDefectRegistration `json:"registrations"`
}

const (
	pendingDefectRegistrationStateFile = "pending-defect-registrations.json"
	pendingDefectRegistrationVersion   = 1
)

func (s *StateStore) RecordPendingDefectRegistration(taskPath, sourceActiveTask string) (PendingDefectRegistration, bool, error) {
	taskID, err := s.TaskID()
	if err != nil {
		return PendingDefectRegistration{}, false, err
	}
	if sourceActiveTask == "" || taskPath == "" {
		return PendingDefectRegistration{}, false, fmt.Errorf("defect registration requires source active task and target task path")
	}
	registrations, err := s.PendingDefectRegistrations()
	if err != nil {
		return PendingDefectRegistration{}, false, err
	}
	for _, registration := range registrations {
		if registration.TaskPath != taskPath {
			continue
		}
		if registration.TaskID != taskID || registration.SourceActiveTask != sourceActiveTask {
			return PendingDefectRegistration{}, false, fmt.Errorf("defect registration %s is already bound to a different task identity", taskPath)
		}
		return registration, false, nil
	}
	registration := PendingDefectRegistration{
		TaskID:           taskID,
		SourceActiveTask: sourceActiveTask,
		TaskPath:         taskPath,
	}
	registrations = append(registrations, registration)
	if err := s.savePendingDefectRegistrations(registrations); err != nil {
		return PendingDefectRegistration{}, false, err
	}
	return registration, true, nil
}

func (s *StateStore) PendingDefectRegistrations() ([]PendingDefectRegistration, error) {
	data, err := s.Read(pendingDefectRegistrationStateFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var stored pendingDefectRegistrationState
	if err := json.Unmarshal([]byte(data), &stored); err != nil {
		return nil, fmt.Errorf("pending defect registration state cannot be read: %w", err)
	}
	if stored.Version != pendingDefectRegistrationVersion {
		return nil, fmt.Errorf("unsupported pending defect registration state version: %d", stored.Version)
	}
	if len(stored.Registrations) == 0 {
		return nil, fmt.Errorf("pending defect registration state has no registrations")
	}
	seen := make(map[string]struct{}, len(stored.Registrations))
	registrations := append([]PendingDefectRegistration(nil), stored.Registrations...)
	for _, registration := range registrations {
		if err := validatePendingDefectRegistration(registration); err != nil {
			return nil, err
		}
		if _, ok := seen[registration.TaskPath]; ok {
			return nil, fmt.Errorf("pending defect registration task path is duplicated: %s", registration.TaskPath)
		}
		seen[registration.TaskPath] = struct{}{}
	}
	sort.Slice(registrations, func(i, j int) bool { return registrations[i].TaskPath < registrations[j].TaskPath })
	return registrations, nil
}

func (s *StateStore) CurrentPendingDefectRegistrations() ([]PendingDefectRegistration, error) {
	registrations, err := s.PendingDefectRegistrations()
	if err != nil || len(registrations) == 0 {
		return registrations, err
	}
	taskID, err := s.TaskID()
	if err != nil {
		return nil, err
	}
	activeTask := s.ReadOr("active-task", "")
	if activeTask == "" {
		return nil, fmt.Errorf("pending defect registration has no current active task binding")
	}
	for _, registration := range registrations {
		if registration.TaskID != taskID || registration.SourceActiveTask != activeTask {
			return nil, fmt.Errorf("pending defect registration %s is stale or bound to a different active task", registration.TaskPath)
		}
	}
	return registrations, nil
}

func (s *StateStore) BindPendingDefectRegistration(taskPath string) (PendingDefectRegistration, error) {
	registrations, err := s.CurrentPendingDefectRegistrations()
	if err != nil {
		return PendingDefectRegistration{}, err
	}
	index := -1
	var bound PendingDefectRegistration
	for i, registration := range registrations {
		if registration.TaskPath == taskPath {
			index = i
			bound = registration
			break
		}
	}
	if index < 0 {
		return PendingDefectRegistration{}, fmt.Errorf("no pending defect registration for %s", taskPath)
	}
	registrations = append(registrations[:index], registrations[index+1:]...)
	if len(registrations) == 0 {
		if err := s.Remove(pendingDefectRegistrationStateFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return PendingDefectRegistration{}, err
		}
		return bound, nil
	}
	if err := s.savePendingDefectRegistrations(registrations); err != nil {
		return PendingDefectRegistration{}, err
	}
	return bound, nil
}

func (s *StateStore) savePendingDefectRegistrations(registrations []PendingDefectRegistration) error {
	if len(registrations) == 0 {
		return fmt.Errorf("cannot save empty pending defect registration state")
	}
	registrations = append([]PendingDefectRegistration(nil), registrations...)
	sort.Slice(registrations, func(i, j int) bool { return registrations[i].TaskPath < registrations[j].TaskPath })
	stored := pendingDefectRegistrationState{Version: pendingDefectRegistrationVersion, Registrations: registrations}
	data, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("pending defect registration state cannot be encoded: %w", err)
	}
	return s.Write(pendingDefectRegistrationStateFile, string(data))
}

func validatePendingDefectRegistration(registration PendingDefectRegistration) error {
	if !ValidGeneratedUUID(registration.TaskID) {
		return fmt.Errorf("pending defect registration task identity is invalid")
	}
	if registration.SourceActiveTask == "" || registration.TaskPath == "" {
		return fmt.Errorf("pending defect registration is missing task binding")
	}
	return nil
}
