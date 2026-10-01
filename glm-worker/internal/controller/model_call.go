package controller

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (s *Store) BindModelCall(admission Admission) (Admission, error) {
	targetLease, target, err := s.prepareModelCallTarget(admission)
	if err != nil {
		return Admission{}, err
	}
	record, err := s.BeginAuthorityTransition(TransitionIntent{
		Kind:               "model-call-admission",
		ExpectedGeneration: admission.Head.ControllerGeneration,
		Source:             admission,
		Target:             target,
	})
	if err != nil {
		return Admission{}, err
	}
	if err := s.verifyPreparedModelCallTransition(record); err != nil {
		return Admission{}, err
	}
	return s.finalizeModelCallTransition(admission, record, targetLease)
}

func (s *Store) prepareModelCallTarget(admission Admission) (ExecutionLease, TransitionAuthority, error) {
	callID, err := state.NewUUID()
	if err != nil {
		return ExecutionLease{}, TransitionAuthority{}, err
	}
	targetLease, err := s.prepareContinuationLease(
		admission.Lease,
		admission.Snapshot,
		admission.Head.ControllerGeneration+3,
	)
	if err != nil {
		return ExecutionLease{}, TransitionAuthority{}, err
	}
	targetLease.InFlightCallID = callID
	if err := s.writeLease(targetLease); err != nil {
		return ExecutionLease{}, TransitionAuthority{}, err
	}
	target := authorityFromAdmission(admission, admission.Snapshot)
	target.LeaseID = targetLease.LeaseID
	return targetLease, target, nil
}

func (s *Store) verifyPreparedModelCallTransition(record TransitionRecord) error {
	persisted, transitionState, err := s.LoadTransition(record.TransitionID)
	if err != nil {
		return err
	}
	if persisted.TransitionID != record.TransitionID || transitionState.Phase != TransitionPhasePrepared {
		return fmt.Errorf("model-call admission transition is not durably prepared")
	}
	return nil
}

func (s *Store) finalizeModelCallTransition(
	admission Admission,
	record TransitionRecord,
	targetLease ExecutionLease,
) (Admission, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return Admission{}, err
	}
	defer func() { _ = lock.Close() }()
	if err := s.markTransitionApplied(record, nil); err != nil {
		return Admission{}, err
	}
	if _, err := s.commitAuthorityTransitionLocked(record, nil, false, func(next *RepositoryControllerHead) error {
		next.LiveLeaseID = targetLease.LeaseID
		return nil
	}); err != nil {
		return Admission{}, err
	}
	head, err := s.finalizeAuthorityTransitionLocked(record)
	if err != nil {
		return Admission{}, err
	}
	if head.LiveLeaseID != targetLease.LeaseID || head.ControllerGeneration != targetLease.ControllerGeneration {
		return Admission{}, fmt.Errorf("model-call admission finalized with inconsistent lease authority")
	}
	return Admission{
		Head:      head,
		Attempt:   admission.Attempt,
		Lease:     targetLease,
		Workspace: admission.Workspace,
		Snapshot:  admission.Snapshot,
	}, nil
}
