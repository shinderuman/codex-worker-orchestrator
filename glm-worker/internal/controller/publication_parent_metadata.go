package controller

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (s *Store) completionMetadataBase(admission Admission) (string, error) {
	base, err := s.projectMetadataBase(admission.Head.ProjectSnapshotID)
	if err != nil {
		return "", err
	}
	changed, err := s.parentCompletionMetadataChanged()
	if err != nil {
		return "", err
	}
	if !changed {
		return base, nil
	}
	return s.captureCompletionMetadata(admission)
}

func (s *Store) candidateMetadataBase(candidate AcceptedCandidate) (string, error) {
	base, err := s.projectMetadataBase(candidate.ProjectSnapshotID)
	if err != nil {
		return "", err
	}
	if candidate.ImplementationTree != "" {
		return candidate.ImplementationTree, nil
	}
	return base, nil
}

func (s *Store) parentCompletionMetadataChanged() (bool, error) {
	changes, err := primaryWorktreeChanges(s.identity.PrimaryRoot)
	if err != nil {
		return false, err
	}
	for path := range changes {
		if state.IsParentManagedPath(path) {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) captureCompletionMetadata(admission Admission) (string, error) {
	before, err := CaptureWorkspaceSnapshot(s.identity.PrimaryRoot)
	if err != nil {
		return "", err
	}
	if before.Head != admission.Head.IntegrationTip {
		return "", fmt.Errorf("parent metadata revision has a different integration base")
	}
	digest, exists, err := worktreeRegularFileDigest(s.identity.PrimaryRoot, admission.Attempt.SemanticTaskRef.TaskPath)
	if err != nil {
		return "", err
	}
	if !exists || digest != admission.Attempt.SemanticTaskRef.ContractDigest {
		return "", fmt.Errorf("active Task contract requires authority synchronization before completion")
	}
	tree, err := captureParentSourceTree(s.identity.PrimaryRoot)
	if err != nil {
		return "", err
	}
	commit, err := suspensionTreeCommit(s.identity.PrimaryRoot, tree, before.Head)
	if err != nil {
		return "", err
	}
	authority, err := ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, commit)
	if err != nil {
		return "", err
	}
	if !authority.Task.Equal(admission.Attempt.SemanticTaskRef) {
		return "", fmt.Errorf("parent metadata changes the active Task authority")
	}
	after, err := CaptureWorkspaceSnapshot(s.identity.PrimaryRoot)
	if err != nil {
		return "", err
	}
	if after != before {
		return "", fmt.Errorf("parent metadata changed during candidate capture")
	}
	return commit, nil
}
