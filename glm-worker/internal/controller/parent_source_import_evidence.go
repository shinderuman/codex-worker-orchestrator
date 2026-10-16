package controller

import (
	"fmt"
)

func (s *Store) parentSourceImportEvidence(op ExecutionOperation) (EvidencePublicationInput, error) {
	previous, err := s.loadAttempt(op.Source.Attempt.PredecessorAttemptID)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	if previous.SemanticTaskRef.TaskPath != op.Source.Attempt.SemanticTaskRef.TaskPath {
		return EvidencePublicationInput{}, fmt.Errorf("parent source import predecessor belongs to another Task")
	}
	sealRef, _, err := s.findAttemptSeal(op.Source.Head, previous)
	if err != nil {
		return EvidencePublicationInput{}, err
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
	refs := []EvidenceObjectRef{archive, sourceRef, op.ParentSource.Archive, *op.RuntimeBeforeRef}
	final, err := s.storeOperationFinalization(op, sealRef, refs)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	return s.appendParentSourceFinalization(op, previous, final)
}

func (s *Store) appendParentSourceFinalization(op ExecutionOperation, previous AttemptRecord, final EvidenceObjectRef) (EvidencePublicationInput, error) {
	ref, err := s.attemptTaskEvidenceRoot(op.Source.Head, previous)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	task, err := s.LoadTaskIndexRevision(ref)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	task.PreviousRevision = &ref
	task.Finalizations = append(task.Finalizations, final)
	task.ControllerGeneration = op.Transition.CommittedGeneration
	task.TransitionID = op.Transition.TransitionID
	task.CreatedAt = op.Transition.CreatedAt
	taskRef, _, err := s.StoreTaskIndexRevision(task)
	return EvidencePublicationInput{FinalizationRefs: []EvidenceObjectRef{final}, TaskRevisionRefs: []EvidenceObjectRef{taskRef}}, err
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
