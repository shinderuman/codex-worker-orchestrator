package controller

import (
	"fmt"
	"reflect"
)

func (s *Store) PreparedCompletionAuthority(head RepositoryControllerHead) (SemanticTaskRef, bool, error) {
	if head.PendingTransitionID == "" {
		return SemanticTaskRef{}, false, nil
	}
	pending, err := s.PendingCompletionRecovery(head)
	if err != nil || !pending {
		return SemanticTaskRef{}, pending, err
	}
	var op ExecutionOperation
	if err := readJSON(s.executionOperationPath(head.PendingTransitionID), &op); err != nil {
		return SemanticTaskRef{}, true, err
	}
	if op.Terminal == nil {
		return SemanticTaskRef{}, false, nil
	}
	phase, err := s.loadTransitionState(head.PendingTransitionID)
	if err != nil {
		return SemanticTaskRef{}, true, err
	}
	if err := s.validatePreparedCompletionAuthority(op, phase); err != nil {
		return SemanticTaskRef{}, true, err
	}
	return *op.Terminal.RootTaskRef, true, nil
}

func (s *Store) PendingCompletionRecovery(head RepositoryControllerHead) (bool, error) {
	return s.pendingTransitionRecovery(head, pendingCompletionRecoveryKind)
}

func (s *Store) PendingAdvancementRecovery(head RepositoryControllerHead) (bool, error) {
	return s.pendingTransitionRecovery(head, pendingAdvancementRecoveryKind)
}

func pendingCompletionRecoveryKind(kind string) bool {
	switch kind {
	case publicationPromote, publicationPublish, executionTaskAuthoritySync, executionRuntimeDisposition:
		return true
	default:
		return false
	}
}

func pendingAdvancementRecoveryKind(kind string) bool {
	switch kind {
	case publicationAdopt, publicationAdoptSource:
		return true
	default:
		return false
	}
}

func (s *Store) pendingTransitionRecovery(head RepositoryControllerHead, recoverableKind func(string) bool) (bool, error) {
	if head.PendingTransitionID == "" {
		return false, nil
	}
	record, phase, err := s.LoadTransition(head.PendingTransitionID)
	if err != nil {
		return true, err
	}
	if !recoverableKind(record.Kind) {
		return false, nil
	}
	var op ExecutionOperation
	if err := readJSON(s.executionOperationPath(record.TransitionID), &op); err != nil {
		return true, err
	}
	if !reflect.DeepEqual(record, op.Transition) {
		return true, fmt.Errorf("pending transition recovery journal differs from transition")
	}
	if _, err := s.validateExecutionOperation(op); err != nil {
		return true, err
	}
	current, _, err := s.loadExecutionRecoveryAuthority(op)
	if err != nil {
		return true, err
	}
	if !reflect.DeepEqual(current, head) {
		return true, fmt.Errorf("pending transition recovery controller changed")
	}
	return true, validateCompletionRecoveryGeneration(op, head, phase)
}

func validateCompletionRecoveryGeneration(op ExecutionOperation, head RepositoryControllerHead, phase TransitionState) error {
	record := op.Transition
	if executionPreparedForRecovery(op, head) {
		if head.ProjectSnapshotID != record.ProjectSnapshotOld || (phase.Phase != TransitionPhasePrepared && phase.Phase != TransitionPhaseApplied && phase.Phase != TransitionPhaseAborted) {
			return fmt.Errorf("prepared completion recovery authority differs")
		}
		return nil
	}
	if head.ControllerGeneration != record.CommittedGeneration || head.ProjectSnapshotID != record.ProjectSnapshotNew || (phase.Phase != TransitionPhasePrepared && phase.Phase != TransitionPhaseApplied && phase.Phase != TransitionPhaseCommitted && phase.Phase != TransitionPhaseFinalizing) {
		return fmt.Errorf("committed completion recovery authority differs")
	}
	return nil
}

func (s *Store) validatePreparedCompletionAuthority(op ExecutionOperation, phase TransitionState) error {
	if phase.Completion != nil && phase.Completion.ObservationRef != nil {
		if _, err := s.loadCompletionReadiness(op, phase); err != nil {
			return err
		}
		if _, err := s.completionObservation(op, phase); err != nil {
			return err
		}
		if err := s.verifyCompletionLocalTarget(op); err != nil {
			return err
		}
	} else if _, err := s.validateCompletionReadiness(op, phase); err != nil {
		return err
	}
	return nil
}
