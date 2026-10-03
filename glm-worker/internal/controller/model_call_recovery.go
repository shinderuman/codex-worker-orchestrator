package controller

import (
	"fmt"
	"reflect"
)

func (s *Store) validateModelCallAdmission(op ExecutionOperation) error {
	if op.Lease == nil || op.Workspace == nil || *op.Workspace != op.Source.Workspace || len(op.Transition.Effects) != 0 || op.Source.Lease.InFlightCallID != "" {
		return fmt.Errorf("model-call admission payload is incomplete")
	}
	expected := ExecutionOperation{Transition: op.Transition, Source: op.Source, Workspace: op.Workspace, Lease: op.Lease}
	if !reflect.DeepEqual(expected, op) {
		return fmt.Errorf("model-call admission has unrelated operation authority")
	}
	if err := validateLiveAuthority(op.Source.Head, op.Source.Attempt.SemanticTaskRef, op.Source.Workspace, op.Source.Snapshot, op.Source.Attempt, op.Source.Lease); err != nil {
		return err
	}
	if err := validateModelCallLeaseBinding(op); err != nil {
		return err
	}
	target := authorityFromAdmission(op.Source, op.Source.Snapshot)
	target.LeaseID = op.Lease.LeaseID
	record := buildTransitionRecord(TransitionIntent{Kind: executionModelCall, Source: op.Source, Target: target}, op.Transition.TransitionID)
	record.CreatedAt = op.Transition.CreatedAt
	record.OperationDigest = op.Transition.OperationDigest
	if !reflect.DeepEqual(record, op.Transition) {
		return fmt.Errorf("model-call admission differs from transition authority")
	}
	return s.verifyModelCallRecords(op)
}

func (s *Store) verifyModelCallRecords(op ExecutionOperation) error {
	attempt, err := s.loadAttempt(op.Source.Attempt.AttemptID)
	if err != nil {
		return err
	}
	source, err := s.loadLease(op.Source.Lease.LeaseID)
	if err != nil {
		return err
	}
	target, err := s.loadLease(op.Lease.LeaseID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(attempt, op.Source.Attempt) || !reflect.DeepEqual(source, op.Source.Lease) || !reflect.DeepEqual(target, *op.Lease) {
		return fmt.Errorf("model-call admission durable records changed")
	}
	return nil
}

func (s *Store) verifyModelCallWorkspace(op ExecutionOperation) error {
	workspace, err := ResolveWorkspaceIdentity(op.Source.Workspace.Root, s.identity)
	if err != nil {
		return err
	}
	snapshot, err := CaptureWorkspaceSnapshot(workspace.Root)
	if err != nil {
		return err
	}
	if workspace != op.Source.Workspace || snapshot != op.Source.Snapshot {
		return fmt.Errorf("model-call admission workspace changed after prepare")
	}
	return nil
}

func (s *Store) applyModelCallAdmission(op ExecutionOperation) error {
	head, err := s.LoadHead()
	if err != nil {
		return err
	}
	expected := op.Source.Head
	expected.ControllerGeneration = op.Transition.PreparedGeneration
	expected.PendingTransitionID = op.Transition.TransitionID
	if !reflect.DeepEqual(head, expected) {
		return fmt.Errorf("model-call admission source authority changed")
	}
	if err := s.verifyModelCallWorkspace(op); err != nil {
		return err
	}
	if err := s.markTransitionApplied(op.Transition, nil); err != nil {
		return err
	}
	_, err = s.commitAuthorityTransitionLocked(op.Transition, nil, false, func(next *RepositoryControllerHead) error {
		next.LiveLeaseID = op.Lease.LeaseID
		return nil
	})
	return err
}

func (s *Store) verifyCommittedModelCallAdmission(op ExecutionOperation, head RepositoryControllerHead) error {
	expected := op.Source.Head
	expected.ControllerGeneration = head.ControllerGeneration
	expected.LiveLeaseID = op.Lease.LeaseID
	if head.ControllerGeneration == op.Transition.CommittedGeneration {
		expected.PendingTransitionID = op.Transition.TransitionID
	}
	if !reflect.DeepEqual(expected, head) {
		return fmt.Errorf("committed model-call admission authority is inconsistent")
	}
	return s.verifyModelCallWorkspace(op)
}

func validateModelCallLeaseBinding(op ExecutionOperation) error {
	lease := op.Source.Lease
	lease.LeaseID = op.Lease.LeaseID
	lease.ControllerGeneration = op.Transition.TargetGeneration
	lease.InFlightCallID = op.Lease.InFlightCallID
	lease.CreatedAt = op.Lease.CreatedAt
	if lease.LeaseID == op.Source.Lease.LeaseID || lease.LeaseID == "" || lease.InFlightCallID == "" || !reflect.DeepEqual(lease, *op.Lease) {
		return fmt.Errorf("model-call admission target lease is inconsistent")
	}
	return nil
}
