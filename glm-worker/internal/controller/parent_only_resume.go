package controller

import "fmt"

func (s *Store) planParentOnlyResumeSource(op *ExecutionOperation) error {
	if op.Episode == nil {
		return nil
	}
	suspended, exists, err := s.suspendedExecutionSnapshot(op.Source.Head, op.Attempt.SemanticTaskRef)
	if err != nil {
		return err
	}
	if !exists || suspended.WorkspaceID != op.Workspace.ID {
		return fmt.Errorf("parent-only Root resume requires its primary suspension")
	}
	before, err := parentOnlyResumeBeforeTrees(op.Workspace.Root, *suspended, op.Lease.ExpectedBaseOID)
	if err != nil {
		return err
	}
	current, err := CaptureExecutionTrees(op.Workspace.Root, op.Lease.ExpectedBaseOID, op.Attempt.SemanticTaskRef)
	if err != nil {
		return err
	}
	if current != before {
		return fmt.Errorf("parent-only suspended source changed before Root resume")
	}
	rebound, err := RebindSuspensionTrees(op.Workspace.Root, *suspended, op.Lease.ExpectedBaseOID)
	if err != nil {
		return err
	}
	op.Suspension, op.Rebound = suspended, &rebound
	op.Transition.Effects[0].ExpectedNew = laneMaterializationIdentity(*op.Workspace, rebound)
	return nil
}

func parentOnlyResumeBeforeTrees(repo string, suspended SuspensionSnapshot, base string) (ExecutionTrees, error) {
	before := ReboundSuspension{Baseline: suspended.Baseline, Current: suspended.Current}
	if err := normalizeReboundTrees(repo, base, &before, suspended.SemanticTaskRef); err != nil {
		return ExecutionTrees{}, err
	}
	return before.Current, nil
}

func (s *Store) applyParentOnlyResumeSource(op ExecutionOperation) (WorkspaceSnapshot, error) {
	snapshot, err := CaptureWorkspaceSnapshot(op.Workspace.Root)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	if snapshot.Head != op.Lease.ExpectedBaseOID {
		return WorkspaceSnapshot{}, fmt.Errorf("parent-only execution HEAD changed after prepare")
	}
	if op.Rebound == nil {
		if snapshot.ID != op.Lease.ExpectedWorkspaceSnapshotID {
			return WorkspaceSnapshot{}, fmt.Errorf("parent-only execution source changed after prepare")
		}
		return snapshot, nil
	}
	if snapshot.RefDigest != op.Source.Snapshot.RefDigest {
		return WorkspaceSnapshot{}, fmt.Errorf("parent-only Root resume refs changed after prepare")
	}
	return s.materializeParentOnlyResumeSource(op, snapshot)
}

func (s *Store) materializeParentOnlyResumeSource(op ExecutionOperation, snapshot WorkspaceSnapshot) (WorkspaceSnapshot, error) {
	suspended, err := s.LoadSuspension(op.Workspace.Root, op.Suspension.SnapshotID)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	if suspended != *op.Suspension {
		return WorkspaceSnapshot{}, fmt.Errorf("parent-only Root resume suspension differs")
	}
	rebound, err := RebindSuspensionTrees(op.Workspace.Root, suspended, op.Lease.ExpectedBaseOID)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	if rebound != *op.Rebound {
		return WorkspaceSnapshot{}, fmt.Errorf("parent-only Root resume target differs")
	}
	before, err := parentOnlyResumeBeforeTrees(op.Workspace.Root, suspended, op.Lease.ExpectedBaseOID)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	if _, err := CaptureExecutionTrees(op.Workspace.Root, op.Lease.ExpectedBaseOID, op.Attempt.SemanticTaskRef); err != nil {
		return WorkspaceSnapshot{}, err
	}
	if err := applyParentOnlyResumeIndex(op, snapshot); err != nil {
		return WorkspaceSnapshot{}, err
	}
	if err := applyParentOnlyResumeWorktree(op.Workspace.Root, before.WorktreeTree, rebound.Current.WorktreeTree); err != nil {
		return WorkspaceSnapshot{}, err
	}
	if _, err := runGitBinary(op.Workspace.Root, nil, "read-tree", rebound.Current.IndexTree); err != nil {
		return WorkspaceSnapshot{}, err
	}
	_, result, err := s.verifyMaterializedExecution(op)
	return result, err
}

func applyParentOnlyResumeIndex(op ExecutionOperation, snapshot WorkspaceSnapshot) error {
	if snapshot.IndexDigest == op.Source.Snapshot.IndexDigest {
		return nil
	}
	matches, err := sourceImportIndexTreeMatches(op.Workspace.Root, op.Rebound.Current.IndexTree, snapshot.IndexDigest)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("parent-only Root resume index changed after prepare")
	}
	return nil
}

func applyParentOnlyResumeWorktree(repo, beforeTree, afterTree string) error {
	before, err := sourceImportTreeEntries(repo, beforeTree)
	if err != nil {
		return err
	}
	after, err := sourceImportTreeEntries(repo, afterTree)
	if err != nil {
		return err
	}
	entries := append(append([]workspaceTreeEntry(nil), before...), after...)
	entries, err = captureCleanupParentActions(repo, entries)
	if err != nil {
		return err
	}
	if err := verifyMaterializationPaths(repo, entries, true); err != nil {
		return err
	}
	before, after, err = parentOnlyResumeFileEntries(before, after)
	if err != nil {
		return err
	}
	return applySourceTreeEntries(repo, before, after)
}

func parentOnlyResumeFileEntries(before, after []workspaceTreeEntry) ([]workspaceTreeEntry, []workspaceTreeEntry, error) {
	previous, next := sourceImportEntryMap(before), sourceImportEntryMap(after)
	for _, entries := range [][]workspaceTreeEntry{before, after} {
		for _, entry := range entries {
			if entry.mode == "160000" && previous[entry.path] != next[entry.path] {
				return nil, nil, fmt.Errorf("parent-only Root resume cannot replace gitlink %q", entry.path)
			}
		}
	}
	files := func(entries []workspaceTreeEntry) []workspaceTreeEntry {
		result := make([]workspaceTreeEntry, 0, len(entries))
		for _, entry := range entries {
			if entry.mode != "160000" {
				result = append(result, entry)
			}
		}
		return result
	}
	return files(before), files(after), nil
}
