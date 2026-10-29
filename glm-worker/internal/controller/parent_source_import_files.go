package controller

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (s *Store) parentSourceBeforeTree(op ExecutionOperation) (string, error) {
	revisionRef, err := s.operationTaskSourceRevision(op)
	if err != nil {
		return "", err
	}
	revision, err := s.LoadTaskSourceRevision(revisionRef)
	if err != nil {
		return "", err
	}
	for _, ref := range revision.EvidenceRefs {
		if ref.Kind == "git-object-archive" && ref.LogicalIdentity == op.Transition.TransitionID+":source-before" {
			roots, err := s.VerifyGitObjectArchive(ref)
			if err != nil {
				return "", err
			}
			return parentSourceBeforeTreeRoot(op.Workspace.Root, roots, op.Source.Snapshot.Head, op.Source.Snapshot.IndexDigest)
		}
	}
	return "", fmt.Errorf("source import before archive is missing")
}

func (s *Store) operationTaskSourceRevision(op ExecutionOperation) (EvidenceObjectRef, error) {
	for _, taskRef := range op.Evidence.TaskRevisionRefs {
		revision, err := s.LoadTaskIndexRevision(taskRef)
		if err != nil {
			return EvidenceObjectRef{}, err
		}
		for index := len(revision.SourceRevisions) - 1; index >= 0; index-- {
			source, err := s.LoadTaskSourceRevision(revision.SourceRevisions[index])
			if err != nil {
				return EvidenceObjectRef{}, err
			}
			if source.TransitionID == op.Transition.TransitionID {
				return revision.SourceRevisions[index], nil
			}
		}
	}
	return EvidenceObjectRef{}, fmt.Errorf("source import lacks its task source revision")
}

func parentSourceBeforeTreeRoot(repo string, roots []GitObjectArchiveRoot, head, indexDigest string) (string, error) {
	hasHead := false
	trees := make([]string, 0, 2)
	for _, root := range roots {
		switch {
		case root.Type == gitCommitObjectType && root.OID == head:
			hasHead = true
		case root.Type == gitTreeObjectType:
			trees = append(trees, root.OID)
		default:
			return "", fmt.Errorf("source import retention roots differ")
		}
	}
	if !hasHead || len(trees) == 0 || len(trees) > 2 {
		return "", fmt.Errorf("source import retention lacks protected roots")
	}
	indexTree, err := sourceImportIndexRoot(repo, trees, indexDigest)
	if err != nil {
		return "", err
	}
	for _, tree := range trees {
		if tree != indexTree {
			return tree, nil
		}
	}
	return indexTree, nil
}

func sourceImportIndexRoot(repo string, trees []string, indexDigest string) (string, error) {
	if indexDigest == "" {
		return "", fmt.Errorf("source import retention index digest is missing")
	}
	match := ""
	for _, tree := range trees {
		matches, err := sourceImportIndexTreeMatches(repo, tree, indexDigest)
		if err != nil {
			return "", err
		}
		if matches {
			if match != "" {
				return "", fmt.Errorf("source import retention index tree is ambiguous")
			}
			match = tree
		}
	}
	if match == "" {
		return "", fmt.Errorf("source import retention index tree does not match the source snapshot")
	}
	return match, nil
}

