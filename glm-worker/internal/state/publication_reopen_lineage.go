package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

type PublicationReopenLineage struct {
	Version   int    `json:"version"`
	TaskID    string `json:"task_id"`
	TaskPath  string `json:"task_path"`
	BaseHead  string `json:"base_head"`
	CommitOID string `json:"commit_oid"`
}

const (
	publicationReopenLineageStateFile = "publication-reopen-lineage.json"
	publicationReopenLineageVersion   = 1
)

func (s *StateStore) CapturePublicationReopenLineage(candidate PublicationCandidate) error {
	existing, err := s.LoadPublicationReopenLineage()
	if err == nil {
		return s.validateCurrentPublicationReopenLineage(existing)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	taskID, err := s.TaskID()
	if err != nil {
		return err
	}
	if candidate.TaskID != taskID {
		return fmt.Errorf("publication reopen candidate task %s does not match current task %s", candidate.TaskID, taskID)
	}
	taskPath, err := s.CurrentTaskAuthorityPath()
	if err != nil {
		return fmt.Errorf("publication reopen task authority unavailable: %w", err)
	}
	lineage := PublicationReopenLineage{
		Version:   publicationReopenLineageVersion,
		TaskID:    taskID,
		TaskPath:  taskPath,
		BaseHead:  candidate.BaseHead,
		CommitOID: candidate.CommitOID,
	}
	if err := validatePublicationReopenLineage(lineage); err != nil {
		return err
	}
	data, err := json.Marshal(lineage)
	if err != nil {
		return fmt.Errorf("publication reopen lineageをJSON化できません: %w", err)
	}
	return s.Write(publicationReopenLineageStateFile, string(data))
}

func (s *StateStore) LoadPublicationReopenLineage() (PublicationReopenLineage, error) {
	data, err := s.Read(publicationReopenLineageStateFile)
	if err != nil {
		return PublicationReopenLineage{}, err
	}
	var lineage PublicationReopenLineage
	if err := json.Unmarshal([]byte(data), &lineage); err != nil {
		return PublicationReopenLineage{}, fmt.Errorf("publication reopen lineageを読めません: %w", err)
	}
	if err := validatePublicationReopenLineage(lineage); err != nil {
		return PublicationReopenLineage{}, err
	}
	return lineage, nil
}

func (s *StateStore) validateCurrentPublicationReopenLineage(lineage PublicationReopenLineage) error {
	taskID, err := s.TaskID()
	if err != nil {
		return err
	}
	if lineage.TaskID != taskID {
		return fmt.Errorf("publication reopen lineage task %s does not match current task %s", lineage.TaskID, taskID)
	}
	taskPath, err := s.CurrentTaskAuthorityPath()
	if err != nil {
		return fmt.Errorf("publication reopen task authority unavailable: %w", err)
	}
	if lineage.TaskPath != taskPath {
		return fmt.Errorf("publication reopen lineage task path %s does not match canonical task authority %s", lineage.TaskPath, taskPath)
	}
	return nil
}

func validatePublicationReopenLineage(lineage PublicationReopenLineage) error {
	if lineage.Version != publicationReopenLineageVersion {
		return fmt.Errorf("unsupported publication reopen lineage version: %d", lineage.Version)
	}
	if !ValidGeneratedUUID(lineage.TaskID) {
		return fmt.Errorf("publication reopen lineage task IDが不正です")
	}
	if strings.TrimSpace(lineage.TaskPath) == "" || strings.ContainsAny(lineage.TaskPath, "\r\n") {
		return fmt.Errorf("publication reopen lineage task pathが不正です")
	}
	if !validPublicationOID(lineage.BaseHead) || !validPublicationOID(lineage.CommitOID) {
		return fmt.Errorf("publication reopen lineage git identityが不正です")
	}
	return nil
}
