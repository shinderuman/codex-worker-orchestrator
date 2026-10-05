package controller

import "fmt"

func (s *Store) validateMaterializationOperation(op ExecutionOperation) error {
	if op.Workspace == nil || op.Rebound == nil || op.Attempt == nil || op.Lease == nil {
		return fmt.Errorf("materialization operation is incomplete")
	}
	if err := s.validateExecutionLaneLocation(*op.Workspace); err != nil {
		return err
	}
	if op.Transition.Effects[0].ExpectedNew != laneMaterializationIdentity(*op.Workspace, *op.Rebound) || op.Transition.TargetWorkspaceID != op.Workspace.ID || op.Transition.TargetAttemptID != op.Attempt.AttemptID || op.Transition.TargetLeaseID != op.Lease.LeaseID {
		return fmt.Errorf("materialization plan differs from journal authority")
	}
	return nil
}

func (s *Store) verifyCommittedMaterialization(op ExecutionOperation, head RepositoryControllerHead) error {
	if head.LiveLeaseID != op.Lease.LeaseID || head.LiveAttemptID != op.Attempt.AttemptID {
		return fmt.Errorf("committed execution bindings differ from operation")
	}
	workspace, err := ResolveWorkspaceIdentity(op.Workspace.Root, s.identity)
	if err != nil {
		return err
	}
	if workspace != *op.Workspace {
		return fmt.Errorf("committed execution workspace was replaced")
	}
	lease, err := s.loadLease(head.LiveLeaseID)
	if err != nil {
		return err
	}
	snapshot, err := CaptureWorkspaceSnapshot(workspace.Root)
	if err != nil {
		return err
	}
	if snapshot.ID != lease.ExpectedWorkspaceSnapshotID || snapshot.Head != lease.ExpectedBaseOID {
		return fmt.Errorf("committed execution workspace changed unexpectedly")
	}
	return nil
}
