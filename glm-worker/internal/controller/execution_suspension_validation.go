package controller

import "fmt"

func (s *Store) validateSuspensionOperation(op ExecutionOperation) error {
	if op.Suspension == nil || op.Episode == nil || op.SealRef == nil {
		return fmt.Errorf("suspension operation is incomplete")
	}
	workspace, err := ResolveWorkspaceIdentity(op.Source.Workspace.Root, s.identity)
	if err != nil {
		return err
	}
	if workspace != op.Source.Workspace || workspace.ID != op.Transition.SourceWorkspaceID {
		return fmt.Errorf("suspension source workspace is inconsistent")
	}
	if op.Transition.Effects[0].Resource != suspensionRef(op.Suspension.SnapshotID) || op.Transition.Effects[0].ExpectedNew != op.Suspension.RetainedCommitOID {
		return fmt.Errorf("suspension retention intent is inconsistent")
	}
	return nil
}

func (s *Store) verifyCommittedSuspension(op ExecutionOperation, head RepositoryControllerHead) error {
	actual, err := CaptureWorkspaceSnapshot(op.Source.Workspace.Root)
	if err != nil {
		return err
	}
	if actual != op.Transition.WorkspaceSnapshotNew || head.LiveLeaseID != "" {
		return fmt.Errorf("committed suspension state is unexpected")
	}
	_, err = s.ProveCleanupDurability(*op.SealRef)
	return err
}
