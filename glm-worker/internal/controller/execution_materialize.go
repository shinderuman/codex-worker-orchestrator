package controller

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const executionWorkspaceIdentityFile = "controller-workspace.json"

func (s *Store) applyExecutionMaterialization(op ExecutionOperation) error {
	if err := validateMaterializationPayload(op); err != nil {
		return err
	}
	if err := s.ensureExecutionLaneRegistration(*op.Workspace, op.Rebound.BaseOID); err != nil {
		return err
	}
	if err := materializeExecutionTrees(*op.Workspace, *op.Rebound); err != nil {
		return err
	}
	workspace, snapshot, err := s.verifyMaterializedExecution(op)
	if err != nil {
		return err
	}
	attempt, lease := materializedExecutionRecords(op, snapshot)
	if err := s.writeMaterializedExecutionRecords(attempt, lease); err != nil {
		return err
	}
	return s.commitMaterializedExecution(op, workspace, attempt, lease)
}

func validateMaterializationPayload(op ExecutionOperation) error {
	if op.Workspace == nil || op.Rebound == nil || op.Attempt == nil || op.Lease == nil {
		return fmt.Errorf("materialization payload is incomplete")
	}
	return nil
}

func materializedExecutionRecords(op ExecutionOperation, snapshot WorkspaceSnapshot) (AttemptRecord, ExecutionLease) {
	attempt := *op.Attempt
	attempt.BaselineSnapshotID = digestStrings(op.Rebound.BaseOID, op.Rebound.Baseline.IndexTree, op.Rebound.Baseline.WorktreeTree)
	attempt.WorkspaceSnapshotID = snapshot.ID
	lease := *op.Lease
	lease.ExpectedWorkspaceSnapshotID = snapshot.ID
	return attempt, lease
}

func (s *Store) writeMaterializedExecutionRecords(attempt AttemptRecord, lease ExecutionLease) error {
	if err := s.writeAttempt(attempt); err != nil {
		return err
	}
	return s.writeLease(lease)
}

func (s *Store) commitMaterializedExecution(op ExecutionOperation, workspace WorkspaceIdentity, attempt AttemptRecord, lease ExecutionLease) error {
	actual := map[string]string{op.Transition.Effects[0].Key(): laneMaterializationIdentity(workspace, *op.Rebound)}
	_, _, err := s.commitAuthorityTransitionWithEvidenceLocked(op.Transition, actual, op.Evidence, func(next *RepositoryControllerHead) error {
		if op.Episode != nil && op.Episode.State == EpisodeStateClosed {
			if err := s.writeEpisodeRevision(*op.Episode); err != nil {
				return err
			}
			next.ActiveEpisodeID = ""
			next.ActiveEpisodeRevision = 0
		}
		next.AcceptedCandidateRef = nil
		next.LiveAttemptID = attempt.AttemptID
		next.LiveLeaseID = lease.LeaseID
		task := attempt.SemanticTaskRef
		next.ExecutionTaskRef = &task
		return nil
	})
	return err
}

func (s *Store) verifyMaterializedExecution(op ExecutionOperation) (WorkspaceIdentity, WorkspaceSnapshot, error) {
	workspace, err := ResolveWorkspaceIdentity(op.Workspace.Root, s.identity)
	if err != nil {
		return WorkspaceIdentity{}, WorkspaceSnapshot{}, err
	}
	if workspace != *op.Workspace {
		return WorkspaceIdentity{}, WorkspaceSnapshot{}, fmt.Errorf("materialized workspace identity mismatch")
	}
	trees, err := CaptureExecutionTrees(workspace.Root, op.Rebound.BaseOID)
	if err != nil {
		return WorkspaceIdentity{}, WorkspaceSnapshot{}, err
	}
	if trees != op.Rebound.Current {
		return WorkspaceIdentity{}, WorkspaceSnapshot{}, fmt.Errorf("materialized index/worktree postcondition mismatch")
	}
	snapshot, err := CaptureWorkspaceSnapshot(workspace.Root)
	if err != nil {
		return WorkspaceIdentity{}, WorkspaceSnapshot{}, err
	}
	return workspace, snapshot, nil
}

