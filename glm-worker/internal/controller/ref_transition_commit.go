package controller

import "fmt"

func (s *Store) commitRefTransitionLocked(
	record TransitionRecord,
	admission Admission,
	observation refTransitionObservation,
	after WorkspaceSnapshot,
	command string,
) (Admission, error) {
	targetLease, err := s.loadLease(record.TargetLeaseID)
	if err != nil {
		return Admission{}, err
	}
	if err := validateRefTransitionTargetLease(record, after, targetLease); err != nil {
		return Admission{}, err
	}
	if err := s.markTransitionApplied(record, observation.values); err != nil {
		return Admission{}, err
	}
	if _, err := s.commitAuthorityTransitionLocked(record, observation.values, false, func(next *RepositoryControllerHead) error {
		next.LiveLeaseID = record.TargetLeaseID
		return nil
	}); err != nil {
		return Admission{}, err
	}
	finalHead, err := s.finalizeAuthorityTransitionLocked(record)
	if err != nil {
		return Admission{}, err
	}
	if err := s.writeTransitionMutationProvenance(record, record.TargetLeaseID, command, "success", admission.Snapshot, after); err != nil {
		return Admission{}, err
	}
	return Admission{
		Head:      finalHead,
		Attempt:   admission.Attempt,
		Lease:     targetLease,
		Workspace: admission.Workspace,
		Snapshot:  after,
	}, observation.applyErr
}

func validateRefTransitionTargetLease(record TransitionRecord, after WorkspaceSnapshot, lease ExecutionLease) error {
	if lease.AttemptID != record.SourceAttemptID || lease.WorkspaceID != record.TargetWorkspaceID {
		return fmt.Errorf("ref transition target lease does not match journal authority")
	}
	if lease.ControllerGeneration != record.TargetGeneration || lease.ExpectedWorkspaceSnapshotID != after.ID {
		return fmt.Errorf("ref transition target lease does not match journal authority")
	}
	return nil
}
