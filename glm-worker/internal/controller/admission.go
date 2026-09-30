package controller

import (
	"fmt"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (s *Store) bootstrapExecution(task SemanticTaskRef, workspace WorkspaceIdentity, snapshot WorkspaceSnapshot) (Admission, error) {
	head, err := s.LoadHead()
	if err != nil {
		return Admission{}, err
	}
	if head.Status != ControllerStatusActive || head.PendingTransitionID != "" {
		return Admission{}, fmt.Errorf("repository controller is not available for execution bootstrap")
	}
	if head.LiveLeaseID != "" || head.LiveAttemptID != "" {
		return s.AdmitMutation(task, workspace, snapshot)
	}
	if err := s.validateBootstrapWorkspace(workspace); err != nil {
		return Admission{}, err
	}
	return s.bootstrapFreshExecution(head, task, workspace, snapshot)
}

func (s *Store) validateBootstrapWorkspace(workspace WorkspaceIdentity) error {
	if workspace.Root != s.identity.PrimaryRoot {
		return fmt.Errorf("only the verified primary worktree may bootstrap repository mutation authority")
	}
	if workspace.RepositoryID != s.identity.LineageID {
		return fmt.Errorf("workspace repository identity does not match controller")
	}
	return nil
}

func (s *Store) bootstrapFreshExecution(
	head RepositoryControllerHead,
	task SemanticTaskRef,
	workspace WorkspaceIdentity,
	snapshot WorkspaceSnapshot,
) (Admission, error) {
	attemptID, err := state.NewUUID()
	if err != nil {
		return Admission{}, err
	}
	leaseID, err := state.NewUUID()
	if err != nil {
		return Admission{}, err
	}
	nextGeneration := head.ControllerGeneration + 1
	root := task
	attempt := AttemptRecord{
		SchemaVersion:             controllerSchemaVersion,
		AttemptID:                 attemptID,
		SemanticTaskRef:           task,
		RootTaskRef:               root,
		ExecutionBaseOID:          snapshot.Head,
		BaselineSnapshotID:        snapshot.ID,
		WorkspaceSnapshotID:       snapshot.ID,
		StartControllerGeneration: nextGeneration,
		AttemptState:              AttemptStateLive,
		CreatedAt:                 time.Now().UTC(),
	}
	lease := ExecutionLease{
		SchemaVersion:               controllerSchemaVersion,
		LeaseID:                     leaseID,
		AttemptID:                   attemptID,
		SemanticTaskRef:             task,
		Purpose:                     "root-execution",
		ControllerGeneration:        nextGeneration,
		WorkspaceID:                 workspace.ID,
		ExpectedBaseOID:             snapshot.Head,
		ExpectedWorkspaceSnapshotID: snapshot.ID,
		CreatedAt:                   time.Now().UTC(),
	}
	if err := s.writeAttempt(attempt); err != nil {
		return Admission{}, err
	}
	if err := s.writeLease(lease); err != nil {
		return Admission{}, err
	}
	next := head
	next.ControllerGeneration = nextGeneration
	next.RootTaskRef = &root
	next.ExecutionTaskRef = &task
	next.LiveAttemptID = attemptID
	next.LiveLeaseID = leaseID
	if err := s.writeHeadCAS(head.ControllerGeneration, next); err != nil {
		return Admission{}, err
	}
	return Admission{Head: next, Attempt: attempt, Lease: lease, Workspace: workspace, Snapshot: snapshot}, nil
}

func (s *Store) admitMutation(task SemanticTaskRef, workspace WorkspaceIdentity, snapshot WorkspaceSnapshot) (Admission, error) {
	head, err := s.loadAdmissibleHead(task)
	if err != nil {
		return Admission{}, err
	}
	attempt, lease, err := s.loadLiveAttemptAndLease(head)
	if err != nil {
		return Admission{}, err
	}
	if err := validateLiveAuthority(head, task, workspace, snapshot, attempt, lease); err != nil {
		return Admission{}, err
	}
	return Admission{Head: head, Attempt: attempt, Lease: lease, Workspace: workspace, Snapshot: snapshot}, nil
}

func (s *Store) loadAdmissibleHead(task SemanticTaskRef) (RepositoryControllerHead, error) {
	head, err := s.LoadHead()
	if err != nil {
		return RepositoryControllerHead{}, err
	}
	if head.Status != ControllerStatusActive {
		return RepositoryControllerHead{}, fmt.Errorf("repository controller is fail-closed")
	}
	if head.PendingTransitionID != "" {
		return RepositoryControllerHead{}, fmt.Errorf("repository controller has pending transition %s", head.PendingTransitionID)
	}
	if head.LiveAttemptID == "" || head.LiveLeaseID == "" || head.ExecutionTaskRef == nil {
		return RepositoryControllerHead{}, fmt.Errorf("repository controller has no live execution lease")
	}
	if !head.ExecutionTaskRef.Equal(task) {
		return RepositoryControllerHead{}, fmt.Errorf("semantic execution task does not match repository controller authority")
	}
	return head, nil
}

func (s *Store) loadLiveAttemptAndLease(head RepositoryControllerHead) (AttemptRecord, ExecutionLease, error) {
	attempt, err := s.loadAttempt(head.LiveAttemptID)
	if err != nil {
		return AttemptRecord{}, ExecutionLease{}, err
	}
	lease, err := s.loadLease(head.LiveLeaseID)
	if err != nil {
		return AttemptRecord{}, ExecutionLease{}, err
	}
	return attempt, lease, nil
}

func validateLiveAuthority(
	head RepositoryControllerHead,
	task SemanticTaskRef,
	workspace WorkspaceIdentity,
	snapshot WorkspaceSnapshot,
	attempt AttemptRecord,
	lease ExecutionLease,
) error {
	if attempt.AttemptState != AttemptStateLive || attempt.AttemptID != lease.AttemptID || attempt.AttemptID != head.LiveAttemptID {
		return fmt.Errorf("live attempt/lease identity is inconsistent")
	}
	if !attempt.SemanticTaskRef.Equal(task) || !lease.SemanticTaskRef.Equal(task) {
		return fmt.Errorf("live attempt/lease semantic task is stale")
	}
	if lease.ControllerGeneration != head.ControllerGeneration {
		return fmt.Errorf("execution lease generation is stale: lease=%d controller=%d", lease.ControllerGeneration, head.ControllerGeneration)
	}
	if workspace.RepositoryID != head.RepositoryIdentity || workspace.ID != lease.WorkspaceID {
		return fmt.Errorf("execution workspace does not match live lease")
	}
	if snapshot.Head != lease.ExpectedBaseOID || snapshot.ID != lease.ExpectedWorkspaceSnapshotID {
		return fmt.Errorf("execution workspace snapshot does not match live lease")
	}
	return nil
}