func sourceImportIndexTreeMatches(repo, tree, indexDigest string) (bool, error) {
	tempDir, err := os.MkdirTemp("", "parent-source-index-*")
	if err != nil {
		return false, fmt.Errorf("source import index verification temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()
	index := filepath.Join(tempDir, "index")
	if _, err := suspensionGit(repo, index, nil, "read-tree", tree); err != nil {
		return false, err
	}
	listing, err := suspensionGit(repo, index, nil, "ls-files", "-s")
	if err != nil {
		return false, err
	}
	return state.DigestIndexListing(listing) == indexDigest, nil
}

func (s *Store) applyParentSourceFiles(op ExecutionOperation) error {
	beforeTree, err := s.parentSourceBeforeTree(op)
	if err != nil {
		return err
	}
	before, err := sourceImportTreeEntries(op.Workspace.Root, beforeTree)
	if err != nil {
		return err
	}
	after, err := sourceImportTreeEntries(op.Workspace.Root, op.Rebound.Current.WorktreeTree)
	if err != nil {
		return err
	}
	entries := append([]workspaceTreeEntry(nil), before...)
	entries = append(entries, after...)
	entries, err = captureCleanupParentActions(op.Workspace.Root, entries)
	if err != nil {
		return err
	}
	if err := verifyLaneMaterializationPaths(op.Workspace.Root, entries); err != nil {
		return err
	}
	return applySourceTreeEntries(op.Workspace.Root, before, after)
}

func applySourceTreeEntries(repo string, before, after []workspaceTreeEntry) error {
	previous := make(map[string]workspaceTreeEntry, len(before))
	for _, entry := range before {
		previous[entry.path] = entry
	}
	for start := 0; start < len(after); start += workspaceBlobBatchSize {
		batch := after[start:min(start+workspaceBlobBatchSize, len(after))]
		prior := make([]workspaceTreeEntry, len(batch))
		for index, entry := range batch {
			prior[index] = previous[entry.path]
			delete(previous, entry.path)
		}
		if err := applySourceEntryBatch(repo, prior, batch); err != nil {
			return err
		}
	}
	remaining := make([]workspaceTreeEntry, 0, len(previous))
	for _, entry := range previous {
		remaining = append(remaining, entry)
	}
	for start := 0; start < len(remaining); start += workspaceBlobBatchSize {
		batch := remaining[start:min(start+workspaceBlobBatchSize, len(remaining))]
		if err := applySourceEntryBatch(repo, batch, make([]workspaceTreeEntry, len(batch))); err != nil {
			return err
		}
	}
	return nil
}

func applySourceEntryBatch(repo string, before, after []workspaceTreeEntry) error {
	blobs, err := readWorkspaceBlobs(repo, before, after)
	if err != nil {
		return err
	}
	for index, entry := range after {
		if err := applyParentSourceEntry(repo, before[index], entry, blobs); err != nil {
			return err
		}
	}
	return nil
}

func sourceImportTreeEntries(repo, tree string) ([]workspaceTreeEntry, error) {
	data, err := runGitBinary(repo, nil, "ls-tree", "-r", "-z", tree)
	if err != nil {
		return nil, err
	}
	return parseWorkspaceEntries(data, false)
}

func sourceImportEntryMatches(path string, entry workspaceTreeEntry, blobs workspaceBlobs) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return entry.path == "", nil
	}
	if err != nil {
		return false, err
	}
	if entry.path == "" {
		return false, nil
	}
	actual, mode, err := suspensionFileBytes(path, info)
	if err != nil {
		return false, err
	}
	data, err := blobs.content(entry)
	return mode == entry.mode && bytes.Equal(actual, data), err
}

func applyParentSourceEntry(repo string, before, after workspaceTreeEntry, blobs workspaceBlobs) error {
	relative := after.path
	if relative == "" {
		relative = before.path
	}
	path, err := suspensionWorktreePath(repo, relative)
	if err != nil {
		return err
	}
	matched, err := sourceImportEntryMatches(path, after, blobs)
	if err != nil || matched {
		return err
	}
	matched, err = sourceImportEntryMatches(path, before, blobs)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("unattributed parent source mutation at %q", relative)
	}
	if after.path == "" {
		return os.Remove(path)
	}
	if after.mode != "100644" && after.mode != "100755" {
		return fmt.Errorf("parent source update requires a regular file: %q", relative)
	}
	return writeParentSourceEntry(path, after, blobs)
}

func writeParentSourceEntry(path string, entry workspaceTreeEntry, blobs workspaceBlobs) error {
	data, err := blobs.content(entry)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if entry.mode == "100755" {
		mode = 0o755
	}
	return execfile.Write(path, data, mode)
}
