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
	authority, episode, err := s.parentOnlyMaterializationAuthority(head)
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
	op := ExecutionOperation{Transition: record, Source: Admission{Head: head}, Episode: episode, Workspace: &workspace, Attempt: &attempt, Lease: &lease}
	if episode != nil {
		if err := planRootResumeClosure(&op, head, authority.Task); err != nil {
			return ExecutionOperation{}, head, err
		}
		if op.Episode.State != EpisodeStateClosed {
			return ExecutionOperation{}, head, fmt.Errorf("parent-only Root episode was not closed")
		}
	}
	return op, head, nil
}

func (s *Store) newParentOnlyExecutionRecords(authority TaskAuthority, workspace WorkspaceIdentity, snapshot WorkspaceSnapshot, generation uint64) (AttemptRecord, ExecutionLease, error) {
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
	if head.Status != ControllerStatusActive || head.PendingTransitionID != "" || head.LiveLeaseID != "" || head.LiveAttemptID != "" || head.ExecutionTaskRef != nil || head.AcceptedCandidateRef != nil {
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

func (s *Store) parentOnlyMaterializationAuthority(head RepositoryControllerHead) (TaskAuthority, *BlockerEpisodeRevision, error) {
	if head.ActiveEpisodeID == "" {
		authority, err := s.parentOnlySuccessorAuthority(head)
		return authority, nil, err
	}
	if head.RootTaskRef == nil || head.ActiveEpisodeRevision == 0 {
		return TaskAuthority{}, nil, fmt.Errorf("parent-only Root resume requires an exact episode and Root authority")
	}
	episode, err := s.LoadEpisodeRevision(head.ActiveEpisodeID, head.ActiveEpisodeRevision)
	if err != nil {
		return TaskAuthority{}, nil, err
	}
	if episode.State != EpisodeStateResumingRoot || episode.ProjectSnapshotID != head.ProjectSnapshotID ||
		!episode.RootTaskRef.Equal(*head.RootTaskRef) || !taskPathSatisfied(episode.SatisfiedTaskRefs, episode.ScopeRootTaskRef.TaskPath) {
		return TaskAuthority{}, nil, fmt.Errorf("parent-only Root resume episode is stale or not satisfied")
	}
	schedule, err := s.scheduleEpisodeAgainstProject(episode)
	if err != nil {
		return TaskAuthority{}, nil, err
	}
	if schedule.Intent != FindingIntentResumeRootTask {
		return TaskAuthority{}, nil, fmt.Errorf("parent-only Root resume is not scheduled")
	}
	authority, err := ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	if err != nil {
		return TaskAuthority{}, nil, err
	}
	if authority.ProjectSnapshotID != head.ProjectSnapshotID || authority.Snapshot.HeadOID != head.IntegrationTip || !authority.Task.Equal(*head.RootTaskRef) {
		return TaskAuthority{}, nil, fmt.Errorf("parent-only Root resume source authority is stale")
	}
	return authority, &episode, nil
}

func (s *Store) parentOnlySuccessorAuthority(head RepositoryControllerHead) (TaskAuthority, error) {
	if IsPristine(head) {
		return ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	}
	if head.UserTerminationRef != nil && head.MetadataLineageRef == nil {
		return s.UserTerminatedSuccessorAuthority(head)
	}
	authority, err := s.retiredProjectAuthority(head)
	if err != nil {
		return TaskAuthority{}, err
	}
	current, err := ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	if err != nil {
		return TaskAuthority{}, err
	}
	if current.ProjectSnapshotID != authority.ProjectSnapshotID {
		return TaskAuthority{}, fmt.Errorf("parent metadata advancement must be adopted before parent-only execution")
	}
	return current, nil
}

// parentOnlyCurrentTaskContract accepts either the original committed Task
// or the exact same-path amendment already admitted by Controller source sync.
func (s *Store) parentOnlyCurrentTaskContract(head RepositoryControllerHead, attempt AttemptRecord, committed TaskAuthority) error {
	if head.LiveAttemptID != attempt.AttemptID || head.ExecutionTaskRef == nil ||
		!head.ExecutionTaskRef.Equal(attempt.SemanticTaskRef) || head.RootTaskRef == nil ||
		!head.RootTaskRef.Equal(attempt.RootTaskRef) || committed.Task.TaskPath != attempt.SemanticTaskRef.TaskPath ||
		committed.Snapshot.HeadOID != attempt.ExecutionBaseOID || head.IntegrationTip != committed.Snapshot.HeadOID {
		return fmt.Errorf("parent-only Task/attempt authority is stale")
	}
	if committed.Task.Equal(attempt.SemanticTaskRef) {
		return nil
	}
	if head.ProjectSnapshotID == committed.ProjectSnapshotID {
		return fmt.Errorf("committed task contract changed during parent-only execution")
	}
	project, err := s.LoadProjectSnapshot(head.ProjectSnapshotID)
	if err != nil {
		return err
	}
	revised, err := ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, project.HeadOID)
	if err != nil {
		return err
	}
	if revised.ProjectSnapshotID != head.ProjectSnapshotID || revised.ExecutionMode != ExecutionAuthorityParentOnly ||
		!revised.Task.Equal(attempt.SemanticTaskRef) {
		return fmt.Errorf("parent-only amendment has no matching Controller project authority")
	}
	if err := verifyTaskOnlyAuthorityDelta(s.identity.PrimaryRoot, committed.Snapshot.HeadOID, revised.Snapshot.HeadOID, attempt.SemanticTaskRef.TaskPath); err != nil {
		return fmt.Errorf("parent-only amendment changed non-Task authority: %w", err)
	}
	return nil
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
	if op.Episode == nil {
		if op.Source.Head.ActiveEpisodeID != "" || op.Transition.TargetEpisodeRevision != 0 {
			return fmt.Errorf("parent-only Root episode authority is missing")
		}
		return nil
	}
	if op.Source.Head.ActiveEpisodeID != op.Episode.EpisodeID || op.Source.Head.ActiveEpisodeRevision+1 != op.Episode.Revision ||
		op.Episode.State != EpisodeStateClosed || op.Transition.TargetEpisodeRevision != op.Episode.Revision ||
		op.Episode.SourceControllerGeneration != op.Transition.SourceGeneration || !op.Episode.RootTaskRef.Equal(op.Attempt.RootTaskRef) {
		return fmt.Errorf("parent-only Root closure differs from episode authority")
	}
	previous, err := s.LoadEpisodeRevision(op.Episode.EpisodeID, op.Source.Head.ActiveEpisodeRevision)
	if err != nil {
		return err
	}
	if previous.State != EpisodeStateResumingRoot || previous.RevisionID != op.Episode.PreviousRevisionID ||
		!taskPathSatisfied(previous.SatisfiedTaskRefs, previous.ScopeRootTaskRef.TaskPath) {
		return fmt.Errorf("parent-only Root closure predecessor is not satisfied")
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
	if err := s.bindParentOnlySmokeTaskID(*op.Attempt); err != nil {
		return err
	}
	actual := map[string]string{op.Transition.Effects[0].Key(): parentOnlyMaterializationIdentity(workspace, snapshot)}
	_, err = s.commitAuthorityTransitionLocked(op.Transition, actual, false, func(next *RepositoryControllerHead) error {
		if op.Episode != nil {
			if err := s.writeEpisodeRevision(*op.Episode); err != nil {
				return err
			}
			next.ActiveEpisodeID = ""
			next.ActiveEpisodeRevision = 0
		}
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

func (s *Store) bindParentOnlySmokeTaskID(attempt AttemptRecord) error {
	if attempt.ExecutionAuthority != ExecutionAuthorityParentOnly || attempt.AttemptID == "" {
		return fmt.Errorf("parent-only smoke identity requires a bound parent-only attempt")
	}
	cfg := WorkflowTaskConfig(s.config, s.identity, attempt.SemanticTaskRef)
	runtime := state.AttachStateStore(cfg)
	if err := os.MkdirAll(runtime.Path("."), 0o700); err != nil {
		return fmt.Errorf("prepare parent-only smoke store: %w", err)
	}
	if info, err := os.Lstat(runtime.Path("task.id")); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("parent-only smoke identity is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat parent-only smoke identity: %w", err)
	}
	if current, err := runtime.Read("task.id"); err == nil && current == attempt.AttemptID {
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read parent-only smoke identity: %w", err)
	}
	return runtime.Write("task.id", attempt.AttemptID)
}

func (s *Store) verifyCommittedParentOnlyExecution(op ExecutionOperation, head RepositoryControllerHead) error {
	if !parentOnlyHeadBindingsMatch(op, head) {
		return fmt.Errorf("committed parent-only execution bindings differ from operation")
	}
	if op.Episode != nil {
		if head.ActiveEpisodeID != "" || head.ActiveEpisodeRevision != 0 {
			return fmt.Errorf("committed parent-only execution left its Root episode active")
		}
		closed, err := s.LoadEpisodeRevision(op.Episode.EpisodeID, op.Episode.Revision)
		if err != nil {
			return err
		}
		if closed.RevisionID != op.Episode.RevisionID || closed.State != EpisodeStateClosed {
			return fmt.Errorf("committed parent-only Root episode closure differs")
		}
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
	runtime := state.AttachStateStore(WorkflowTaskConfig(s.config, s.identity, attempt.SemanticTaskRef))
	taskID, err := runtime.Read("task.id")
	if err != nil {
		return fmt.Errorf("read committed parent-only smoke identity: %w", err)
	}
	if taskID != attempt.AttemptID {
		return fmt.Errorf("committed parent-only smoke identity does not match attempt %s", attempt.AttemptID)
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

func (s *Store) ParentOnlySmokeIdentityMatches(taskID string) bool {
	head, err := s.LoadHead()
	if err != nil || head.Status != ControllerStatusActive {
		return false
	}
	if head.LiveAttemptID != "" && head.LiveAttemptID != taskID {
		return false
	}
	if head.AcceptedCandidateRef != nil {
		candidate, err := s.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
		if err != nil || candidate.AttemptID != taskID {
			return false
		}
	}
	attempt, err := s.loadAttempt(taskID)
	if err != nil || attempt.ExecutionAuthority != ExecutionAuthorityParentOnly {
		return false
	}
	authority, err := ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	if err != nil || authority.ExecutionMode != ExecutionAuthorityParentOnly {
		return false
	}
	if head.LiveAttemptID != "" {
		return s.parentOnlyCurrentTaskContract(head, attempt, authority) == nil
	}
	// A sealed parent-only candidate retains the attempt identity after its
	// live lease is released. Validate its accepted Task/source independently.
	candidate, err := s.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
	if err != nil || !candidate.TaskRef.Equal(attempt.SemanticTaskRef) ||
		!candidate.RootTaskRef.Equal(attempt.RootTaskRef) ||
		candidate.ProjectSnapshotID != head.ProjectSnapshotID ||
		authority.Task.TaskPath != attempt.SemanticTaskRef.TaskPath ||
		authority.Snapshot.HeadOID != attempt.ExecutionBaseOID {
		return false
	}
	if authority.Task.Equal(attempt.SemanticTaskRef) {
		return true
	}
	project, err := s.LoadProjectSnapshot(head.ProjectSnapshotID)
	if err != nil {
		return false
	}
	revised, err := ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, project.HeadOID)
	if err != nil || revised.ProjectSnapshotID != head.ProjectSnapshotID ||
		revised.ExecutionMode != ExecutionAuthorityParentOnly ||
		!revised.Task.Equal(attempt.SemanticTaskRef) {
		return false
	}
	return verifyTaskOnlyAuthorityDelta(s.identity.PrimaryRoot, authority.Snapshot.HeadOID, revised.Snapshot.HeadOID, attempt.SemanticTaskRef.TaskPath) == nil
}

func (s *Store) LiveParentOnlyExecution() (bool, error) {
	head, err := s.LoadHead()
	if err != nil {
		return false, err
	}
	_, live, err := s.parentOnlyLiveExecution(head)
	return live, err
}
