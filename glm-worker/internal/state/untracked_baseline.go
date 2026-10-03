package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type UntrackedBaselineEntry struct {
	Path    string
	Kind    string
	Mode    uint32
	Size    int64
	SHA256  string
	Content []byte
}

type untrackedBaselineSnapshot struct {
	Version int                              `json:"version"`
	Entries []untrackedBaselineSnapshotEntry `json:"entries"`
}

type untrackedBaselineSnapshotEntry struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Mode    uint32 `json:"mode"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	Content []byte `json:"content"`
}

const (
	UntrackedBaselineKindFile    = "file"
	UntrackedBaselineKindSymlink = "symlink"

	untrackedBaselineVersion          = 1
	untrackedBaselineMaxEntryBytes    = 512 * 1024
	untrackedBaselineMaxTotalBytes    = 4 * 1024 * 1024
	untrackedBaselineMaxEntries       = 4096
	untrackedBaselineMaxSnapshotBytes = 8 * 1024 * 1024
)

func captureUntrackedBaselineSnapshot(repoRoot string, state *StateStore, rawPaths []byte) error {
	paths, err := prepareUntrackedBaselinePaths(rawPaths)
	if err != nil {
		return err
	}
	snapshot, err := captureUntrackedBaselineEntries(repoRoot, paths)
	if err != nil {
		return err
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode untracked baseline snapshot: %w", err)
	}
	data = append(data, '\n')
	if len(data) > untrackedBaselineMaxSnapshotBytes {
		return fmt.Errorf("untracked baseline snapshot exceeds encoded limit of %d bytes", untrackedBaselineMaxSnapshotBytes)
	}
	if err := writeFileAtomic(state.Path(baselineUntrackedFile), data, 0o600); err != nil {
		return fmt.Errorf("write untracked baseline snapshot: %w", err)
	}
	return nil
}

func prepareUntrackedBaselinePaths(raw []byte) ([]string, error) {
	paths := splitUntrackedBaselinePaths(raw)
	if len(paths) > untrackedBaselineMaxEntries {
		return nil, fmt.Errorf("untracked baseline has %d paths; limit is %d", len(paths), untrackedBaselineMaxEntries)
	}
	sort.Strings(paths)
	return paths, nil
}

func captureUntrackedBaselineEntries(repoRoot string, paths []string) (untrackedBaselineSnapshot, error) {
	snapshot := untrackedBaselineSnapshot{Version: untrackedBaselineVersion}
	totalBytes := int64(0)
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if _, exists := seen[path]; exists {
			return untrackedBaselineSnapshot{}, fmt.Errorf("duplicate untracked baseline path %q", path)
		}
		seen[path] = struct{}{}
		entry, err := captureUntrackedBaselineEntry(repoRoot, path)
		if err != nil {
			return untrackedBaselineSnapshot{}, err
		}
		totalBytes += entry.Size
		if totalBytes > untrackedBaselineMaxTotalBytes {
			return untrackedBaselineSnapshot{}, fmt.Errorf("untracked baseline content exceeds total limit of %d bytes", untrackedBaselineMaxTotalBytes)
		}
		snapshot.Entries = append(snapshot.Entries, entry)
	}
	return snapshot, nil
}

func captureUntrackedBaselineEntry(repoRoot, path string) (untrackedBaselineSnapshotEntry, error) {
	fullPath, info, err := safeUntrackedBaselinePath(repoRoot, path)
	if err != nil {
		return untrackedBaselineSnapshotEntry{}, err
	}

	kind := ""
	mode := uint32(0)
	var content []byte
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		kind = UntrackedBaselineKindSymlink
		target, readErr := os.Readlink(fullPath)
		if readErr != nil {
			return untrackedBaselineSnapshotEntry{}, fmt.Errorf("read untracked baseline symlink %q: %w", path, readErr)
		}
		content = []byte(target)
	case info.Mode().IsRegular():
		kind = UntrackedBaselineKindFile
		mode = uint32(info.Mode().Perm())
		content, err = readStableUntrackedBaselineFile(fullPath, info)
		if err != nil {
			return untrackedBaselineSnapshotEntry{}, fmt.Errorf("read untracked baseline file %q: %w", path, err)
		}
	default:
		return untrackedBaselineSnapshotEntry{}, fmt.Errorf("unsupported untracked baseline file type for %q", path)
	}
	if len(content) > untrackedBaselineMaxEntryBytes {
		return untrackedBaselineSnapshotEntry{}, fmt.Errorf("untracked baseline path %q exceeds per-entry limit of %d bytes", path, untrackedBaselineMaxEntryBytes)
	}

	digest := sha256.Sum256(content)
	return untrackedBaselineSnapshotEntry{
		Path:    path,
		Kind:    kind,
		Mode:    mode,
		Size:    int64(len(content)),
		SHA256:  hex.EncodeToString(digest[:]),
		Content: content,
	}, nil
}

func readStableUntrackedBaselineFile(path string, initial os.FileInfo) ([]byte, error) {
	if err := validateUntrackedBaselineInitialFile(initial); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if err := validateUntrackedBaselineOpenedFile(initial, opened); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(untrackedBaselineMaxEntryBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > untrackedBaselineMaxEntryBytes {
		return nil, fmt.Errorf("file exceeds per-entry limit of %d bytes", untrackedBaselineMaxEntryBytes)
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if err := validateUntrackedBaselineStableRead(opened, after, int64(len(data))); err != nil {
		return nil, err
	}
	return data, nil
}

func validateUntrackedBaselineInitialFile(info os.FileInfo) error {
	if info.Size() > untrackedBaselineMaxEntryBytes {
		return fmt.Errorf("file exceeds per-entry limit of %d bytes", untrackedBaselineMaxEntryBytes)
	}
	return nil
}

func validateUntrackedBaselineOpenedFile(initial, opened os.FileInfo) error {
	if !opened.Mode().IsRegular() || !os.SameFile(initial, opened) {
		return fmt.Errorf("file changed type or identity while opening")
	}
	return nil
}

func validateUntrackedBaselineStableRead(opened, after os.FileInfo, readSize int64) error {
	if !os.SameFile(opened, after) || opened.Size() != after.Size() || opened.Mode() != after.Mode() || readSize != after.Size() {
		return fmt.Errorf("file changed while capturing baseline")
	}
	return nil
}

func (s *StateStore) ReadUntrackedBaseline() ([]UntrackedBaselineEntry, error) {
	path := s.Path(baselineUntrackedFile)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("read untracked baseline snapshot: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("untracked baseline snapshot is not a regular file")
	}
	if info.Size() > untrackedBaselineMaxSnapshotBytes {
		return nil, fmt.Errorf("untracked baseline snapshot exceeds encoded limit of %d bytes", untrackedBaselineMaxSnapshotBytes)
	}
	data, err := readBoundedUntrackedBaselineSnapshot(path)
	if err != nil {
		return nil, err
	}
	snapshot, err := decodeUntrackedBaselineSnapshot(data)
	if err != nil {
		return nil, err
	}
	return validateUntrackedBaselineSnapshot(snapshot)
}

func readBoundedUntrackedBaselineSnapshot(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open untracked baseline snapshot: %w", err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, int64(untrackedBaselineMaxSnapshotBytes)+1))
	if err != nil {
		return nil, fmt.Errorf("read untracked baseline snapshot: %w", err)
	}
	if len(data) > untrackedBaselineMaxSnapshotBytes {
		return nil, fmt.Errorf("untracked baseline snapshot exceeds encoded limit of %d bytes", untrackedBaselineMaxSnapshotBytes)
	}
	return data, nil
}

func decodeUntrackedBaselineSnapshot(data []byte) (untrackedBaselineSnapshot, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var snapshot untrackedBaselineSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return untrackedBaselineSnapshot{}, fmt.Errorf("decode untracked baseline snapshot: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return untrackedBaselineSnapshot{}, fmt.Errorf("untracked baseline snapshot has trailing data")
	}
	if snapshot.Version != untrackedBaselineVersion {
		return untrackedBaselineSnapshot{}, fmt.Errorf("unsupported untracked baseline version %d", snapshot.Version)
	}
	if len(snapshot.Entries) > untrackedBaselineMaxEntries {
		return untrackedBaselineSnapshot{}, fmt.Errorf("untracked baseline has %d paths; limit is %d", len(snapshot.Entries), untrackedBaselineMaxEntries)
	}
	return snapshot, nil
}

func validateUntrackedBaselineSnapshot(snapshot untrackedBaselineSnapshot) ([]UntrackedBaselineEntry, error) {
	entries := make([]UntrackedBaselineEntry, 0, len(snapshot.Entries))
	seen := make(map[string]struct{}, len(snapshot.Entries))
	totalBytes := int64(0)
	for _, item := range snapshot.Entries {
		if err := validateUntrackedBaselineSnapshotEntry(item, seen); err != nil {
			return nil, err
		}
		seen[item.Path] = struct{}{}
		totalBytes += item.Size
		if totalBytes > untrackedBaselineMaxTotalBytes {
			return nil, fmt.Errorf("untracked baseline content exceeds total limit of %d bytes", untrackedBaselineMaxTotalBytes)
		}
		entries = append(entries, UntrackedBaselineEntry{
			Path:    item.Path,
			Kind:    item.Kind,
			Mode:    item.Mode,
			Size:    item.Size,
			SHA256:  item.SHA256,
			Content: append([]byte(nil), item.Content...),
		})
	}
	return entries, nil
}

func validateUntrackedBaselineSnapshotEntry(entry untrackedBaselineSnapshotEntry, seen map[string]struct{}) error {
	if err := validateUntrackedBaselineEntryIdentity(entry, seen); err != nil {
		return err
	}
	if err := validateUntrackedBaselineEntryMetadata(entry); err != nil {
		return err
	}
	return validateUntrackedBaselineEntryContent(entry)
}

func validateUntrackedBaselineEntryIdentity(entry untrackedBaselineSnapshotEntry, seen map[string]struct{}) error {
	if !validUntrackedBaselineRelativePath(entry.Path) {
		return fmt.Errorf("invalid untracked baseline path %q", entry.Path)
	}
	if _, exists := seen[entry.Path]; exists {
		return fmt.Errorf("duplicate untracked baseline path %q", entry.Path)
	}
	if entry.Kind != UntrackedBaselineKindFile && entry.Kind != UntrackedBaselineKindSymlink {
		return fmt.Errorf("invalid untracked baseline kind for %q", entry.Path)
	}
	return nil
}

func validateUntrackedBaselineEntryMetadata(entry untrackedBaselineSnapshotEntry) error {
	if entry.Kind == UntrackedBaselineKindSymlink && entry.Mode != 0 {
		return fmt.Errorf("symlink untracked baseline has unexpected mode for %q", entry.Path)
	}
	if entry.Kind == UntrackedBaselineKindFile && entry.Mode&^uint32(0o777) != 0 {
		return fmt.Errorf("regular untracked baseline has invalid mode for %q", entry.Path)
	}
	if entry.Size < 0 || entry.Size > untrackedBaselineMaxEntryBytes || entry.Size != int64(len(entry.Content)) {
		return fmt.Errorf("invalid untracked baseline size for %q", entry.Path)
	}
	return nil
}

func validateUntrackedBaselineEntryContent(entry untrackedBaselineSnapshotEntry) error {
	if !validUntrackedBaselineDigest(entry.SHA256) {
		return fmt.Errorf("invalid untracked baseline digest for %q", entry.Path)
	}
	digest := sha256.Sum256(entry.Content)
	if hex.EncodeToString(digest[:]) != entry.SHA256 {
		return fmt.Errorf("untracked baseline content digest mismatch for %q", entry.Path)
	}
	return nil
}

func safeUntrackedBaselinePath(repoRoot, path string) (string, os.FileInfo, error) {
	if !validUntrackedBaselineRelativePath(path) {
		return "", nil, fmt.Errorf("invalid untracked baseline path %q", path)
	}
	parts := strings.Split(filepath.FromSlash(path), string(filepath.Separator))
	current := repoRoot
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return "", nil, fmt.Errorf("stat untracked baseline path %q: %w", path, err)
		}
		if index == len(parts)-1 {
			return current, info, nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", nil, fmt.Errorf("untracked baseline path %q traverses symlink parent", path)
		}
		if !info.IsDir() {
			return "", nil, fmt.Errorf("untracked baseline path %q traverses non-directory parent", path)
		}
	}
	return "", nil, fmt.Errorf("invalid untracked baseline path %q", path)
}

func validUntrackedBaselineRelativePath(path string) bool {
	if path == "" || strings.ContainsRune(path, 0) {
		return false
	}
	local := filepath.FromSlash(path)
	if filepath.IsAbs(local) {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(local))
	return clean == path && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func validUntrackedBaselineDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func splitUntrackedBaselinePaths(raw []byte) []string {
	parts := strings.Split(string(raw), "\x00")
	paths := make([]string, 0, len(parts))
	for _, path := range parts {
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

func removeUntrackedBaselineSnapshot(state *StateStore) error {
	err := removeStatePath(state.Path(baselineUntrackedFile))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove untracked baseline snapshot: %w", err)
	}
	return nil
}
