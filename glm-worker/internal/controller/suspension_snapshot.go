package controller

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type SuspensionSnapshot struct {
	SchemaVersion           int             `json:"schema_version"`
	SnapshotID              string          `json:"snapshot_id"`
	RepositoryIdentity      string          `json:"repository_identity"`
	SemanticTaskRef         SemanticTaskRef `json:"semantic_task_ref"`
	RootTaskRef             SemanticTaskRef `json:"root_task_ref"`
	AttemptID               string          `json:"attempt_id"`
	WorkspaceID             string          `json:"workspace_id"`
	SourceProjectSnapshotID string          `json:"source_project_snapshot_id"`
	ControllerGeneration    uint64          `json:"controller_generation"`
	EpisodeID               string          `json:"episode_id,omitempty"`
	EpisodeRevision         uint64          `json:"episode_revision,omitempty"`
	ExecutionBaseOID        string          `json:"execution_base_oid"`
	Baseline                ExecutionTrees  `json:"baseline"`
	Current                 ExecutionTrees  `json:"current"`
	RetainedCommitOID       string          `json:"retained_commit_oid"`
}

func (s *Store) captureSuspensionLocked(admission Admission) (SuspensionSnapshot, error) {
	workspace, err := ResolveWorkspaceIdentity(admission.Workspace.Root, s.identity)
	if err != nil {
		return SuspensionSnapshot{}, err
	}
	if workspace != admission.Workspace {
		return SuspensionSnapshot{}, fmt.Errorf("suspension workspace identity is inconsistent")
	}
	current, err := s.AdmitMutation(admission.MutationAuthority(), admission.Workspace, admission.Snapshot)
	if err != nil {
		return SuspensionSnapshot{}, err
	}
	if current.Lease.InFlightCallID != "" {
		return SuspensionSnapshot{}, fmt.Errorf("suspension requires quiescent execution")
	}
	if current.Attempt.BaselineTrees.IndexTree == "" || current.Attempt.BaselineTrees.WorktreeTree == "" {
		return SuspensionSnapshot{}, fmt.Errorf("attempt has no lossless execution baseline")
	}
	if err := s.restoreExecutionBaseline(current.Workspace.Root, current.Attempt); err != nil {
		return SuspensionSnapshot{}, err
	}
	trees, err := CaptureExecutionTrees(current.Workspace.Root, current.Attempt.ExecutionBaseOID)
	if err != nil {
		return SuspensionSnapshot{}, err
	}
	observed, err := CaptureWorkspaceSnapshot(current.Workspace.Root)
	if err != nil {
		return SuspensionSnapshot{}, err
	}
	if observed != admission.Snapshot {
		return SuspensionSnapshot{}, fmt.Errorf("workspace changed during suspension capture")
	}
	result := SuspensionSnapshot{
		SchemaVersion: controllerSchemaVersion, RepositoryIdentity: s.identity.LineageID,
		SemanticTaskRef: current.Attempt.SemanticTaskRef, RootTaskRef: current.Attempt.RootTaskRef,
		AttemptID: current.Attempt.AttemptID, WorkspaceID: current.Workspace.ID,
		SourceProjectSnapshotID: current.Head.ProjectSnapshotID, ControllerGeneration: current.Head.ControllerGeneration,
		EpisodeID: current.Head.ActiveEpisodeID, EpisodeRevision: current.Head.ActiveEpisodeRevision,
		ExecutionBaseOID: current.Attempt.ExecutionBaseOID, Baseline: current.Attempt.BaselineTrees, Current: trees,
	}
	return prepareSuspension(current.Workspace.Root, result)
}

func prepareSuspension(repo string, snapshot SuspensionSnapshot) (SuspensionSnapshot, error) {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return SuspensionSnapshot{}, err
	}
	snapshot.SnapshotID = digestBytes(data)
	data, err = json.Marshal(snapshot)
	if err != nil {
		return SuspensionSnapshot{}, err
	}
	blob, err := runGitBinary(repo, data, "hash-object", "-w", "--stdin")
	if err != nil {
		return SuspensionSnapshot{}, err
	}
	treeInput := fmt.Sprintf("040000 tree %s\tbaseline-index\n040000 tree %s\tbaseline-worktree\n040000 tree %s\tcurrent-index\n040000 tree %s\tcurrent-worktree\n100644 blob %s\tmanifest\n", snapshot.Baseline.IndexTree, snapshot.Baseline.WorktreeTree, snapshot.Current.IndexTree, snapshot.Current.WorktreeTree, strings.TrimSpace(string(blob)))
	tree, err := runGitBinary(repo, []byte(treeInput), "mktree")
	if err != nil {
		return SuspensionSnapshot{}, err
	}
	root, err := suspensionTreeCommit(repo, strings.TrimSpace(string(tree)), snapshot.ExecutionBaseOID)
	if err != nil {
		return SuspensionSnapshot{}, err
	}
	snapshot.RetainedCommitOID = root
	return snapshot, nil
}

