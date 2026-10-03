package controller

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const executionModelCall = "model-call-admission"

func (s *Store) BindModelCall(admission Admission) (Admission, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return Admission{}, err
	}
	defer func() { _ = lock.Close() }()
	op, err := s.planModelCallAdmission(admission)
	if err != nil {
		return Admission{}, err
	}
	if err := s.prepareExecutionOperation(&op, admission.Head); err != nil {
		return Admission{}, err
	}
	result, err := s.recoverExecutionOperationLocked(op)
	if err != nil {
		return Admission{}, err
	}
	if result.Admission == nil {
		return Admission{}, fmt.Errorf("model-call admission has no live authority")
	}
	return *result.Admission, nil
}

func (s *Store) planModelCallAdmission(admission Admission) (ExecutionOperation, error) {
	if admission.Lease.InFlightCallID != "" {
		return ExecutionOperation{}, fmt.Errorf("model-call admission requires a quiescent lease")
	}
	if err := s.validateTransitionSource(TransitionIntent{ExpectedGeneration: admission.Head.ControllerGeneration, Source: admission}); err != nil {
		return ExecutionOperation{}, err
	}
	lease, target, err := s.prepareModelCallTarget(admission)
	if err != nil {
		return ExecutionOperation{}, err
	}
	id, err := state.NewUUID()
	if err != nil {
		return ExecutionOperation{}, err
	}
	record := buildTransitionRecord(TransitionIntent{Kind: executionModelCall, Source: admission, Target: target}, id)
	workspace := admission.Workspace
	return ExecutionOperation{Transition: record, Source: admission, Lease: &lease, Workspace: &workspace}, nil
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
