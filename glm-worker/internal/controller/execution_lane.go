package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type MaterializeExecutionInput struct {
	ExpectedGeneration uint64 `json:"expected_generation"`
	EpisodeID          string `json:"episode_id"`
	EpisodeRevision    uint64 `json:"episode_revision"`
	SuspensionID       string `json:"suspension_id,omitempty"`
}

func (s *Store) MaterializeExecution(input MaterializeExecutionInput) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	op, head, err := s.planExecutionMaterialization(input)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.prepareExecutionOperation(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func (s *Store) planExecutionMaterialization(input MaterializeExecutionInput) (ExecutionOperation, RepositoryControllerHead, error) {
	head, episode, task, err := s.materializationAuthority(input)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	rebound, suspended, seal, err := s.materializationTrees(head, task, input.SuspensionID)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	workspace, err := s.plannedExecutionLane()
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	attempt, lease, err := plannedLaneExecution(head, task, workspace, rebound)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	if err := s.archiveExecutionBaseline(s.identity.PrimaryRoot, &attempt); err != nil {
		return ExecutionOperation{}, head, err
	}
	if suspended != nil {
		attempt.PredecessorAttemptID = suspended.AttemptID
		attempt.ResumedFromSealID = seal.LogicalIdentity
	}
	record, err := executionTransition(head, executionMaterialize)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	record.TargetAttemptID = attempt.AttemptID
	record.TargetLeaseID = lease.LeaseID
	record.TargetWorkspaceID = workspace.ID
	record.TargetExecutionTaskRef = task
	record.TargetRootTaskRef = *head.RootTaskRef
	record.Effects = []EffectExpectation{{Surface: MutationSurfaceSource, Resource: workspace.Root, ExpectedNew: laneMaterializationIdentity(workspace, rebound)}}
	op := ExecutionOperation{Transition: record, Suspension: suspended, SealRef: seal, Episode: &episode, Rebound: &rebound, Workspace: &workspace, Attempt: &attempt, Lease: &lease}
	return op, head, nil
}

func (s *Store) materializationAuthority(input MaterializeExecutionInput) (RepositoryControllerHead, BlockerEpisodeRevision, SemanticTaskRef, error) {
	head, err := s.LoadHead()
	if err != nil {
		return head, BlockerEpisodeRevision{}, SemanticTaskRef{}, err
	}
	if err := validateMaterializationHead(head, input); err != nil {
		return head, BlockerEpisodeRevision{}, SemanticTaskRef{}, err
	}
	if input.EpisodeID == "" && head.ActiveEpisodeID == "" {
		if head.ExecutionTaskRef == nil || input.SuspensionID == "" {
			return head, BlockerEpisodeRevision{}, SemanticTaskRef{}, fmt.Errorf("ordinary resume requires exact suspended execution")
		}
		return head, BlockerEpisodeRevision{}, *head.ExecutionTaskRef, nil
	}
	episode, err := s.LoadEpisodeRevision(input.EpisodeID, input.EpisodeRevision)
	if err != nil {
		return head, episode, SemanticTaskRef{}, err
	}
	if episode.ProjectSnapshotID != head.ProjectSnapshotID {
		return head, episode, SemanticTaskRef{}, fmt.Errorf("materialization project authority is stale")
	}
	schedule, err := s.scheduleEpisodeAgainstProject(episode)
	if err != nil {
		return head, episode, SemanticTaskRef{}, err
	}
	if schedule.Intent == FindingIntentResumeRootTask {
		return head, episode, *head.RootTaskRef, nil
	}
	if schedule.NextTaskRef == nil {
		return head, episode, SemanticTaskRef{}, fmt.Errorf("episode has no runnable execution: %s", schedule.Reason)
	}
	return head, episode, *schedule.NextTaskRef, nil
}

func (s *Store) materializationTrees(head RepositoryControllerHead, task SemanticTaskRef, id string) (ReboundSuspension, *SuspensionSnapshot, *EvidenceObjectRef, error) {
	if id == "" {
		tree, err := gitTrimmed(s.identity.PrimaryRoot, "rev-parse", head.IntegrationTip+"^{tree}")
		if err != nil {
			return ReboundSuspension{}, nil, nil, err
		}
		trees := ExecutionTrees{IndexTree: tree, WorktreeTree: tree}
		result := ReboundSuspension{BaseOID: head.IntegrationTip, Baseline: trees, Current: trees}
		if err := normalizeReboundTrees(s.identity.PrimaryRoot, head.IntegrationTip, &result); err != nil {
			return ReboundSuspension{}, nil, nil, err
		}
		if s.hasSuspendedExecution(task) {
			return ReboundSuspension{}, nil, nil, fmt.Errorf("resumed Task requires its exact suspension identity")
		}
		return result, nil, nil, nil
	}
	snapshot, err := s.LoadSuspension(s.identity.PrimaryRoot, id)
	if err != nil {
		return ReboundSuspension{}, nil, nil, err
	}
	if !snapshot.SemanticTaskRef.Equal(task) || !snapshot.RootTaskRef.Equal(*head.RootTaskRef) {
		return ReboundSuspension{}, nil, nil, fmt.Errorf("suspension does not belong to scheduled execution")
	}
	seal, err := s.suspensionSealRef(snapshot)
	if err != nil {
		return ReboundSuspension{}, nil, nil, err
	}
	if _, err := s.ProveCleanupDurability(seal); err != nil {
		return ReboundSuspension{}, nil, nil, err
	}
	result, err := RebindSuspensionTrees(s.identity.PrimaryRoot, snapshot, head.IntegrationTip)
	if err != nil {
		return result, &snapshot, &seal, s.preservePublicationConflict(head, &snapshot, err)
	}
	return result, &snapshot, &seal, nil
}

func (s *Store) hasSuspendedExecution(task SemanticTaskRef) bool {
	entries, err := os.ReadDir(filepath.Join(s.dir, "suspensions"))
	if err != nil {
		return !os.IsNotExist(err)
	}
	for _, entry := range entries {
		var snapshot SuspensionSnapshot
		if err := readJSON(filepath.Join(s.dir, "suspensions", entry.Name()), &snapshot); err != nil {
			return true
		}
		if snapshot.SemanticTaskRef.TaskPath == task.TaskPath {
			return true
		}
	}
	return false
}

func (s *Store) plannedExecutionLane() (WorkspaceIdentity, error) {
	name := s.identity.LineageID[:16] + "-lane"
	base, err := canonicalPath(s.dir)
	if err != nil {
		return WorkspaceIdentity{}, err
	}
	root := filepath.Join(base, name)
	gitDir := filepath.Join(s.identity.CommonDir, "worktrees", name)
	for _, path := range []string{root, gitDir} {
		if _, err := os.Lstat(path); err == nil || !os.IsNotExist(err) {
			return WorkspaceIdentity{}, fmt.Errorf("execution lane is occupied: %s", path)
		}
	}
	id, err := state.NewUUID()
	return WorkspaceIdentity{ID: id, RepositoryID: s.identity.LineageID, Root: root, GitDir: gitDir}, err
}

func plannedLaneExecution(head RepositoryControllerHead, task SemanticTaskRef, workspace WorkspaceIdentity, rebound ReboundSuspension) (AttemptRecord, ExecutionLease, error) {
	attemptID, err := state.NewUUID()
	if err != nil {
		return AttemptRecord{}, ExecutionLease{}, err
	}
	leaseID, err := state.NewUUID()
	if err != nil {
		return AttemptRecord{}, ExecutionLease{}, err
	}
	generation := head.ControllerGeneration + 3
	now := time.Now().UTC()
	attempt := AttemptRecord{SchemaVersion: controllerSchemaVersion, AttemptID: attemptID, SemanticTaskRef: task, RootTaskRef: *head.RootTaskRef, EpisodeID: head.ActiveEpisodeID, EpisodeRevision: head.ActiveEpisodeRevision, ExecutionBaseOID: rebound.BaseOID, BaselineTrees: rebound.Baseline, StartControllerGeneration: generation, AttemptState: AttemptStateLive, CreatedAt: now}
	lease := ExecutionLease{SchemaVersion: controllerSchemaVersion, LeaseID: leaseID, AttemptID: attemptID, SemanticTaskRef: task, Purpose: "episode-execution", ControllerGeneration: generation, EpisodeID: head.ActiveEpisodeID, EpisodeRevision: head.ActiveEpisodeRevision, WorkspaceID: workspace.ID, ExpectedBaseOID: rebound.BaseOID, CreatedAt: now}
	return attempt, lease, nil
}

func laneMaterializationIdentity(workspace WorkspaceIdentity, rebound ReboundSuspension) string {
	return digestStrings(workspace.ID, workspace.RepositoryID, workspace.Root, workspace.GitDir, rebound.BaseOID, rebound.Current.IndexTree, rebound.Current.WorktreeTree)
}

func validateMaterializationHead(head RepositoryControllerHead, input MaterializeExecutionInput) error {
	if head.Status != ControllerStatusActive || head.ControllerGeneration != input.ExpectedGeneration || head.LiveLeaseID != "" || head.PendingTransitionID != "" || head.RootTaskRef == nil || head.IntegrationTip == "" {
		return fmt.Errorf("materialization requires exact quiescent controller authority")
	}
	if head.ObservedRemoteTip != "" && head.ObservedRemoteTip != head.IntegrationTip {
		return fmt.Errorf("observed external advancement must be adopted before materialization")
	}
	if head.ActiveEpisodeID != input.EpisodeID || head.ActiveEpisodeRevision != input.EpisodeRevision {
		return fmt.Errorf("materialization episode is stale")
	}
	return nil
}
