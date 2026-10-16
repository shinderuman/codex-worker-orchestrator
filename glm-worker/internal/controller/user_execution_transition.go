package controller

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type UserExecutionTransitionInput struct {
	ExpectedGeneration uint64          `json:"expected_generation"`
	SourceProjectID    string          `json:"source_project_id"`
	SourceTask         SemanticTaskRef `json:"source_task"`
	SourceAttemptID    string          `json:"source_attempt_id"`
	SourceLeaseID      string          `json:"source_lease_id"`
	SuccessorTask      SemanticTaskRef `json:"successor_task"`
	SuccessorProjectID string          `json:"successor_project_id"`
	Reason             string          `json:"reason"`
}

type UserExecutionTermination struct {
	Attempt     AttemptRecord       `json:"attempt"`
	Lease       ExecutionLease      `json:"lease"`
	Workspace   WorkspaceIdentity   `json:"workspace"`
	Snapshot    WorkspaceSnapshot   `json:"snapshot"`
	Trees       ExecutionTrees      `json:"trees"`
	Archive     EvidenceObjectRef   `json:"archive"`
	RuntimeRefs []EvidenceObjectRef `json:"runtime_refs"`
}

const userExecutionTransition = "USER_TERMINATE_EXECUTION_AND_SELECT_SUCCESSOR"

func userTerminationKind(record UserTaskTermination) string {
	if record.Execution != nil {
		return userExecutionTransition
	}
	return userTaskTransition
}

func (s *Store) PreviewExecutionTermination() (UserExecutionTransitionInput, error) {
	head, err := s.LoadHead()
	if err != nil {
		return UserExecutionTransitionInput{}, err
	}
	if err := s.validateExecutionTerminationHead(head); err != nil {
		return UserExecutionTransitionInput{}, err
	}
	authority, err := ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	if err != nil {
		return UserExecutionTransitionInput{}, err
	}
	input := UserExecutionTransitionInput{ExpectedGeneration: head.ControllerGeneration, SourceProjectID: head.ProjectSnapshotID, SourceTask: *head.ExecutionTaskRef, SourceAttemptID: head.LiveAttemptID, SourceLeaseID: head.LiveLeaseID, SuccessorTask: authority.Task, SuccessorProjectID: authority.ProjectSnapshotID}
	source, err := s.executionTerminationSource(head, input)
	if err != nil {
		return input, err
	}
	record := UserTaskTermination{SourceHead: head, SuccessorProject: authority.Snapshot, SuccessorTask: authority.Task, Execution: &UserExecutionTermination{Attempt: source.Attempt, Lease: source.Lease, Workspace: source.Workspace}}
	if err := s.verifyTerminatedTaskDisposition(record); err != nil {
		return input, err
	}
	return input, s.planUserExecutionRuntimeTermination(&record)
}

func (s *Store) validateExecutionTerminationHead(head RepositoryControllerHead) error {
	if head.Status != ControllerStatusActive || head.RepositoryIdentity != s.identity.LineageID || head.PendingTransitionID != "" || head.ActiveEpisodeID != "" || head.RootTaskRef == nil || head.ExecutionTaskRef == nil || !head.RootTaskRef.Equal(*head.ExecutionTaskRef) || head.LiveAttemptID == "" || head.LiveLeaseID == "" || head.AcceptedCandidateRef != nil {
		return fmt.Errorf("execution termination requires idle unaccepted root authority")
	}
	return nil
}

