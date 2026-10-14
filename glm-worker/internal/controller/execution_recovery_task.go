package controller

import (
	"fmt"
	"reflect"
)

func (s *Store) PendingExecutionRecoveryTask(head RepositoryControllerHead) (SemanticTaskRef, error) {
	if head.Status != ControllerStatusActive || head.PendingTransitionID == "" || head.ExecutionTaskRef == nil {
		return SemanticTaskRef{}, fmt.Errorf("controller has no pending execution recovery authority")
	}
	var op ExecutionOperation
	if err := readJSON(s.executionOperationPath(head.PendingTransitionID), &op); err != nil {
		return SemanticTaskRef{}, err
	}
	record, phase, err := s.LoadTransition(head.PendingTransitionID)
	if err != nil {
		return SemanticTaskRef{}, err
	}
	if !reflect.DeepEqual(op.Transition, record) {
		return SemanticTaskRef{}, fmt.Errorf("pending execution journal differs")
	}
	if _, err := s.validateExecutionOperation(op); err != nil {
		return SemanticTaskRef{}, err
	}
	if err := pendingExecutionTaskMatchesHead(head, record, phase); err != nil {
		return SemanticTaskRef{}, err
	}
	return *head.ExecutionTaskRef, nil
}

func pendingExecutionTaskMatchesHead(head RepositoryControllerHead, record TransitionRecord, phase TransitionState) error {
	if head.ControllerGeneration == record.PreparedGeneration && (phase.Phase == TransitionPhasePrepared || phase.Phase == TransitionPhaseApplied) {
		if pendingExecutionIdentityMatches(head, record.SourceExecutionTaskRef, record.SourceAttemptID, record.SourceLeaseID) {
			return nil
		}
	}
	if head.ControllerGeneration == record.CommittedGeneration && (phase.Phase == TransitionPhaseCommitted || phase.Phase == TransitionPhaseFinalizing) {
		if pendingExecutionIdentityMatches(head, record.TargetExecutionTaskRef, record.TargetAttemptID, record.TargetLeaseID) {
			return nil
		}
	}
	return fmt.Errorf("pending execution recovery Task authority differs")
}

func pendingExecutionIdentityMatches(head RepositoryControllerHead, task SemanticTaskRef, attempt, lease string) bool {
	return head.ExecutionTaskRef.Equal(task) && head.LiveAttemptID == attempt && head.LiveLeaseID == lease
}
