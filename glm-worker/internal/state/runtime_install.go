package state

import (
	"encoding/json"
	"fmt"
	"os"
)

const (
	runtimeInstallEvidenceFile    = "runtime-install-evidence.json"
	runtimeInstallEvidenceVersion = 1
)

type RuntimeInstallEvidence struct {
	Version           int    `json:"version"`
	TaskID            string `json:"task_id"`
	Head              string `json:"head"`
	SourceDigest      string `json:"source_digest"`
	InstalledRevision string `json:"installed_revision"`
	SmokeResult       string `json:"smoke_result"`
}

func (s *StateStore) SaveRuntimeInstallEvidence(evidence RuntimeInstallEvidence) error {
	if err := validateRuntimeInstallEvidence(evidence); err != nil {
		return err
	}
	taskID, err := s.TaskID()
	if err != nil {
		return err
	}
	if evidence.TaskID != taskID {
		return fmt.Errorf("runtime install evidence task %s does not match current task %s", evidence.TaskID, taskID)
	}
	data, err := json.Marshal(evidence)
	if err != nil {
		return fmt.Errorf("runtime install evidenceをJSON化できません: %w", err)
	}
	if err := writeFileAtomic(s.Path(runtimeInstallEvidenceFile), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("runtime install evidenceを書き込めません: %w", err)
	}
	return nil
}

func (s *StateStore) LoadRuntimeInstallEvidence() (RuntimeInstallEvidence, error) {
	data, err := os.ReadFile(s.Path(runtimeInstallEvidenceFile))
	if err != nil {
		return RuntimeInstallEvidence{}, err
	}
	var evidence RuntimeInstallEvidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		return RuntimeInstallEvidence{}, fmt.Errorf("runtime install evidenceを読めません: %w", err)
	}
	if err := validateRuntimeInstallEvidence(evidence); err != nil {
		return RuntimeInstallEvidence{}, err
	}
	return evidence, nil
}

func (s *StateStore) ClearRuntimeInstallEvidence() error {
	return s.Remove(runtimeInstallEvidenceFile)
}

func validateRuntimeInstallEvidence(evidence RuntimeInstallEvidence) error {
	if evidence.Version != runtimeInstallEvidenceVersion {
		return fmt.Errorf("unsupported runtime install evidence version: %d", evidence.Version)
	}
	if !ValidGeneratedUUID(evidence.TaskID) {
		return fmt.Errorf("runtime install evidence task IDが不正です")
	}
	if evidence.Head == "" || evidence.SourceDigest == "" || evidence.InstalledRevision == "" {
		return fmt.Errorf("runtime install evidence identityが不足しています")
	}
	if evidence.InstalledRevision != evidence.Head {
		return fmt.Errorf("runtime install evidence installed revisionがsource HEADと一致しません")
	}
	if evidence.SmokeResult != ValidationResultPass {
		return fmt.Errorf("runtime install evidence smoke resultがpassではありません")
	}
	return nil
}
