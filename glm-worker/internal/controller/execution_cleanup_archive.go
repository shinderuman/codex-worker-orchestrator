package controller

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func captureCleanupTrees(repo string) (ExecutionTrees, error) {
	indexed, err := suspensionIndexEntries(repo)
	if err != nil {
		return ExecutionTrees{}, err
	}
	for _, entry := range indexed {
		if entry.mode == "160000" {
			return ExecutionTrees{}, fmt.Errorf("cleanup cannot archive submodule workspace data")
		}
	}
	indexTree, err := cleanupEntryTree(repo, indexed)
	if err != nil {
		return ExecutionTrees{}, err
	}
	worktree, err := captureSuspensionWorktree(repo, indexed)
	if err != nil {
		return ExecutionTrees{}, err
	}
	parents, err := cleanupParentEntries(repo, indexed)
	if err != nil {
		return ExecutionTrees{}, err
	}
	for _, entry := range parents {
		captured, exists, err := captureSuspensionEntry(repo, entry)
		if err != nil {
			return ExecutionTrees{}, err
		}
		if exists {
			worktree = append(worktree, captured)
		}
	}
	if err := verifyLaneMaterializationPaths(repo, worktree); err != nil {
		return ExecutionTrees{}, fmt.Errorf("cleanup has unarchived filesystem data: %w", err)
	}
	worktreeTree, err := cleanupEntryTree(repo, worktree)
	return ExecutionTrees{IndexTree: indexTree, WorktreeTree: worktreeTree}, err
}

func cleanupParentEntries(repo string, indexed []workspaceTreeEntry) (map[string]workspaceTreeEntry, error) {
	parents := map[string]workspaceTreeEntry{}
	for _, entry := range indexed {
		if state.IsParentManagedPath(entry.path) {
			parents[entry.path] = entry
		}
	}
	other, err := runGitBinary(repo, nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, path := range strings.Split(string(other), "\x00") {
		if state.IsParentManagedPath(path) {
			parents[path] = workspaceTreeEntry{path: path}
		}
	}
	return parents, nil
}

func cleanupEntryTree(repo string, entries []workspaceTreeEntry) (string, error) {
	index, err := newSuspensionIndex(repo, "")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(index) }()
	if err := writeSuspensionEntries(repo, index, entries); err != nil {
		return "", err
	}
	return suspensionGitText(repo, index, nil, "write-tree")
}

func (s *Store) archiveCleanupWorkspace(op *ExecutionOperation, seal AttemptSeal) error {
	trees, err := captureCleanupTrees(op.Workspace.Root)
	if err != nil {
		return err
	}
	archive, _, err := s.CaptureGitObjectArchive(op.Workspace.Root, seal.AttemptID+":cleanup:"+op.Transition.TransitionID, []string{op.CleanupSnapshot.Head, trees.IndexTree, trees.WorktreeTree})
	if err != nil {
		return err
	}
	op.CleanupTrees = &trees
	op.CleanupArchive = &archive
	if err := s.verifyCleanupArchive(*op); err != nil {
		return err
	}
	if err := s.verifyCleanupWorkspace(*op); err != nil {
		return err
	}
	op.Evidence, err = s.cleanupRetentionEvidence(*op, seal)
	return err
}

func (s *Store) verifyCleanupArchive(op ExecutionOperation) error {
	if op.CleanupArchive == nil && op.CleanupTrees == nil {
		return nil
	}
	if op.CleanupArchive == nil || op.CleanupTrees == nil || op.CleanupSnapshot == nil {
		return fmt.Errorf("cleanup retention authority is incomplete")
	}
	roots, err := s.VerifyGitObjectArchive(*op.CleanupArchive)
	if err != nil {
		return err
	}
	expected := map[string]string{op.CleanupSnapshot.Head: "commit", op.CleanupTrees.IndexTree: "tree", op.CleanupTrees.WorktreeTree: "tree"}
	if len(roots) != len(expected) {
		return fmt.Errorf("cleanup archive roots differ from retained data")
	}
	for _, root := range roots {
		if expected[root.OID] != root.Type {
			return fmt.Errorf("cleanup archive does not retain exact index/worktree")
		}
	}
	return nil
}

func (s *Store) cleanupRetentionEvidence(op ExecutionOperation, seal AttemptSeal) (EvidencePublicationInput, error) {
	data, err := json.Marshal(struct {
		Workspace WorkspaceIdentity `json:"workspace"`
		Snapshot  WorkspaceSnapshot `json:"snapshot"`
		Trees     ExecutionTrees    `json:"trees"`
		Archive   EvidenceObjectRef `json:"archive"`
		Seal      EvidenceObjectRef `json:"seal"`
	}{*op.Workspace, *op.CleanupSnapshot, *op.CleanupTrees, *op.CleanupArchive, *op.SealRef})
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	receipt, err := s.PutEvidenceObject("runtime-artifact", "application/json", seal.AttemptID+":cleanup-retention:"+op.Transition.TransitionID, true, data)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	final, err := s.storeOperationFinalization(op, *op.SealRef, []EvidenceObjectRef{receipt, *op.CleanupArchive})
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	head, err := s.LoadHead()
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	ref, err := s.attemptTaskEvidenceRoot(head, AttemptRecord{AttemptID: seal.AttemptID, SemanticTaskRef: seal.SemanticTaskRef})
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	task, err := s.LoadTaskIndexRevision(ref)
	if err != nil {
		return EvidencePublicationInput{}, err
	}
	task.PreviousRevision = &ref
	task.Finalizations = append(task.Finalizations, final)
	task.ControllerGeneration = op.Transition.CommittedGeneration
	task.TransitionID = op.Transition.TransitionID
	task.CreatedAt = op.Transition.CreatedAt
	taskRef, _, err := s.StoreTaskIndexRevision(task)
	return EvidencePublicationInput{FinalizationRefs: []EvidenceObjectRef{final}, TaskRevisionRefs: []EvidenceObjectRef{taskRef}}, err
}
