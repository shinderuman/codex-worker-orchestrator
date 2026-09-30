package controller

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (s *Store) BindModelCall(admission Admission) (Admission, error) {
	callID, err := state.NewUUID()
	if err != nil {
		return Admission{}, err
	}
	targetLease, err := s.prepareContinuationLease(
		admission.Lease,
		admission.Snapshot,
		admission.Head.ControllerGeneration+3,
	)
	if err != nil {
		return Admission{}, err
	}
	targetLease.InFlightCallID = callID
	if err := s.writeLease(targetLease); err != nil {
		return Admission{}, err
	}
	target := authorityFromAdmission(admission, admission.Snapshot)
	target.LeaseID = targetLease.LeaseID
	record, err := s.BeginAuthorityTransition(TransitionIntent{
		Kind:               "model-call-admission",
		ExpectedGeneration: admission.Head.ControllerGeneration,
		Source:             admission,
		Target:             target,
	})
	if err != nil {
		return Admission{}, err
	}
	persisted, transitionState, err := s.LoadTransition(record.TransitionID)
	if err != nil {
		return Admission{}, err
	}
	if persisted.TransitionID != record.TransitionID || transitionState.Phase != TransitionPhasePrepared {
		return Admission{}, fmt.Errorf("model-call admission transition is not durably prepared")
	}
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
