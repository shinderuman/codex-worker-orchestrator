package controller

import "fmt"

func (s *Store) FailClosedAdmission(admission Admission, reason string, actual WorkspaceSnapshot) error {
	if reason == "" {
		return fmt.Errorf("controller fail-close reason is required")
	}
	lock, err := s.acquireMutationLock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	current, err := s.LoadHead()
	if err != nil {
		return err
	}
	if current.Status != ControllerStatusActive || current.LiveLeaseID != admission.Lease.LeaseID || current.ControllerGeneration != admission.Head.ControllerGeneration {
		return fmt.Errorf("controller fail-close admission is stale")
	}
	_, err = s.failClosedLocked(reason, current.PendingTransitionID, admission.Workspace, admission.Snapshot, actual, nil)
	return err
}
