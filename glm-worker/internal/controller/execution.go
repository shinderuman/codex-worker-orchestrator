package controller

import (
	"errors"
	"fmt"
)

func (s *Store) AdmitMutationOrFailClosed(task SemanticTaskRef, workspace WorkspaceIdentity, snapshot WorkspaceSnapshot) (Admission, error) {
	admission, err := s.AdmitMutation(task, workspace, snapshot)
	if err == nil {
		return admission, nil
	}
	lock, lockErr := s.acquireMutationLock()
	if lockErr != nil {
		return Admission{}, errors.Join(err, lockErr)
	}
	defer func() { _ = lock.Close() }()
	head, headErr := s.LoadHead()
	if headErr != nil || head.Status != ControllerStatusActive || head.LiveLeaseID == "" {
		return Admission{}, err
	}
	lease, leaseErr := s.loadLease(head.LiveLeaseID)
	if leaseErr != nil || lease.WorkspaceID != workspace.ID {
		return Admission{}, err
	}
	if snapshot.Head == lease.ExpectedBaseOID && snapshot.ID == lease.ExpectedWorkspaceSnapshotID {
		return Admission{}, err
	}
	expected := WorkspaceSnapshot{ID: lease.ExpectedWorkspaceSnapshotID, Head: lease.ExpectedBaseOID}
	if _, failErr := s.failClosedLocked("unattributed mutation changed the live execution workspace", "", workspace, expected, snapshot, nil); failErr != nil {
		return Admission{}, errors.Join(err, fmt.Errorf("repository controller fail-close failed: %w", failErr))
	}
	return Admission{}, fmt.Errorf("%w; repository controller entered fail-closed state", err)
}
