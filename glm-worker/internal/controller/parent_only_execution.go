package controller

import (
	"errors"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type ParentOnlyMaterializeInput struct {
	ExpectedGeneration uint64 `json:"expected_generation"`
}

const (
	ExecutionAuthorityParentOnly = taskcontract.ExecutionAuthorityModeParentOnly
	ExecutionAuthorityGLM        = taskcontract.ExecutionAuthorityModeGLM

	parentOnlyExecutionPurpose = "parent-only-execution"
)

func (s *Store) AdmitParentOnlyExecution(input ParentOnlyMaterializeInput) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	op, head, err := s.planParentOnlyExecution(input)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.prepareExecutionOperation(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func (s *Store) planParentOnlyExecution(input ParentOnlyMaterializeInput) (ExecutionOperation, RepositoryControllerHead, error) {
	head, err := s.LoadHead()
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	if err := validateParentOnlyMaterializeHead(head, input); err != nil {
		return ExecutionOperation{}, head, err
	}
	authority, err := s.parentOnlySuccessorAuthority(head)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	if authority.ExecutionMode != ExecutionAuthorityParentOnly {
		return ExecutionOperation{}, head, fmt.Errorf("committed task %s is not designated for parent-only execution", authority.Task.TaskPath)
	}
	workspace, err := s.parentOnlyWorkspace()
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	snapshot, err := CaptureWorkspaceSnapshot(workspace.Root)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	if snapshot.Head != authority.Snapshot.HeadOID {
		return ExecutionOperation{}, head, fmt.Errorf("primary worktree HEAD does not match the committed parent-only task authority")
	}
	if err := s.rejectBoundTaskRuntime(authority.Task); err != nil {
		return ExecutionOperation{}, head, err
	}
	if err := s.writeProjectSnapshot(authority.Snapshot); err != nil {
		return ExecutionOperation{}, head, err
	}
	attempt, lease, err := s.newParentOnlyExecutionRecords(authority, workspace, snapshot, head.ControllerGeneration+3)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	record, err := executionTransition(head, executionParentOnlyMaterialize)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	record.ProjectSnapshotNew = authority.ProjectSnapshotID
	record.TargetAttemptID = attempt.AttemptID
	record.TargetLeaseID = lease.LeaseID
	record.TargetWorkspaceID = workspace.ID
	record.TargetExecutionTaskRef = attempt.SemanticTaskRef
	record.TargetRootTaskRef = attempt.RootTaskRef
	record.Effects = []EffectExpectation{{Surface: MutationSurfaceSource, Resource: s.identity.PrimaryRoot, ExpectedNew: parentOnlyMaterializationIdentity(workspace, snapshot)}}
	op := ExecutionOperation{Transition: record, Source: Admission{Head: head}, Workspace: &workspace, Attempt: &attempt, Lease: &lease}
	return op, head, nil
}

func (s *Store) newParentOnlyExecutionRecords(authority CommittedTaskAuthority, workspace WorkspaceIdentity, snapshot WorkspaceSnapshot, generation uint64) (AttemptRecord, ExecutionLease, error) {
	attempt, lease, err := newExecutionRecords(authority.Task, authority.Task, workspace, snapshot, generation, parentOnlyExecutionPurpose)
	if err != nil {
		return AttemptRecord{}, ExecutionLease{}, err
	}
	attempt.ExecutionAuthority = ExecutionAuthorityParentOnly
	baseTree, err := gitTrimmed(s.identity.PrimaryRoot, "rev-parse", authority.Snapshot.HeadOID+"^{tree}")
	if err != nil {
		return AttemptRecord{}, ExecutionLease{}, err
	}
	attempt.BaselineTrees.IndexTree = baseTree
	attempt.BaselineTrees.WorktreeTree = baseTree
	if err := s.archiveExecutionBaseline(workspace.Root, &attempt); err != nil {
		return AttemptRecord{}, ExecutionLease{}, err
	}
	return attempt, lease, nil
}

func validateParentOnlyMaterializeHead(head RepositoryControllerHead, input ParentOnlyMaterializeInput) error {
	if head.Status != ControllerStatusActive || head.PendingTransitionID != "" || head.LiveLeaseID != "" || head.LiveAttemptID != "" || head.ExecutionTaskRef != nil || head.AcceptedCandidateRef != nil || head.ActiveEpisodeID != "" {
		return fmt.Errorf("parent-only execution materialization requires a quiescent controller")
	}
	if head.ControllerGeneration != input.ExpectedGeneration {
		return fmt.Errorf("parent-only execution materialization generation is stale")
	}
	if head.ObservedRemoteTip != "" && head.ObservedRemoteTip != head.IntegrationTip {
		return fmt.Errorf("observed external advancement must be adopted before parent-only execution")
	}
	return nil
}

func (s *Store) parentOnlySuccessorAuthority(head RepositoryControllerHead) (CommittedTaskAuthority, error) {
	if IsPristine(head) {
		return ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	}
	if head.UserTerminationRef != nil && head.MetadataLineageRef == nil {
		return s.UserTerminatedSuccessorAuthority(head)
	}
	authority, err := s.retiredProjectAuthority(head)
	if err != nil {
		return CommittedTaskAuthority{}, err
	}
	current, err := ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	if err != nil {
		return CommittedTaskAuthority{}, err
	}
	if current.ProjectSnapshotID != authority.ProjectSnapshotID {
		return CommittedTaskAuthority{}, fmt.Errorf("parent metadata advancement must be adopted before parent-only execution")
	}
	return current, nil
}

func (s *Store) parentOnlyWorkspace() (WorkspaceIdentity, error) {
	workspace, err := ResolveWorkspaceIdentity(s.identity.PrimaryRoot, s.identity)
	if err != nil {
		return WorkspaceIdentity{}, err
	}
	if err := s.validateBootstrapWorkspace(workspace); err != nil {
		return WorkspaceIdentity{}, err
	}
	return workspace, nil
}

func (s *Store) rejectBoundTaskRuntime(task SemanticTaskRef) error {
	runtime := state.AttachStateStore(WorkflowTaskConfig(s.config, s.identity, task))
	binding, err := runtime.LoadControllerRuntimeBinding()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read task runtime binding: %w", err)
	}
	return fmt.Errorf("task runtime is already bound to attempt %s; parent-only execution requires an execution without a Runtime", binding.AttemptID)
}

func parentOnlyMaterializationIdentity(workspace WorkspaceIdentity, snapshot WorkspaceSnapshot) string {
	return digestStrings("parent-only-materialization", workspace.ID, workspace.RepositoryID, workspace.Root, workspace.GitDir, snapshot.Head, snapshot.ID)
}

func (s *Store) validateParentOnlyExecutionOperation(op ExecutionOperation) error {
	if op.Workspace == nil || op.Attempt == nil || op.Lease == nil {
		return fmt.Errorf("parent-only execution operation is incomplete")
	}
	if op.Attempt.ExecutionAuthority != ExecutionAuthorityParentOnly || op.Lease.Purpose != parentOnlyExecutionPurpose {
		return fmt.Errorf("parent-only execution operation authority differs")
	}
	if op.Workspace.Root != s.identity.PrimaryRoot || op.Workspace.RepositoryID != s.identity.LineageID {
		return fmt.Errorf("parent-only execution is outside the primary worktree")
	}
	if !parentOnlyPlanMatchesJournal(op) {
		return fmt.Errorf("parent-only execution plan differs from journal authority")
	}
	bound := WorkspaceSnapshot{ID: op.Lease.ExpectedWorkspaceSnapshotID, Head: op.Lease.ExpectedBaseOID}
	if op.Transition.Effects[0].ExpectedNew != parentOnlyMaterializationIdentity(*op.Workspace, bound) {
		return fmt.Errorf("parent-only execution source binding differs")
	}
	if !parentOnlyAttemptLeaseBindingMatches(op) {
		return fmt.Errorf("parent-only attempt and lease binding differs")
	}
	return nil
}

func parentOnlyPlanMatchesJournal(op ExecutionOperation) bool {
	return op.Transition.TargetAttemptID == op.Attempt.AttemptID && op.Transition.TargetLeaseID == op.Lease.LeaseID && op.Transition.TargetWorkspaceID == op.Workspace.ID && op.Transition.TargetExecutionTaskRef.Equal(op.Attempt.SemanticTaskRef) && op.Transition.TargetRootTaskRef.Equal(op.Attempt.RootTaskRef)
}

func parentOnlyAttemptLeaseBindingMatches(op ExecutionOperation) bool {
	return op.Attempt.ExecutionBaseOID == op.Lease.ExpectedBaseOID && op.Attempt.WorkspaceSnapshotID == op.Lease.ExpectedWorkspaceSnapshotID && op.Lease.AttemptID == op.Attempt.AttemptID && op.Lease.WorkspaceID == op.Workspace.ID && op.Lease.SemanticTaskRef.Equal(op.Attempt.SemanticTaskRef)
}

func (s *Store) applyParentOnlyExecution(op ExecutionOperation) error {
	workspace, err := ResolveWorkspaceIdentity(op.Workspace.Root, s.identity)
	if err != nil {
		return err
	}
	if workspace != *op.Workspace {
		return fmt.Errorf("parent-only execution workspace was replaced")
	}
	snapshot, err := CaptureWorkspaceSnapshot(workspace.Root)
	if err != nil {
		return err
	}
	if snapshot.Head != op.Lease.ExpectedBaseOID || snapshot.ID != op.Lease.ExpectedWorkspaceSnapshotID {
		return fmt.Errorf("parent-only execution source changed after prepare")
	}
	if err := s.writeAttempt(*op.Attempt); err != nil {
		return err
	}
	if err := s.writeLease(*op.Lease); err != nil {
		return err
	}
	actual := map[string]string{op.Transition.Effects[0].Key(): parentOnlyMaterializationIdentity(workspace, snapshot)}
	_, err = s.commitAuthorityTransitionLocked(op.Transition, actual, false, func(next *RepositoryControllerHead) error {
		next.ProjectSnapshotID = op.Transition.ProjectSnapshotNew
		next.IntegrationTip = op.Lease.ExpectedBaseOID
		next.RootTaskRef = &op.Attempt.RootTaskRef
		task := op.Attempt.SemanticTaskRef
		next.ExecutionTaskRef = &task
		next.LiveAttemptID = op.Attempt.AttemptID
		next.LiveLeaseID = op.Lease.LeaseID
		return nil
	})
	return err
}

func (s *Store) verifyCommittedParentOnlyExecution(op ExecutionOperation, head RepositoryControllerHead) error {
	if !parentOnlyHeadBindingsMatch(op, head) {
		return fmt.Errorf("committed parent-only execution bindings differ from operation")
	}
	workspace, err := ResolveWorkspaceIdentity(op.Workspace.Root, s.identity)
	if err != nil {
		return err
	}
	if workspace != *op.Workspace {
		return fmt.Errorf("committed parent-only execution workspace was replaced")
	}
	attempt, err := s.loadAttempt(head.LiveAttemptID)
	if err != nil {
		return err
	}
	if attempt.ExecutionAuthority != ExecutionAuthorityParentOnly {
		return fmt.Errorf("committed parent-only execution attempt lost its execution authority")
	}
	lease, err := s.loadLease(head.LiveLeaseID)
	if err != nil {
		return err
	}
	snapshot, err := CaptureWorkspaceSnapshot(workspace.Root)
	if err != nil {
		return err
	}
	if snapshot.ID != lease.ExpectedWorkspaceSnapshotID || snapshot.Head != lease.ExpectedBaseOID {
		return fmt.Errorf("committed parent-only execution source changed unexpectedly")
	}
	return nil
}

func parentOnlyHeadBindingsMatch(op ExecutionOperation, head RepositoryControllerHead) bool {
	return head.LiveLeaseID == op.Lease.LeaseID && head.LiveAttemptID == op.Attempt.AttemptID && head.ExecutionTaskRef != nil && head.ExecutionTaskRef.Equal(op.Attempt.SemanticTaskRef) && head.RootTaskRef != nil && head.RootTaskRef.Equal(op.Attempt.RootTaskRef) && head.ProjectSnapshotID == op.Transition.ProjectSnapshotNew && head.IntegrationTip == op.Lease.ExpectedBaseOID
}

func (s *Store) parentOnlyLiveExecution(head RepositoryControllerHead) (AttemptRecord, bool, error) {
	if head.LiveAttemptID == "" || head.LiveLeaseID == "" || head.ExecutionTaskRef == nil {
		return AttemptRecord{}, false, nil
	}
	attempt, err := s.loadAttempt(head.LiveAttemptID)
	if err != nil {
		return AttemptRecord{}, false, err
	}
	if attempt.ExecutionAuthority != ExecutionAuthorityParentOnly || !attempt.SemanticTaskRef.Equal(*head.ExecutionTaskRef) {
		return AttemptRecord{}, false, nil
	}
	return attempt, true, nil
}

func (s *Store) LiveParentOnlyExecution() (bool, error) {
	head, err := s.LoadHead()
	if err != nil {
		return false, err
	}
	_, live, err := s.parentOnlyLiveExecution(head)
	return live, err
}
