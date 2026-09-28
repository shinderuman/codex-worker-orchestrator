package state

import (
	"encoding/json"
	"fmt"
	"os"
)

type FinalizationEvidence struct {
	Version         int    `json:"version"`
	TaskID          string `json:"task_id"`
	Form            string `json:"form"`
	ValidationRunID string `json:"validation_run_id"`
	SnapshotID      string `json:"snapshot_id"`
	RepositoryLint  string `json:"repository_lint"`
}

const (
	finalizationEvidenceFile    = "finalization-evidence.json"
	finalizationEvidenceVersion = 1
)

func NewFinalizationEvidence(taskID, form, validationRunID, snapshotID string) FinalizationEvidence {
	return FinalizationEvidence{
		Version:         finalizationEvidenceVersion,
		TaskID:          taskID,
		Form:            form,
		ValidationRunID: validationRunID,
		SnapshotID:      snapshotID,
		RepositoryLint:  ValidationResultPass,
	}
}

func (s *StateStore) SaveFinalizationEvidence(evidence FinalizationEvidence) error {
	if err := validateFinalizationEvidence(evidence); err != nil {
		return err
	}
	currentTaskID, err := s.TaskID()
	if err != nil {
		return err
	}
	if evidence.TaskID != currentTaskID {
		return fmt.Errorf("finalization evidence task %s does not match current task %s", evidence.TaskID, currentTaskID)
	}
	data, err := json.Marshal(evidence)
	if err != nil {
		return fmt.Errorf("finalization evidenceをJSON化できません: %w", err)
	}
	if err := s.Write(finalizationEvidenceFile, string(data)); err != nil {
		return fmt.Errorf("finalization evidenceを書き込めません: %w", err)
	}
	return nil
}

func (s *StateStore) LoadFinalizationEvidence() (FinalizationEvidence, error) {
	data, err := os.ReadFile(s.Path(finalizationEvidenceFile))
	if err != nil {
		return FinalizationEvidence{}, err
	}
	var evidence FinalizationEvidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		return FinalizationEvidence{}, fmt.Errorf("finalization evidenceを読めません: %w", err)
	}
	if err := validateFinalizationEvidence(evidence); err != nil {
		return FinalizationEvidence{}, err
	}
	return evidence, nil
}

func (s *StateStore) ClearFinalizationEvidence() error {
	return s.Remove(finalizationEvidenceFile)
}

func validateFinalizationEvidence(evidence FinalizationEvidence) error {
	if evidence.Version != finalizationEvidenceVersion {
		return fmt.Errorf("unsupported finalization evidence version: %d", evidence.Version)
	}
	if !ValidGeneratedUUID(evidence.TaskID) {
		return fmt.Errorf("finalization evidence task IDが不正です")
	}
	if evidence.Form == "" || evidence.ValidationRunID == "" || evidence.SnapshotID == "" {
		return fmt.Errorf("finalization evidence identityが不足しています")
	}
	if evidence.RepositoryLint != ValidationResultPass {
		return fmt.Errorf("finalization evidence repository lint resultがpassではありません")
	}
	return nil
}
