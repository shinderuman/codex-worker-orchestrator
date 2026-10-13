package controller

import (
	"errors"
	"fmt"
)

func (s *Store) AdmitMutationOrFailClosed(authority MutationAuthority, workspace WorkspaceIdentity, snapshot WorkspaceSnapshot) (Admission, error) {
	admission, err := s.AdmitMutation(authority, workspace, snapshot)
	if err == nil {
		return admission, nil
	}
	lock, lockErr := s.acquireMutationLock()
	if lockErr != nil {
		return Admission{}, errors.Join(err, lockErr)
	}
	defer func() { _ = lock.Close() }()
	settled, admitAgain, settleErr := s.settleQualityMutationLocked(authority, workspace, snapshot)
	if settleErr == nil && settled {
		return admitAgain, nil
	}
	return s.failClosedForUnattributedMutation(authority, workspace, snapshot, err, settleErr)
}

func (s *Store) failClosedForUnattributedMutation(authority MutationAuthority, workspace WorkspaceIdentity, snapshot WorkspaceSnapshot, err, settleErr error) (Admission, error) {
	head, headErr := s.LoadHead()
	if headErr != nil || head.Status != ControllerStatusActive || head.LiveLeaseID == "" {
		return Admission{}, err
	}
	if claimErr := validateMutationAuthorityClaim(head, authority); claimErr != nil {
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
	if settleErr != nil {
		return Admission{}, fmt.Errorf("%w; staged quality mutation settlement was rejected: %w; repository controller entered fail-closed state", err, settleErr)
	}
	return Admission{}, fmt.Errorf("%w; repository controller entered fail-closed state", err)
}