func (s *Store) executionTerminationSource(head RepositoryControllerHead, input UserExecutionTransitionInput) (Admission, error) {
	if err := s.validateExecutionTerminationHead(head); err != nil {
		return Admission{}, err
	}
	if head.ControllerGeneration != input.ExpectedGeneration || head.ProjectSnapshotID != input.SourceProjectID || !head.ExecutionTaskRef.Equal(input.SourceTask) || head.LiveAttemptID != input.SourceAttemptID || head.LiveLeaseID != input.SourceLeaseID {
		return Admission{}, fmt.Errorf("execution termination source authority differs")
	}
	attempt, lease, err := s.loadLiveAttemptAndLease(head)
	if err != nil {
		return Admission{}, err
	}
	if err := validateLiveRecordIdentity(head, input.SourceTask, attempt, lease); err != nil {
		return Admission{}, err
	}
	if lease.ControllerGeneration != head.ControllerGeneration || lease.InFlightCallID != "" {
		return Admission{}, fmt.Errorf("execution termination lease is stale or in flight")
	}
	workspace, err := s.executionTerminationWorkspace(lease)
	if err != nil {
		return Admission{}, err
	}
	snapshot, err := CaptureWorkspaceSnapshot(workspace.Root)
	return Admission{Head: head, Attempt: attempt, Lease: lease, Workspace: workspace, Snapshot: snapshot}, err
}

func (s *Store) executionTerminationWorkspace(lease ExecutionLease) (WorkspaceIdentity, error) {
	root, err := s.executionLaneRoot()
	if err != nil {
		return WorkspaceIdentity{}, err
	}
	workspace, err := ResolveWorkspaceIdentity(root, s.identity)
	if err != nil {
		return WorkspaceIdentity{}, err
	}
	if workspace.ID != lease.WorkspaceID {
		return WorkspaceIdentity{}, fmt.Errorf("execution termination workspace differs from lease")
	}
	if err := s.validateExecutionLaneLocation(workspace); err != nil {
		return WorkspaceIdentity{}, err
	}
	return workspace, nil
}

