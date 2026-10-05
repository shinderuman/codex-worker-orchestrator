package controller

import (
	"fmt"
	"os"
)

func (s *Store) verifyCommittedCleanup(op ExecutionOperation, _ RepositoryControllerHead) error {
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
