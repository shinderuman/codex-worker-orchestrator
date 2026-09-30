package controller

import (
	"errors"
	"fmt"
)

func (s *Store) RotateExecution(task SemanticTaskRef, workspace WorkspaceIdentity, snapshot WorkspaceSnapshot, purpose string) (Admission, error) {
	head, err := s.LoadHead()
	if err != nil {
		return Admission{}, err
	}
	if head.Status != ControllerStatusActive || head.PendingTransitionID != "" {
		return Admission{}, fmt.Errorf("repository controller is not available for execution rotation")
	}
	if workspace.Root != s.identity.PrimaryRoot || workspace.RepositoryID != s.identity.LineageID {
		return Admission{}, fmt.Errorf("execution rotation requires the verified primary worktree")
	}
	if purpose == "" {
		return Admission{}, fmt.Errorf("execution rotation purpose is required")
	}
	authority, err := ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	if err != nil {
		return Admission{}, err
	}
	if !authority.Task.Equal(task) {
		return Admission{}, fmt.Errorf("requested execution task does not match committed repository authority")
	}
	if head.LiveAttemptID == "" || head.LiveLeaseID == "" || head.ExecutionTaskRef == nil {
		return Admission{}, fmt.Errorf("execution rotation requires an existing live execution lease")
	}
	current, err := s.AdmitMutation(*head.ExecutionTaskRef, workspace, snapshot)
	if err != nil {
		return Admission{}, err
	}
	return s.rotateAdmittedExecution(current, authority, purpose)
}

func (s *Store) rotateAdmittedExecution(
	current Admission,
	authority CommittedTaskAuthority,
	purpose string,
) (Admission, error) {
	transition, err := s.BeginTransition("execution-rotation:"+purpose, current.Head.ControllerGeneration, nil)
	if err != nil {
		return Admission{}, err
	}
	attempt, lease, err := newExecutionRecords(
		authority.Task,
		authority.Task,
		current.Workspace,
		current.Snapshot,
		transition.TargetGeneration,
		purpose,
	)
	if err != nil {
		return Admission{}, err
	}
	if err := s.writeAttempt(attempt); err != nil {
		return Admission{}, err
	}
	if err := s.writeLease(lease); err != nil {
		return Admission{}, err
	}
	root := authority.Task
	next, err := s.CommitTransition(transition, map[string]string{}, true, func(head *RepositoryControllerHead) error {
		if head.LiveLeaseID != current.Lease.LeaseID || head.LiveAttemptID != current.Attempt.AttemptID {
			return fmt.Errorf("live execution authority changed during rotation")
		}
		head.ProjectSnapshotID = authority.ProjectSnapshotID
		head.RootTaskRef = &root
		head.ExecutionTaskRef = &root
		head.LiveAttemptID = attempt.AttemptID
		head.LiveLeaseID = lease.LeaseID
		head.ActiveEpisodeID = ""
		head.ActiveEpisodeRevision = 0
		return nil
	})
	if err != nil {
		return Admission{}, err
	}
	return Admission{
		Head:      next,
		Attempt:   attempt,
		Lease:     lease,
		Workspace: current.Workspace,
		Snapshot:  current.Snapshot,
	}, nil
}

func (s *Store) AdmitMutationOrFailClosed(task SemanticTaskRef, workspace WorkspaceIdentity, snapshot WorkspaceSnapshot) (Admission, error) {
	admission, err := s.AdmitMutation(task, workspace, snapshot)
	if err == nil {
		return admission, nil
	}
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
	if _, failErr := s.FailClosed("unattributed mutation changed the live execution workspace", "", workspace, expected, snapshot, nil); failErr != nil {
		return Admission{}, errors.Join(err, fmt.Errorf("repository controller fail-close failed: %w", failErr))
	}
	return Admission{}, fmt.Errorf("%w; repository controller entered fail-closed state", err)
}
