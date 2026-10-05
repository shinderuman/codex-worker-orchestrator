package controller

import (
	"fmt"
	"os"
)

func (s *Store) validateCleanupOperation(op ExecutionOperation) error {
	if op.Workspace == nil || op.CleanupSnapshot == nil || op.SealRef == nil {
		return fmt.Errorf("cleanup operation is incomplete")
	}
	if err := s.validateExecutionLaneLocation(*op.Workspace); err != nil {
		return err
	}
	if op.Transition.Effects[0].Resource != op.Workspace.Root || op.Transition.Effects[0].ExpectedOld != op.CleanupSnapshot.ID {
		return fmt.Errorf("cleanup intent is inconsistent")
	}
	return nil
}

func validateSuspensionGCOperation(op ExecutionOperation) error {
	if op.Suspension == nil || op.SealRef == nil || op.Transition.Effects[0].Resource != suspensionRef(op.Suspension.SnapshotID) || op.Transition.Effects[0].ExpectedOld != op.Suspension.RetainedCommitOID {
		return fmt.Errorf("suspension GC authority is inconsistent")
	}
	return nil
}

func (*Store) verifyCommittedCleanup(op ExecutionOperation, _ RepositoryControllerHead) error {
	if op.Workspace == nil {
		return fmt.Errorf("cleanup operation is incomplete")
	}
	for _, path := range []string{op.Workspace.Root, op.Workspace.GitDir} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return fmt.Errorf("committed cleanup target was recreated")
		}
	}
	return nil
}

func (s *Store) verifyCommittedSuspensionGC(op ExecutionOperation, _ RepositoryControllerHead) error {
	if op.Suspension == nil {
		return fmt.Errorf("suspension GC authority is inconsistent")
	}
	_, exists, err := readExecutionRef(s.identity.PrimaryRoot, suspensionRef(op.Suspension.SnapshotID))
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("committed suspension GC ref was recreated")
	}
	return nil
}
