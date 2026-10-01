package controller

import (
	"fmt"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (s *Store) bootstrapExecution(task SemanticTaskRef, workspace WorkspaceIdentity, snapshot WorkspaceSnapshot) (Admission, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return Admission{}, err
	}
	defer func() { _ = lock.Close() }()
	head, err := s.LoadHead()
	if err != nil {
		return Admission{}, err
	}
	if head.Status != ControllerStatusActive || head.PendingTransitionID != "" {
		return Admission{}, fmt.Errorf("repository controller is not available for execution bootstrap")
	}
	if head.LiveLeaseID != "" || head.LiveAttemptID != "" {
		authority, err := MutationAuthorityFromHead(head)
		if err != nil {
			return Admission{}, err
		}
		return s.AdmitMutation(authority, workspace, snapshot)
	}
	if err := s.validateBootstrapWorkspace(workspace); err != nil {
		return Admission{}, err
	}
	authority, err := ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	if err != nil {
		return Admission{}, err
	}
	if !authority.Task.Equal(task) {
		return Admission{}, fmt.Errorf("requested bootstrap task does not match committed repository authority")
	}
	if snapshot.Head != authority.Snapshot.HeadOID {
		return Admission{}, fmt.Errorf("bootstrap workspace HEAD does not match committed project snapshot")
	}
	return s.bootstrapFreshExecution(head, authority, workspace, snapshot)
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
	authority CommittedTaskAuthority,
	workspace WorkspaceIdentity,
	snapshot WorkspaceSnapshot,
) (Admission, error) {
	if err := s.writeProjectSnapshot(authority.Snapshot); err != nil {
		return Admission{}, err
	}
	nextGeneration := head.ControllerGeneration + 1
	attempt, lease, err := newExecutionRecords(
		authority.Task,
		authority.Task,
		workspace,
		snapshot,
		nextGeneration,
		"root-execution",
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
	next := head
	next.ControllerGeneration = nextGeneration
	next.ProjectSnapshotID = authority.ProjectSnapshotID
	next.RootTaskRef = &root
	next.ExecutionTaskRef = &root
	next.LiveAttemptID = attempt.AttemptID
	next.LiveLeaseID = lease.LeaseID
	if err := s.writeHeadCAS(head.ControllerGeneration, next); err != nil {
		return Admission{}, err
	}
	return Admission{Head: next, Attempt: attempt, Lease: lease, Workspace: workspace, Snapshot: snapshot}, nil
}

func (s *Store) admitMutation(authority MutationAuthority, workspace WorkspaceIdentity, snapshot WorkspaceSnapshot) (Admission, error) {
	head, err := s.loadAdmissibleHead(authority.SemanticTaskRef)
	if err != nil {
		return Admission{}, err
	}
	if err := validateMutationAuthorityClaim(head, authority); err != nil {
		return Admission{}, err
	}
	attempt, lease, err := s.loadLiveAttemptAndLease(head)
	if err != nil {
		return Admission{}, err
	}
	if err := validateLiveAuthority(head, authority.SemanticTaskRef, workspace, snapshot, attempt, lease); err != nil {
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
	if head.LiveAttemptID == "" || head.LiveLeaseID == "" || head.ExecutionTaskRef == nil || head.RootTaskRef == nil {
		return RepositoryControllerHead{}, fmt.Errorf("repository controller has no complete live execution authority")
	}
	if !head.ExecutionTaskRef.Equal(task) {
		return RepositoryControllerHead{}, fmt.Errorf("semantic execution task does not match repository controller authority")
	}
	if head.ProjectSnapshotID == "" {
		return RepositoryControllerHead{}, fmt.Errorf("repository controller has no project snapshot authority")
	}
	if _, err := s.LoadProjectSnapshot(head.ProjectSnapshotID); err != nil {
		return RepositoryControllerHead{}, err
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
	if err := validateLiveRecordIdentity(head, task, attempt, lease); err != nil {
		return err
	}
	return validateLiveLeaseBinding(head, workspace, snapshot, lease)
}

func validateLiveRecordIdentity(
	head RepositoryControllerHead,
	task SemanticTaskRef,
	attempt AttemptRecord,
	lease ExecutionLease,
) error {
	if attempt.AttemptState != AttemptStateLive || attempt.AttemptID != lease.AttemptID || attempt.AttemptID != head.LiveAttemptID {
		return fmt.Errorf("live attempt/lease identity is inconsistent")
	}
	if !attempt.SemanticTaskRef.Equal(task) || !lease.SemanticTaskRef.Equal(task) {
		return fmt.Errorf("live attempt/lease semantic task is stale")
	}
	if head.RootTaskRef == nil || !attempt.RootTaskRef.Equal(*head.RootTaskRef) {
		return fmt.Errorf("live attempt root task does not match repository controller authority")
	}
	return nil
}

func validateLiveLeaseBinding(
	head RepositoryControllerHead,
	workspace WorkspaceIdentity,
	snapshot WorkspaceSnapshot,
	lease ExecutionLease,
) error {
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

func newExecutionRecords(
	task SemanticTaskRef,
	root SemanticTaskRef,
	workspace WorkspaceIdentity,
	snapshot WorkspaceSnapshot,
	generation uint64,
	purpose string,
) (AttemptRecord, ExecutionLease, error) {
	attemptID, err := state.NewUUID()
	if err != nil {
		return AttemptRecord{}, ExecutionLease{}, err
	}
	leaseID, err := state.NewUUID()
	if err != nil {
		return AttemptRecord{}, ExecutionLease{}, err
	}
	now := time.Now().UTC()
	attempt := AttemptRecord{
		SchemaVersion:             controllerSchemaVersion,
		AttemptID:                 attemptID,
		SemanticTaskRef:           task,
		RootTaskRef:               root,
		ExecutionBaseOID:          snapshot.Head,
		BaselineSnapshotID:        snapshot.ID,
		WorkspaceSnapshotID:       snapshot.ID,
		StartControllerGeneration: generation,
		AttemptState:              AttemptStateLive,
		CreatedAt:                 now,
	}
	lease := ExecutionLease{
		SchemaVersion:               controllerSchemaVersion,
		LeaseID:                     leaseID,
		AttemptID:                   attemptID,
		SemanticTaskRef:             task,
		Purpose:                     purpose,
		ControllerGeneration:        generation,
		WorkspaceID:                 workspace.ID,
		ExpectedBaseOID:             snapshot.Head,
		ExpectedWorkspaceSnapshotID: snapshot.ID,
		CreatedAt:                   now,
	}
	return attempt, lease, nil
}
