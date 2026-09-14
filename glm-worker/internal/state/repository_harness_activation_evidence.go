package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const repositoryHarnessActivationEvidenceVersion = 1

const repositoryHarnessActivationEvidenceDir = "repository-harness-activation"

type RepositoryHarnessActivationEvidence struct {
	Version int    `json:"version"`
	TaskID  string `json:"task_id"`
	Active  bool   `json:"active"`
}

func (s *StateStore) RecordRepositoryHarnessActivation(active bool) error {
	taskID, err := s.TaskID()
	if err != nil {
		return err
	}
	evidence := RepositoryHarnessActivationEvidence{
		Version: repositoryHarnessActivationEvidenceVersion,
		TaskID:  taskID,
		Active:  active,
	}
	data, err := marshalCurrentStateJSON(evidence)
	if err != nil {
		return fmt.Errorf("repository harness activation evidenceをJSON化できません: %w", err)
	}
	if err := writeFileAtomic(s.repositoryHarnessActivationEvidencePath(taskID), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("repository harness activation evidenceを書き込めません: %w", err)
	}
	return nil
}

func (s *StateStore) ReadRepositoryHarnessActivation(taskID string) (bool, bool, error) {
	data, err := os.ReadFile(s.repositoryHarnessActivationEvidencePath(taskID))
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("repository harness activation evidenceを読み込めません: %w", err)
	}
	var evidence RepositoryHarnessActivationEvidence
	if err := decodeCurrentStateJSON(data, &evidence); err != nil {
		return false, false, fmt.Errorf("repository harness activation evidenceが不正です: %w", err)
	}
	if evidence.Version != repositoryHarnessActivationEvidenceVersion {
		return false, false, fmt.Errorf("repository harness activation evidence versionが不正です: %d", evidence.Version)
	}
	if evidence.TaskID != taskID {
		return false, false, fmt.Errorf("repository harness activation evidence task IDが不一致です: got %q want %q", evidence.TaskID, taskID)
	}
	return evidence.Active, true, nil
}

func WarnRepositoryHarnessActivationEvidenceSkip(err error) {
	writeStatsWarningEvent("repository_harness_activation", "repository harness activation evidenceの保存に失敗したためhistorical classificationではunknownとして扱います", err)
}

func (s *StateStore) repositoryHarnessActivationEvidencePath(taskID string) string {
	return filepath.Join(s.dir, repositoryHarnessActivationEvidenceDir, taskID+".json")
}