func (s *Store) ensureExecutionLaneRegistration(workspace WorkspaceIdentity, base string) error {
	_, err := os.Lstat(workspace.Root)
	if os.IsNotExist(err) {
		if _, err := os.Lstat(workspace.GitDir); err == nil || !os.IsNotExist(err) {
			return fmt.Errorf("unexpected execution Git directory before materialization")
		}
		if _, err := runGitBinary(s.identity.PrimaryRoot, nil, "worktree", "add", "--detach", "--no-checkout", workspace.Root, base); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	actual, err := s.verifyExecutionLaneRegistration(workspace, base)
	if err != nil {
		return err
	}
	cookie := filepath.Join(workspace.GitDir, executionWorkspaceIdentityFile)
	if _, err := os.Lstat(cookie); os.IsNotExist(err) {
		return writeJSONAtomic(cookie, workspace)
	}
	if err != nil {
		return err
	}
	if actual.ID != workspace.ID {
		return fmt.Errorf("execution workspace nonce mismatch")
	}
	return nil
}

func (s *Store) verifyExecutionLaneRegistration(workspace WorkspaceIdentity, base string) (WorkspaceIdentity, error) {
	actual, err := ResolveWorkspaceIdentity(workspace.Root, s.identity)
	if err != nil {
		return WorkspaceIdentity{}, err
	}
	if actual.Root != workspace.Root || actual.GitDir != workspace.GitDir {
		return WorkspaceIdentity{}, fmt.Errorf("execution lane registration is unexpected: root=%q gitdir=%q, expected root=%q gitdir=%q", actual.Root, actual.GitDir, workspace.Root, workspace.GitDir)
	}
	head, err := gitTrimmed(workspace.Root, "rev-parse", "HEAD")
	if err != nil || head != base {
		return WorkspaceIdentity{}, fmt.Errorf("execution lane base is unexpected")
	}
	branch, err := gitTrimmed(workspace.Root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || branch != "HEAD" {
		return WorkspaceIdentity{}, fmt.Errorf("execution lane is not detached")
	}
	return actual, nil
}

func materializeExecutionTrees(workspace WorkspaceIdentity, rebound ReboundSuspension) error {
	data, err := runGitBinary(workspace.Root, nil, "ls-tree", "-r", "-z", rebound.Current.WorktreeTree)
	if err != nil {
		return err
	}
	entries, err := parseWorkspaceEntries(data, false)
	if err != nil {
		return err
	}
	if err := verifyLaneMaterializationPaths(workspace.Root, entries); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := materializeExecutionEntry(workspace.Root, entry); err != nil {
			return err
		}
	}
	index, err := suspensionIndexEntries(workspace.Root)
	if err != nil {
		return err
	}
	if len(index) != 0 {
		actual, err := gitTrimmed(workspace.Root, "write-tree")
		if err != nil || actual != rebound.Current.IndexTree {
			return fmt.Errorf("execution index has unexpected partial state")
		}
		return nil
	}
	_, err = runGitBinary(workspace.Root, nil, "read-tree", rebound.Current.IndexTree)
	return err
}

func verifyLaneMaterializationPaths(root string, entries []workspaceTreeEntry) error {
	allowed := map[string]bool{".git": true}
	for _, entry := range entries {
		allowed[entry.path] = true
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !allowed[filepath.ToSlash(relative)] {
			return fmt.Errorf("unexpected materialization path %q", relative)
		}
		return nil
	})
}

func materializeExecutionEntry(repo string, entry workspaceTreeEntry) error {
	path, err := suspensionWorktreePath(repo, entry.path)
	if err != nil {
		return err
	}
	if entry.mode == "160000" {
		return os.MkdirAll(path, 0o755)
	}
	data, err := runGitBinary(repo, nil, "cat-file", "blob", entry.oid)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err == nil {
		actual, mode, err := suspensionFileBytes(path, info)
		if err != nil {
			return err
		}
		if mode != entry.mode || !bytes.Equal(actual, data) {
			return fmt.Errorf("unexpected partial materialization at %q", entry.path)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if entry.mode == "120000" {
		return os.Symlink(string(data), path)
	}
	mode := os.FileMode(0o644)
	if entry.mode == "100755" {
		mode = 0o755
	}
	return os.WriteFile(path, data, mode)
}

func resolveExecutionWorkspaceNonce(workspace WorkspaceIdentity) (WorkspaceIdentity, error) {
	path := filepath.Join(workspace.GitDir, executionWorkspaceIdentityFile)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return workspace, nil
	}
	if err != nil {
		return WorkspaceIdentity{}, err
	}
	if !info.Mode().IsRegular() {
		return WorkspaceIdentity{}, fmt.Errorf("execution workspace identity is not regular")
	}
	var recorded WorkspaceIdentity
	if err := readJSON(path, &recorded); err != nil {
		return WorkspaceIdentity{}, err
	}
	if recorded.ID == "" || recorded.RepositoryID != workspace.RepositoryID || recorded.Root != workspace.Root || recorded.GitDir != workspace.GitDir || strings.ContainsAny(recorded.ID, "/\\") {
		return WorkspaceIdentity{}, fmt.Errorf("execution workspace identity is corrupt")
	}
	return recorded, nil
}
