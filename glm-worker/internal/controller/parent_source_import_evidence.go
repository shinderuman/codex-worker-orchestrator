package controller

import (
	"fmt"
)

const taskStatusExecuting = "executing"

func (s *Store) parentSourceImportEvidence(op ExecutionOperation) (EvidencePublicationInput, error) {
	if op.Attempt == nil || op.Attempt.AttemptID == "" || op.Attempt.AttemptID != op.Source.Attempt.AttemptID {
		return EvidencePublicationInput{}, fmt.Errorf("parent source import evidence requires the same live attempt")
	}
	if op.Attempt.SemanticTaskRef.TaskPath != op.Source.Attempt.SemanticTaskRef.TaskPath {
		return EvidencePublicationInput{}, fmt.Errorf("parent source import revises another Task")
	}
	worktree, err := captureParentSourceTree(op.Source.Workspace.Root)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	index, err := gitTrimmed(op.Source.Workspace.Root, "write-tree")
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	archive, _, err := s.CaptureGitObjectArchive(op.Source.Workspace.Root, op.Transition.TransitionID+":source-before", []string{op.Source.Snapshot.Head, index, worktree})
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	sourceRef, err := s.putEvidenceJSON("runtime-artifact", op.Transition.TransitionID+":parent-source", *op.ParentSource)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	revisionRef, _, err := s.StoreTaskSourceRevision(TaskSourceRevisionRecord{
		SchemaVersion:             evidenceSchemaVersion,
		AttemptID:                 op.Source.Attempt.AttemptID,
		PreviousTaskRef:           op.Source.Attempt.SemanticTaskRef,
		RevisedTaskRef:            op.Attempt.SemanticTaskRef,
		PreviousProjectSnapshotID: op.Transition.ProjectSnapshotOld,
		RevisedProjectSnapshotID:  op.Transition.ProjectSnapshotNew,
		SourceSnapshotID:          op.ParentSource.Snapshot.ID,
		TransitionID:              op.Transition.TransitionID,
		ControllerGeneration:      op.Transition.CommittedGeneration,
		EvidenceRefs:              []EvidenceObjectRef{archive, sourceRef, op.ParentSource.Archive, *op.RuntimeBeforeRef},
		CreatedAt:                 op.Transition.CreatedAt,
	})
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	return s.appendTaskSourceRevisionIndex(op, revisionRef)
}

func (s *Store) appendTaskSourceRevisionIndex(op ExecutionOperation, revisionRef EvidenceObjectRef) (EvidencePublicationInput, error) {
	task := TaskIndexRevision{SchemaVersion: evidenceSchemaVersion, TaskRef: op.Attempt.SemanticTaskRef, SemanticStatus: taskStatusExecuting}
	if current, ok, err := s.publishedTaskSubjectRevision(op.Source.Head, op.Attempt.SemanticTaskRef); err != nil {
		return EvidencePublicationInput{}, err
	} else if ok {
		loaded, err := s.LoadTaskIndexRevision(current)
		if err != nil {
			return EvidencePublicationInput{}, err
		}
		task = loaded
		task.PreviousRevision = &current
	}
	task.SourceRevisions = append(task.SourceRevisions, revisionRef)
	task.ControllerGeneration = op.Transition.CommittedGeneration
	task.TransitionID = op.Transition.TransitionID
	task.CreatedAt = op.Transition.CreatedAt
	taskRef, _, err := s.StoreTaskIndexRevision(task)
	return EvidencePublicationInput{
		TaskRevisionRefs:   []EvidenceObjectRef{taskRef},
		SourceRevisionRefs: []EvidenceObjectRef{revisionRef},
	}, err
}

func (s *Store) publishedTaskSubjectRevision(head RepositoryControllerHead, taskRef SemanticTaskRef) (EvidenceObjectRef, bool, error) {
	if head.EvidenceHeadRef == nil {
		return EvidenceObjectRef{}, false, nil
	}
	evidenceHead, err := s.LoadEvidenceHead(*head.EvidenceHeadRef)
	if err != nil {
		return EvidenceObjectRef{}, false, err
	}
	subject, ok := evidenceSubjectHead(evidenceHead.TaskHeads, taskEvidenceSubjectID(taskRef))
	if !ok {
		return EvidenceObjectRef{}, false, nil
	}
	return subject.RevisionRef, true, nil
}

func (s *Store) publishedTaskSourceRevisionRefs(taskRef SemanticTaskRef) ([]EvidenceObjectRef, error) {
	head, err := s.LoadHead()
	if err != nil {
		return nil, err
	}
	current, ok, err := s.publishedTaskSubjectRevision(head, taskRef)
	if err != nil || !ok {
		return nil, err
	}
	revision, err := s.LoadTaskIndexRevision(current)
	if err != nil {
		return nil, err
	}
	return revision.SourceRevisions, nil
}

func (s *Store) ReentryReviewRepairEvidence(transitionID string) ([]byte, error) {
	var op ExecutionOperation
	if err := readJSON(s.executionOperationPath(transitionID), &op); err != nil {
		return nil, err
	}
	if op.Transition.Kind != executionParentSourceImport || !parentSourceRuntimeIdentity(op, "reentry-review-repair") {
		return nil, nil
	}
	head, err := s.LoadHead()
	if err != nil {
		return nil, err
	}
	if _, err := s.validateExecutionOperation(op); err != nil {
		return nil, err
	}
	if !executionAlreadyFinalized(op, head) {
		return nil, fmt.Errorf("review repair source import is not finalized")
	}
	if err := s.verifyCommittedParentSourceImport(op, head); err != nil {
		return nil, err
	}
	return s.LoadEvidenceObject(*op.RuntimeBeforeRef)
}

func parentSourceRuntimeIdentity(op ExecutionOperation, producer string) bool {
	return op.ParentSource != nil && op.RuntimeBeforeRef != nil && op.RuntimeBeforeRef.LogicalIdentity == op.Source.Attempt.AttemptID+":"+producer+":"+op.ParentSource.Snapshot.ID
}
