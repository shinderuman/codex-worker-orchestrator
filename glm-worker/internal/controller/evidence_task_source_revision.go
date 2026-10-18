package controller

import (
	"fmt"
	"strings"
	"time"
)

type TaskSourceRevisionRecord struct {
	SchemaVersion             int                 `json:"schema_version"`
	SourceRevisionID          string              `json:"source_revision_id"`
	AttemptID                 string              `json:"attempt_id"`
	PreviousTaskRef           SemanticTaskRef     `json:"previous_task_ref"`
	RevisedTaskRef            SemanticTaskRef     `json:"revised_task_ref"`
	PreviousProjectSnapshotID string              `json:"previous_project_snapshot_id"`
	RevisedProjectSnapshotID  string              `json:"revised_project_snapshot_id"`
	SourceSnapshotID          string              `json:"source_snapshot_id"`
	TransitionID              string              `json:"transition_id"`
	ControllerGeneration      uint64              `json:"controller_generation"`
	EvidenceRefs              []EvidenceObjectRef `json:"evidence_refs,omitempty"`
	CreatedAt                 time.Time           `json:"created_at"`
}

func (s *Store) StoreTaskSourceRevision(record TaskSourceRevisionRecord) (EvidenceObjectRef, TaskSourceRevisionRecord, error) {
	if err := validateTaskSourceRevision(record); err != nil {
		return EvidenceObjectRef{}, TaskSourceRevisionRecord{}, err
	}
	record.EvidenceRefs = canonicalEvidenceRefs(record.EvidenceRefs)
	record.SourceRevisionID = ""
	id, err := evidenceRecordDigest(record)
	if err != nil {
		return EvidenceObjectRef{}, TaskSourceRevisionRecord{}, err
	}
	record.SourceRevisionID = id
	ref, err := s.putEvidenceJSON("task-source-revision", taskSourceRevisionIdentity(record), record)
	return ref, record, err
}

func (s *Store) LoadTaskSourceRevision(ref EvidenceObjectRef) (TaskSourceRevisionRecord, error) {
	return loadEvidenceRecord(
		s,
		ref,
		"task-source-revision",
		func(record *TaskSourceRevisionRecord) (string, string) {
			id := record.SourceRevisionID
			record.SourceRevisionID = ""
			return id, taskSourceRevisionIdentity(*record)
		},
		func(record *TaskSourceRevisionRecord, id string) { record.SourceRevisionID = id },
		validateTaskSourceRevision,
		"task source revision identity is inconsistent",
	)
}

func taskSourceRevisionIdentity(record TaskSourceRevisionRecord) string {
	return record.AttemptID + ":" + record.TransitionID
}

func validateTaskSourceRevision(record TaskSourceRevisionRecord) error {
	if err := validateTaskSourceRevisionIdentity(record); err != nil {
		return err
	}
	return validateTaskSourceRevisionRefs(record)
}

func validateTaskSourceRevisionIdentity(record TaskSourceRevisionRecord) error {
	if record.SchemaVersion != evidenceSchemaVersion || strings.TrimSpace(record.AttemptID) == "" || strings.TrimSpace(record.TransitionID) == "" || record.ControllerGeneration == 0 || record.CreatedAt.IsZero() {
		return fmt.Errorf("task source revision identity is incomplete")
	}
	return validateTaskSourceRevisionAuthority(record)
}

func validateTaskSourceRevisionAuthority(record TaskSourceRevisionRecord) error {
	if record.PreviousTaskRef.Empty() || record.RevisedTaskRef.Empty() || record.PreviousTaskRef.TaskPath != record.RevisedTaskRef.TaskPath {
		return fmt.Errorf("task source revision changes the Task subject")
	}
	if strings.TrimSpace(record.PreviousProjectSnapshotID) == "" || strings.TrimSpace(record.RevisedProjectSnapshotID) == "" || strings.TrimSpace(record.SourceSnapshotID) == "" {
		return fmt.Errorf("task source revision source identity is incomplete")
	}
	return nil
}

func validateTaskSourceRevisionRefs(record TaskSourceRevisionRecord) error {
	for _, ref := range record.EvidenceRefs {
		if err := validateEvidenceRef(ref); err != nil {
			return fmt.Errorf("task source revision evidence reference: %w", err)
		}
	}
	return nil
}
