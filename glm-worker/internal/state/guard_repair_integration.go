package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

type GuardRepairIntegrationFile struct {
	Path    string `json:"path"`
	Content []byte `json:"content,omitempty"`
	Mode    uint32 `json:"mode,omitempty"`
	Exists  bool   `json:"exists"`
}

type GuardRepairIntegrationJournal struct {
	Version            int                          `json:"version"`
	TaskID             string                       `json:"task_id"`
	Phase              string                       `json:"phase"`
	Fingerprint        string                       `json:"fingerprint"`
	Strategy           string                       `json:"strategy"`
	RelevantDigest     string                       `json:"relevant_digest"`
	ResumeCheckpoint   ResumeCheckpoint             `json:"resume_checkpoint"`
	RepositoryBoundary GitSnapshot                  `json:"repository_boundary"`
	Files              []GuardRepairIntegrationFile `json:"files"`
	UpdatedAt          time.Time                    `json:"updated_at"`
}

const (
	guardRepairIntegrationStateFile    = "guard-repair-integration.json"
	guardRepairIntegrationStateVersion = 1
)

var ErrNoGuardRepairIntegrationJournal = errors.New("guard repair integration journal is not available")

func (journal GuardRepairIntegrationJournal) validate() error {
	if journal.TaskID == "" || journal.Phase == "" || journal.Fingerprint == "" || journal.Strategy == "" || journal.RelevantDigest == "" {
		return fmt.Errorf("guard repair integration journal provenance is incomplete")
	}
	if journal.ResumeCheckpoint.StopKind != ResumeStopGuardRecoverable || journal.ResumeCheckpoint.Phase != journal.Phase || journal.ResumeCheckpoint.StopGitSnapshot == nil {
		return fmt.Errorf("guard repair integration journal checkpoint provenance is invalid")
	}
	if journal.RepositoryBoundary.Head == "" || journal.RepositoryBoundary.IndexDigest == "" || journal.RepositoryBoundary.WorktreeDigest == "" || journal.RepositoryBoundary.ParentFiles == nil {
		return fmt.Errorf("guard repair integration journal repository boundary is incomplete")
	}
	if len(journal.Files) == 0 {
		return fmt.Errorf("guard repair integration journal has no file preimages")
	}
	seen := make(map[string]struct{}, len(journal.Files))
	for _, file := range journal.Files {
		if file.Path == "" {
			return fmt.Errorf("guard repair integration journal has an empty file path")
		}
		if _, ok := seen[file.Path]; ok {
			return fmt.Errorf("guard repair integration journal has duplicate file path %s", file.Path)
		}
		seen[file.Path] = struct{}{}
		if file.Mode&^uint32(0o777) != 0 {
			return fmt.Errorf("guard repair integration journal has invalid mode for %s", file.Path)
		}
		if !file.Exists && (len(file.Content) != 0 || file.Mode != 0) {
			return fmt.Errorf("guard repair integration journal has content for absent file %s", file.Path)
		}
	}
	return nil
}

func (s *StateStore) SaveGuardRepairIntegrationJournal(journal GuardRepairIntegrationJournal) error {
	if err := journal.validate(); err != nil {
		return err
	}
	journal.Version = guardRepairIntegrationStateVersion
	journal.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return fmt.Errorf("guard repair integration journalをJSON化できません: %w", err)
	}
	if err := writeFileAtomic(s.Path(guardRepairIntegrationStateFile), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("guard repair integration journalを書き込めません: %w", err)
	}
	return nil
}

func (s *StateStore) LoadGuardRepairIntegrationJournal() (GuardRepairIntegrationJournal, error) {
	data, err := os.ReadFile(s.Path(guardRepairIntegrationStateFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return GuardRepairIntegrationJournal{}, ErrNoGuardRepairIntegrationJournal
		}
		return GuardRepairIntegrationJournal{}, err
	}
	var journal GuardRepairIntegrationJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		return GuardRepairIntegrationJournal{}, fmt.Errorf("guard repair integration journalを読めません: %w", err)
	}
	if journal.Version != guardRepairIntegrationStateVersion {
		return GuardRepairIntegrationJournal{}, fmt.Errorf("unsupported guard repair integration journal version: %d", journal.Version)
	}
	if err := journal.validate(); err != nil {
		return GuardRepairIntegrationJournal{}, err
	}
	return journal, nil
}

func (s *StateStore) RemoveGuardRepairIntegrationJournal() error {
	if err := removeStatePath(s.Path(guardRepairIntegrationStateFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("guard repair integration journalを削除できません: %w", err)
	}
	return nil
}