func (s *Store) retainSuspension(repo string, snapshot SuspensionSnapshot) error {
	actual, exists, err := readExecutionRef(repo, suspensionRef(snapshot.SnapshotID))
	if err != nil {
		return err
	}
	if exists && actual != snapshot.RetainedCommitOID {
		return fmt.Errorf("unexpected suspension ref state")
	}
	if !exists {
		if _, err := runGitBinary(repo, nil, "update-ref", suspensionRef(snapshot.SnapshotID), snapshot.RetainedCommitOID, ""); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(s.dir, "suspensions"), 0o700); err != nil {
		return err
	}
	if err := writeJSONAtomic(s.suspensionPath(snapshot.SnapshotID), snapshot); err != nil {
		return err
	}
	_, err = s.LoadSuspension(repo, snapshot.SnapshotID)
	return err
}

func removeSuspensionRef(repo string, snapshot SuspensionSnapshot) error {
	if _, err := runGitBinary(repo, nil, "update-ref", "-d", suspensionRef(snapshot.SnapshotID), snapshot.RetainedCommitOID); err != nil {
		return err
	}
	return nil
}

func (s *Store) LoadSuspension(repo, id string) (SuspensionSnapshot, error) {
	identity, err := ResolveRepositoryIdentity(repo)
	if err != nil {
		return SuspensionSnapshot{}, err
	}
	if identity.LineageID != s.identity.LineageID {
		return SuspensionSnapshot{}, fmt.Errorf("suspension repository lineage mismatch")
	}
	if !validSuspensionID(id) {
		return SuspensionSnapshot{}, fmt.Errorf("invalid suspension identity")
	}
	var snapshot SuspensionSnapshot
	if err := readJSON(s.suspensionPath(id), &snapshot); err != nil {
		return SuspensionSnapshot{}, err
	}
	if snapshot.SchemaVersion != controllerSchemaVersion || snapshot.RepositoryIdentity != s.identity.LineageID || snapshot.SnapshotID != id {
		return SuspensionSnapshot{}, fmt.Errorf("suspension manifest authority is invalid")
	}
	if err := verifySuspensionRoot(repo, snapshot); err != nil {
		return SuspensionSnapshot{}, err
	}
	return snapshot, nil
}

func verifySuspensionRoot(repo string, snapshot SuspensionSnapshot) error {
	root, err := gitTrimmed(repo, "rev-parse", "--verify", suspensionRef(snapshot.SnapshotID))
	if err != nil {
		return fmt.Errorf("retained suspension root missing: %w", err)
	}
	if root != snapshot.RetainedCommitOID {
		return fmt.Errorf("retained suspension root differs from manifest")
	}
	if err := verifySuspensionManifest(repo, root, snapshot); err != nil {
		return err
	}
	parent, err := gitTrimmed(repo, "rev-parse", root+"^")
	if err != nil || parent != snapshot.ExecutionBaseOID {
		return fmt.Errorf("suspension execution base mismatch")
	}
	for name, expected := range map[string]string{"baseline-index": snapshot.Baseline.IndexTree, "baseline-worktree": snapshot.Baseline.WorktreeTree, "current-index": snapshot.Current.IndexTree, "current-worktree": snapshot.Current.WorktreeTree} {
		actual, err := gitTrimmed(repo, "rev-parse", root+":"+name)
		if err != nil || actual != expected {
			return fmt.Errorf("retained suspension tree %s missing or corrupt", name)
		}
		if _, err := runGitBinary(repo, nil, "ls-tree", "-r", actual); err != nil {
			return err
		}
	}
	return nil
}

func verifySuspensionManifest(repo, root string, snapshot SuspensionSnapshot) error {
	data, err := runGitBinary(repo, nil, "cat-file", "blob", root+":manifest")
	if err != nil {
		return err
	}
	canonical := snapshot
	canonical.RetainedCommitOID = ""
	expected, err := json.Marshal(canonical)
	if err != nil {
		return err
	}
	if string(data) != string(expected) {
		return fmt.Errorf("retained suspension manifest is corrupt")
	}
	canonical.SnapshotID = ""
	identity, err := json.Marshal(canonical)
	if err != nil {
		return err
	}
	if digestBytes(identity) != snapshot.SnapshotID {
		return fmt.Errorf("suspension content identity mismatch")
	}
	return nil
}

func (s *Store) suspensionPath(id string) string {
	return filepath.Join(s.dir, "suspensions", id+".json")
}

func suspensionRef(id string) string { return "refs/glm-worker/suspensions/" + id }

func validSuspensionID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, value := range id {
		if !strings.ContainsRune("0123456789abcdef", value) {
			return false
		}
	}
	return true
}
