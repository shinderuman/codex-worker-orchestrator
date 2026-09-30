package controller

import (
	"fmt"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
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
	return s.mintExecution(head, task, workspace, snapshot, purpose)
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
		return Admission{}, fmt.Errorf("%w; repository controller fail-close failed: %v", err, failErr)
	}
	return Admission{}, fmt.Errorf("%w; repository controller entered fail-closed state", err)
}

func (s *Store) mintExecution(head RepositoryControllerHead, task SemanticTaskRef, workspace WorkspaceIdentity, snapshot WorkspaceSnapshot, purpose string) (Admission, error) {
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
		Purpose:                     purpose,
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
	next.ActiveEpisodeID = ""
	next.ActiveEpisodeRevision = 0
	if err := s.writeHeadCAS(head.ControllerGeneration, next); err != nil {
		return Admission{}, err
	}
	return Admission{Head: next, Attempt: attempt, Lease: lease, Workspace: workspace, Snapshot: snapshot}, nil
}
