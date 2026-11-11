package controller

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

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
	beforeDirectories := sourceTreeDirectories(before)
	afterDirectories := sourceTreeDirectories(after)
	if err := validateSourceTreeReplacementDirectories(repo, before, after, beforeDirectories, afterDirectories); err != nil {
		return err
	}
	previous := sourceImportEntryMap(before)
	next := sourceImportEntryMap(after)
	removed := make([]workspaceTreeEntry, 0)
	for _, entry := range before {
		if _, remains := next[entry.path]; remains {
			continue
		}
		if sourceTreeReplacementDirectoryPresent(repo, entry.path, afterDirectories) ||
			sourceTreeAncestorReplacementPresent(repo, entry.path, next) {
			continue
		}
		removed = append(removed, entry)
	}
	for start := 0; start < len(removed); start += workspaceBlobBatchSize {
		batch := removed[start:min(start+workspaceBlobBatchSize, len(removed))]
		if err := applySourceEntryBatch(repo, batch, make([]workspaceTreeEntry, len(batch))); err != nil {
			return err
		}
	}
	return applySourceTreeAddedEntries(repo, previous, beforeDirectories, after)
}

func validateSourceTreeReplacementDirectories(repo string, before, after []workspaceTreeEntry, beforeDirectories, afterDirectories map[string]bool) error {
	entries := append(append([]workspaceTreeEntry(nil), before...), after...)
	for _, entry := range entries {
		if err := validateSuspensionPath(entry.path); err != nil {
			return err
		}
	}
	directories := make(map[string]bool, len(beforeDirectories)+len(afterDirectories))
	for path := range beforeDirectories {
		directories[path] = true
	}
	for path := range afterDirectories {
		directories[path] = true
	}
	known := sourceImportEntryMap(entries)
	validated := make(map[string]bool)
	for _, entry := range entries {
		if !directories[entry.path] || validated[entry.path] {
			continue
		}
		if err := validateSourceTreeReplacementDirectory(repo, entry.path, directories, known); err != nil {
			return err
		}
		validated[entry.path] = true
	}
	return nil
}

func sourceTreeDirectories(entries []workspaceTreeEntry) map[string]bool {
	directories := make(map[string]bool)
	for _, entry := range entries {
		for directory := filepath.ToSlash(filepath.Dir(entry.path)); directory != "."; directory = filepath.ToSlash(filepath.Dir(directory)) {
			directories[directory] = true
		}
	}
	return directories
}

func validateSourceTreeReplacementDirectory(repo, root string, directories map[string]bool, known map[string]workspaceTreeEntry) error {
	abs, err := suspensionWorktreePath(repo, root)
	if err != nil {
		return err
	}
	info, err := os.Lstat(abs)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	rootAbs, err := filepath.Abs(abs)
	if err != nil {
		return err
	}
	return filepath.WalkDir(rootAbs, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(rootAbs, current)
		if err != nil {
			return err
		}
		path := filepath.ToSlash(filepath.Join(root, relative))
		if sourceTreeReplacementEntryOwned(path, entry, directories, known) {
			return nil
		}
		return fmt.Errorf("unattributed source tree replacement path at %q", path)
	})
}

func sourceTreeReplacementEntryOwned(path string, entry os.DirEntry, directories map[string]bool, known map[string]workspaceTreeEntry) bool {
	if entry.IsDir() {
		return directories[path]
	}
	return known[path].path != "" && (entry.Type().IsRegular() || entry.Type() == os.ModeSymlink)
}

func applySourceTreeAddedEntries(repo string, previous map[string]workspaceTreeEntry, beforeDirectories map[string]bool, after []workspaceTreeEntry) error {
	var directoryChildren map[string][]string
	for start := 0; start < len(after); start += workspaceBlobBatchSize {
		batch := after[start:min(start+workspaceBlobBatchSize, len(after))]
		prior := make([]workspaceTreeEntry, len(batch))
		for index, entry := range batch {
			prior[index] = previous[entry.path]
			if !beforeDirectories[entry.path] {
				continue
			}
			if directoryChildren == nil {
				directoryChildren = sourceTreeDirectoryChildren(beforeDirectories)
			}
			if err := removeSourceTreeReplacedDirectory(repo, entry.path, directoryChildren); err != nil {
				return err
			}
		}
		if err := applySourceEntryBatch(repo, prior, batch); err != nil {
			return err
		}
	}
	return nil
}

func sourceTreeReplacementDirectoryPresent(repo, path string, afterDirectories map[string]bool) bool {
	if !afterDirectories[path] {
		return false
	}
	info, err := os.Lstat(filepath.Join(repo, filepath.FromSlash(path)))
	return err == nil && info.IsDir()
}

func sourceTreeAncestorReplacementPresent(repo, path string, after map[string]workspaceTreeEntry) bool {
	for ancestor := filepath.ToSlash(filepath.Dir(path)); ancestor != "."; ancestor = filepath.ToSlash(filepath.Dir(ancestor)) {
		if after[ancestor].path == "" {
			continue
		}
		info, err := os.Lstat(filepath.Join(repo, filepath.FromSlash(ancestor)))
		return err == nil && !info.IsDir()
	}
	return false
}

func sourceTreeDirectoryChildren(directories map[string]bool) map[string][]string {
	children := make(map[string][]string)
	for directory := range directories {
		parent := filepath.ToSlash(filepath.Dir(directory))
		if directories[parent] {
			children[parent] = append(children[parent], directory)
		}
	}
	return children
}

func removeSourceTreeReplacedDirectory(repo, path string, directoryChildren map[string][]string) error {
	abs, err := suspensionWorktreePath(repo, path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(abs)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	ordered := []string{path}
	for index := 0; index < len(ordered); index++ {
		ordered = append(ordered, directoryChildren[ordered[index]]...)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return len(ordered[i]) > len(ordered[j])
	})
	for _, directory := range ordered {
		if err := removeSourceTreeEmptyDirectory(repo, directory); err != nil {
			return err
		}
	}
	return nil
}

func removeSourceTreeEmptyDirectory(repo, path string) error {
	abs, err := suspensionWorktreePath(repo, path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(abs)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	return os.Remove(abs)
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
	if after.mode != "100644" && after.mode != "100755" && after.mode != "120000" {
		return fmt.Errorf("parent source update requires a file or symlink: %q", relative)
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
	if entry.mode == "120000" {
		return replaceParentSourceSymlink(path, string(data))
	}
	mode := os.FileMode(0o644)
	if entry.mode == "100755" {
		mode = 0o755
	}
	return execfile.Write(path, data, mode)
}

func replaceParentSourceSymlink(path, target string) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".source-link-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer func() { _ = os.Remove(name) }()
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Remove(name); err != nil {
		return err
	}
	if err := os.Symlink(target, name); err != nil {
		return err
	}
	return os.Rename(name, path)
}
