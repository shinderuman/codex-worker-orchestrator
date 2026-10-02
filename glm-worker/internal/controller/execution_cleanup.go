package controller

import (
	"fmt"
	"os"
	"path/filepath"
)

type CleanupExecutionInput struct {
	ExpectedGeneration uint64            `json:"expected_generation"`
	WorkspaceID        string            `json:"workspace_id"`
	SealRef            EvidenceObjectRef `json:"seal_ref"`
}

func (s *Store) CleanupExecution(input CleanupExecutionInput) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	op, head, err := s.planExecutionCleanup(input)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.prepareExecutionOperation(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func (s *Store) planExecutionCleanup(input CleanupExecutionInput) (ExecutionOperation, RepositoryControllerHead, error) {
	head, err := s.quiescentExecutionHead(input.ExpectedGeneration)
	if err != nil {
		return ExecutionOperation{}, RepositoryControllerHead{}, err
	}
	proof, err := s.ProveCleanupDurability(input.SealRef)
	if err != nil {
		return ExecutionOperation{}, RepositoryControllerHead{}, err
	}
	seal, err := s.LoadAttemptSeal(input.SealRef)
	if err != nil {
		return ExecutionOperation{}, RepositoryControllerHead{}, err
	}
	if proof.ControllerGeneration != head.ControllerGeneration || seal.WorkspaceID != input.WorkspaceID {
		return ExecutionOperation{}, RepositoryControllerHead{}, fmt.Errorf("cleanup seal/workspace authority is stale")
	}
	base, err := canonicalPath(s.dir)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	root := filepath.Join(base, s.identity.LineageID[:16]+"-lane")
	workspace, err := ResolveWorkspaceIdentity(root, s.identity)
	if err != nil {
		return ExecutionOperation{}, RepositoryControllerHead{}, err
	}
	if workspace.ID != input.WorkspaceID {
		return ExecutionOperation{}, RepositoryControllerHead{}, fmt.Errorf("cleanup workspace identity does not match sealed attempt")
	}
	if err := s.verifySealedCleanupContent(workspace, seal); err != nil {
		return ExecutionOperation{}, head, err
	}
	snapshot, err := CaptureWorkspaceSnapshot(root)
	if err != nil {
		return ExecutionOperation{}, RepositoryControllerHead{}, err
	}
	record, err := executionTransition(head, executionCleanup)
	if err != nil {
		return ExecutionOperation{}, RepositoryControllerHead{}, err
	}
	record.Effects = []EffectExpectation{{Surface: MutationSurfaceCleanup, Resource: root, ExpectedOld: snapshot.ID}}
	op := ExecutionOperation{Transition: record, Workspace: &workspace, SealRef: &input.SealRef, CleanupSnapshot: &snapshot}
	return op, head, nil
}

func (s *Store) applyExecutionCleanup(op ExecutionOperation) error {
	if op.Workspace == nil || op.CleanupSnapshot == nil || op.SealRef == nil {
		return fmt.Errorf("cleanup operation is incomplete")
	}
	if _, err := s.ProveCleanupDurability(*op.SealRef); err != nil {
		return err
	}
	root := op.Workspace.Root
	_, rootErr := os.Lstat(root)
	_, gitErr := os.Lstat(op.Workspace.GitDir)
	if os.IsNotExist(rootErr) && os.IsNotExist(gitErr) {
		return s.commitExecutionCleanup(op)
	}
	if rootErr != nil || gitErr != nil {
		return fmt.Errorf("cleanup has unexpected partial workspace registration")
	}
	if err := s.verifyCleanupWorkspace(op); err != nil {
		return err
	}
	if _, err := runGitBinary(s.identity.PrimaryRoot, nil, "worktree", "remove", "--force", root); err != nil {
		return fmt.Errorf("execution cleanup remains pending: %w", err)
	}
	if err := verifyExecutionLaneAbsent(*op.Workspace); err != nil {
		return err
	}
	return s.commitExecutionCleanup(op)
}

func (s *Store) verifyCleanupWorkspace(op ExecutionOperation) error {
	root := op.Workspace.Root
	workspace, err := ResolveWorkspaceIdentity(root, s.identity)
	if err != nil {
		return err
	}
	if workspace != *op.Workspace {
		return fmt.Errorf("cleanup target was replaced")
	}
	snapshot, err := CaptureWorkspaceSnapshot(root)
	if err != nil {
		return err
	}
	if snapshot != *op.CleanupSnapshot {
		return fmt.Errorf("cleanup workspace changed after prepare")
	}
	return nil
}

func (s *Store) commitExecutionCleanup(op ExecutionOperation) error {
	actual := map[string]string{op.Transition.Effects[0].Key(): ""}
	_, err := s.commitAuthorityTransitionLocked(op.Transition, actual, false, nil)
	return err
}

func (s *Store) quiescentExecutionHead(generation uint64) (RepositoryControllerHead, error) {
	head, err := s.LoadHead()
	if err != nil {
		return head, err
	}
	if head.Status != ControllerStatusActive || head.ControllerGeneration != generation || head.PendingTransitionID != "" || head.LiveLeaseID != "" {
		return head, fmt.Errorf("execution cleanup requires exact quiescent controller authority")
	}
	return head, nil
}

func (s *Store) GarbageCollectSuspension(expectedGeneration uint64, id string) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	head, err := s.quiescentExecutionHead(expectedGeneration)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	snapshot, err := s.LoadSuspension(s.identity.PrimaryRoot, id)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if snapshot.SemanticTaskRef.Equal(snapshot.RootTaskRef) && head.ActiveEpisodeID != "" {
		return ExecutionOperationResult{}, fmt.Errorf("root suspension is retained until episode closure")
	}
	sealRef, err := s.suspensionSealRef(snapshot)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if _, err := s.ProveCleanupDurability(sealRef); err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.proveSuspensionSuccessor(snapshot, sealRef, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	record, err := executionTransition(head, executionGC)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	record.Effects = []EffectExpectation{{Surface: MutationSurfaceRef, Resource: suspensionRef(id), ExpectedOld: snapshot.RetainedCommitOID}}
	op := ExecutionOperation{Transition: record, Suspension: &snapshot, SealRef: &sealRef}
	if err := s.prepareExecutionOperation(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func (s *Store) proveSuspensionSuccessor(snapshot SuspensionSnapshot, seal EvidenceObjectRef, head RepositoryControllerHead) error {
	entries, err := os.ReadDir(filepath.Join(s.dir, "attempts"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		var attempt AttemptRecord
		if err := readJSON(filepath.Join(s.dir, "attempts", entry.Name()), &attempt); err != nil {
			return err
		}
		if attempt.PredecessorAttemptID == snapshot.AttemptID && attempt.ResumedFromSealID == seal.LogicalIdentity && attempt.StartControllerGeneration <= head.ControllerGeneration && attempt.AttemptState != AttemptStatePrepared {
			return nil
		}
	}
	return fmt.Errorf("suspension has no durably materialized successor")
}

func (s *Store) applySuspensionGC(op ExecutionOperation) error {
	if op.Suspension == nil || op.SealRef == nil {
		return fmt.Errorf("snapshot GC payload is incomplete")
	}
	if _, err := s.ProveCleanupDurability(*op.SealRef); err != nil {
		return err
	}
	actual, exists, err := readExecutionRef(s.identity.PrimaryRoot, suspensionRef(op.Suspension.SnapshotID))
	if err != nil {
		return err
	}
	if exists {
		if actual != op.Suspension.RetainedCommitOID {
			return fmt.Errorf("GC suspension ref was replaced")
		}
		if err := removeSuspensionRef(s.identity.PrimaryRoot, *op.Suspension); err != nil {
			return err
		}
	}
	return s.commitExecutionCleanup(op)
}

func (s *Store) verifySealedCleanupContent(workspace WorkspaceIdentity, seal AttemptSeal) error {
	root := workspace.Root
	attempt, err := s.loadAttempt(seal.AttemptID)
	if err != nil {
		return err
	}
	if !sealedExecutionAttemptState(attempt.AttemptState) {
		return fmt.Errorf("cleanup attempt is not suspended or accepted")
	}
	trees, err := CaptureExecutionTrees(root, seal.ExecutionBaseOID)
	if err != nil {
		return err
	}
	if trees.IndexTree != seal.CurrentIndexTree || trees.WorktreeTree != seal.CurrentWorktreeTree {
		return fmt.Errorf("cleanup workspace differs from sealed evidence")
	}
	return nil
}

func verifyExecutionLaneAbsent(workspace WorkspaceIdentity) error {
	for _, path := range []string{workspace.Root, workspace.GitDir} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return fmt.Errorf("execution cleanup postcondition failed at %q", path)
		}
	}
	return nil
}

func sealedExecutionAttemptState(value AttemptState) bool {
	switch value {
	case AttemptStateSuspendedForBlocker, AttemptStateSuspendedForAdvancement, AttemptStateAccepted:
		return true
	default:
		return false
	}
}