func (s *Store) TerminateExecutionAndSelectSuccessor(input UserExecutionTransitionInput) (ExecutionOperationResult, error) {
	if strings.TrimSpace(input.Reason) == "" {
		return ExecutionOperationResult{}, fmt.Errorf("execution termination requires user reason")
	}
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	head, err := s.LoadHead()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	source, err := s.executionTerminationSource(head, input)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	op, err := s.planUserExecutionTransition(source, input)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.prepareExecutionOperation(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func (s *Store) planUserExecutionTransition(source Admission, input UserExecutionTransitionInput) (ExecutionOperation, error) {
	authority, err := ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	if err != nil {
		return ExecutionOperation{}, err
	}
	if authority.ProjectSnapshotID != input.SuccessorProjectID || !authority.Task.Equal(input.SuccessorTask) {
		return ExecutionOperation{}, fmt.Errorf("execution termination successor authority differs")
	}
	transition, err := executionTransition(source.Head, userExecutionTransition)
	if err != nil {
		return ExecutionOperation{}, err
	}
	execution, err := s.captureUserExecutionTermination(source, transition)
	if err != nil {
		return ExecutionOperation{}, err
	}
	archive, _, err := s.CaptureGitObjectArchive(s.identity.PrimaryRoot, "user-termination:"+transition.TransitionID, []string{authority.Snapshot.HeadOID})
	if err != nil {
		return ExecutionOperation{}, err
	}
	record := UserTaskTermination{SchemaVersion: controllerSchemaVersion, RepositoryIdentity: s.identity.LineageID, TransitionID: transition.TransitionID, SourceHead: source.Head, SuccessorProject: authority.Snapshot, SuccessorTask: authority.Task, Reason: input.Reason, GitArchive: archive, Execution: &execution}
	if err := s.planUserRuntimeTermination(&record); err != nil {
		return ExecutionOperation{}, err
	}
	if err := s.validateUserTaskTermination(record); err != nil {
		return ExecutionOperation{}, err
	}
	if err := s.verifyUserTaskTransitionPrimary(record); err != nil {
		return ExecutionOperation{}, err
	}
	ref, err := s.putEvidenceJSON("user-task-termination", transition.TransitionID, record)
	if err != nil {
		return ExecutionOperation{}, err
	}
	transition.ProjectSnapshotNew = authority.ProjectSnapshotID
	transition.SourceRootTaskRef = source.Attempt.RootTaskRef
	transition.SourceExecutionTaskRef = source.Attempt.SemanticTaskRef
	transition.SourceAttemptID = source.Attempt.AttemptID
	transition.SourceLeaseID = source.Lease.LeaseID
	transition.TargetRootTaskRef = authority.Task
	transition.Effects = []EffectExpectation{{Surface: MutationSurfaceState, Resource: "user-task-termination", ExpectedNew: ref.Digest}}
	return ExecutionOperation{Transition: transition, UserTermination: &UserTaskTransitionOperation{Record: record, RecordRef: ref}}, nil
}

func (s *Store) captureUserExecutionTermination(source Admission, transition TransitionRecord) (UserExecutionTermination, error) {
	trees, err := captureCleanupTrees(source.Workspace.Root)
	if err != nil {
		return UserExecutionTermination{}, err
	}
	archive, _, err := s.CaptureGitObjectArchive(source.Workspace.Root, "execution-termination:"+transition.TransitionID, []string{source.Snapshot.Head, trees.IndexTree, trees.WorktreeTree})
	if err != nil {
		return UserExecutionTermination{}, err
	}
	runtime, taskID, err := s.boundAttemptRuntime(source)
	if err != nil || runtime == nil {
		return UserExecutionTermination{}, fmt.Errorf("execution termination requires bound runtime: %w", err)
	}
	retained := AttemptSeal{AttemptID: source.Attempt.AttemptID, StartedAt: source.Attempt.CreatedAt, SealedAt: time.Now().UTC()}
	collector := attemptRuntimeEvidence{store: s, runtime: runtime, workspace: source.Workspace.Root, seal: &retained, taskID: taskID}
	if err := collector.captureStateFiles(); err != nil {
		return UserExecutionTermination{}, err
	}
	return UserExecutionTermination{Attempt: source.Attempt, Lease: source.Lease, Workspace: source.Workspace, Snapshot: source.Snapshot, Trees: trees, Archive: archive, RuntimeRefs: retained.EvidenceRefs}, nil
}

func (s *Store) verifyTerminatedTaskDisposition(record UserTaskTermination) error {
	if record.Execution == nil {
		return s.verifyTerminatedTaskAbsent(record)
	}
	path := record.SourceHead.ExecutionTaskRef.TaskPath
	if record.SuccessorTask.TaskPath == path || len(record.SuccessorProject.Next) == 0 || record.SuccessorProject.Next[0] != path {
		return fmt.Errorf("terminated execution Task must remain first NEXT")
	}
	if _, ok := findTaskRef(record.SuccessorProject.Tasks, path); !ok {
		return fmt.Errorf("terminated execution requirements are missing")
	}
	return nil
}

func executionTerminationCleanup(record UserTaskTermination) ExecutionOperation {
	execution := record.Execution
	return ExecutionOperation{Workspace: &execution.Workspace, CleanupSnapshot: &execution.Snapshot, CleanupTrees: &execution.Trees, CleanupArchive: &execution.Archive}
}

func (s *Store) verifyUserExecutionTermination(record UserTaskTermination) error {
	e := record.Execution
	if e == nil || record.CleanupTransitionID != "" || record.RuntimeTermination == nil {
		return fmt.Errorf("execution termination retention authority is incomplete")
	}
	expected := state.ControllerTaskTermination{Version: 1, TransitionID: record.TransitionID, SourceGeneration: record.SourceHead.ControllerGeneration, TaskPath: e.Attempt.SemanticTaskRef.TaskPath, TaskContractDigest: e.Attempt.SemanticTaskRef.ContractDigest, AttemptID: e.Attempt.AttemptID, PrimaryRoot: s.identity.PrimaryRoot, PreviousRepoRoot: e.Workspace.Root, PreviousStatus: state.TaskStatusAwaitingParentCompletion, Reason: record.Reason}
	if !reflect.DeepEqual(*record.RuntimeTermination, expected) {
		return fmt.Errorf("execution termination runtime record differs")
	}
	if err := s.verifyUserExecutionLease(record); err != nil {
		return err
	}
	if err := s.verifyCleanupArchive(executionTerminationCleanup(record)); err != nil {
		return err
	}
	if len(e.RuntimeRefs) == 0 {
		return fmt.Errorf("execution termination runtime retention is missing")
	}
	for _, ref := range e.RuntimeRefs {
		if _, err := s.LoadEvidenceObject(ref); err != nil {
			return err
		}
	}
	return s.verifyUserTaskEvidence(record.SourceHead)
}

func (s *Store) verifyUserExecutionLease(record UserTaskTermination) error {
	e := record.Execution
	if err := s.validateExecutionTerminationHead(record.SourceHead); err != nil {
		return err
	}
	if err := validateLiveRecordIdentity(record.SourceHead, *record.SourceHead.ExecutionTaskRef, e.Attempt, e.Lease); err != nil {
		return err
	}
	if e.Lease.InFlightCallID != "" || e.Lease.ControllerGeneration != record.SourceHead.ControllerGeneration || e.Workspace.ID != e.Lease.WorkspaceID || e.Workspace.RepositoryID != s.identity.LineageID {
		return fmt.Errorf("execution termination retained lease differs")
	}
	return s.validateExecutionLaneLocation(e.Workspace)
}

func (s *Store) planUserExecutionRuntimeTermination(record *UserTaskTermination) error {
	e := record.Execution
	cfg := WorkflowTaskConfig(s.config, s.identity, e.Attempt.SemanticTaskRef)
	runtime := state.AttachStateStore(cfg)
	binding, err := runtime.LoadControllerRuntimeBinding()
	if err != nil {
		return err
	}
	if binding.AttemptID != e.Attempt.AttemptID || binding.TaskPath != e.Attempt.SemanticTaskRef.TaskPath || binding.TaskContractDigest != e.Attempt.SemanticTaskRef.ContractDigest || runtime.TaskStatus() != state.TaskStatusAwaitingParentCompletion || runtime.ReadOr("repo-root", "") != e.Workspace.Root {
		return fmt.Errorf("execution termination runtime authority differs")
	}
	record.RuntimeTermination = &state.ControllerTaskTermination{Version: 1, TransitionID: record.TransitionID, SourceGeneration: record.SourceHead.ControllerGeneration, TaskPath: binding.TaskPath, TaskContractDigest: binding.TaskContractDigest, AttemptID: binding.AttemptID, Reason: record.Reason, PreviousRepoRoot: e.Workspace.Root, PrimaryRoot: s.identity.PrimaryRoot, PreviousStatus: runtime.TaskStatus()}
	return nil
}

func (s *Store) applyUserExecutionTermination(record UserTaskTermination) error {
	if record.Execution == nil {
		return nil
	}
	current, err := s.loadAttempt(record.Execution.Attempt.AttemptID)
	if err != nil {
		return err
	}
	expected := record.Execution.Attempt
	if current.AttemptState == AttemptStateUserTerminated {
		expected.AttemptState = AttemptStateUserTerminated
	}
	lease, err := s.loadLease(record.Execution.Lease.LeaseID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, expected) || !reflect.DeepEqual(lease, record.Execution.Lease) {
		return fmt.Errorf("execution termination source records changed")
	}
	if err := s.removeExecutionCleanupWorkspace(executionTerminationCleanup(record)); err != nil {
		return err
	}
	current.AttemptState = AttemptStateUserTerminated
	return s.writeAttempt(current)
}

func (s *Store) verifyCommittedUserExecutionTransition(op ExecutionOperation, head RepositoryControllerHead) error {
	if err := s.verifyCommittedUserTaskTransition(op, head); err != nil {
		return err
	}
	record := op.UserTermination.Record
	if record.Execution == nil {
		return fmt.Errorf("execution termination record missing")
	}
	attempt, err := s.loadAttempt(record.Execution.Attempt.AttemptID)
	if err != nil {
		return err
	}
	expected := record.Execution.Attempt
	expected.AttemptState = AttemptStateUserTerminated
	if !reflect.DeepEqual(attempt, expected) {
		return fmt.Errorf("terminated execution remains resumable")
	}
	return verifyExecutionLaneAbsent(record.Execution.Workspace)
}
