package state

import (
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

type untrackedBaselineManifest struct {
	Version int                              `json:"version"`
	Entries []untrackedBaselineManifestEntry `json:"entries"`
}

type untrackedBaselineManifestEntry struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Blob   string `json:"blob"`
}

const (
	UntrackedBaselineKindFile    = "file"
	UntrackedBaselineKindSymlink = "symlink"

	untrackedBaselineVersion          = 1
	untrackedBaselineManifestFile     = "manifest.json"
	untrackedBaselineBlobDir          = "blobs"
	untrackedBaselineMaxEntryBytes    = 512 * 1024
	untrackedBaselineMaxTotalBytes    = 4 * 1024 * 1024
	untrackedBaselineMaxEntries       = 4096
	untrackedBaselineMaxManifestBytes = 4 * 1024 * 1024
)

func captureUntrackedBaselineSnapshot(repoRoot string, state *StateStore, rawPaths []byte) error {
	paths := splitUntrackedBaselinePaths(rawPaths)
	if len(paths) > untrackedBaselineMaxEntries {
		return fmt.Errorf("untracked baseline has %d paths; limit is %d", len(paths), untrackedBaselineMaxEntries)
	}
	sort.Strings(paths)

	parent := filepath.Dir(state.Path(baselineUntrackedFile))
	tempDir, err := os.MkdirTemp(parent, ".baseline-untracked-")
	if err != nil {
		return fmt.Errorf("create untracked baseline staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()
	if err := os.Chmod(tempDir, 0o700); err != nil {
		return fmt.Errorf("protect untracked baseline staging directory: %w", err)
	}
	blobDir := filepath.Join(tempDir, untrackedBaselineBlobDir)
	if err := os.Mkdir(blobDir, 0o700); err != nil {
		return fmt.Errorf("create untracked baseline blob directory: %w", err)
	}

	manifest := untrackedBaselineManifest{Version: untrackedBaselineVersion}
	totalBytes := int64(0)
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if _, exists := seen[path]; exists {
			return fmt.Errorf("duplicate untracked baseline path %q", path)
		}
		seen[path] = struct{}{}
		entry, content, err := captureUntrackedBaselineEntry(repoRoot, path)
		if err != nil {
			return err
		}
		totalBytes += int64(len(content))
		if totalBytes > untrackedBaselineMaxTotalBytes {
			return fmt.Errorf("untracked baseline content exceeds total limit of %d bytes", untrackedBaselineMaxTotalBytes)
		}
		if err := writeUntrackedBaselineBlob(blobDir, entry.Blob, content); err != nil {
			return err
		}
		manifest.Entries = append(manifest.Entries, entry)
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode untracked baseline manifest: %w", err)
	}
	data = append(data, '\n')
	if len(data) > untrackedBaselineMaxManifestBytes {
		return fmt.Errorf("untracked baseline manifest exceeds limit of %d bytes", untrackedBaselineMaxManifestBytes)
	}
	if err := os.WriteFile(filepath.Join(tempDir, untrackedBaselineManifestFile), data, 0o600); err != nil {
		return fmt.Errorf("write untracked baseline manifest: %w", err)
	}

	finalDir := state.Path(baselineUntrackedFile)
	if err := os.RemoveAll(finalDir); err != nil {
		return fmt.Errorf("remove previous untracked baseline: %w", err)
	}
	if err := os.Rename(tempDir, finalDir); err != nil {
		return fmt.Errorf("publish untracked baseline snapshot: %w", err)
	}
	return nil
}

func captureUntrackedBaselineEntry(
	repoRoot string,
	path string,
) (untrackedBaselineManifestEntry, []byte, error) {
	fullPath, err := safeUntrackedBaselinePath(repoRoot, path)
	if err != nil {
		return untrackedBaselineManifestEntry{}, nil, err
	}
	info, err := os.Lstat(fullPath)
	if err != nil {
		return untrackedBaselineManifestEntry{}, nil, fmt.Errorf("stat untracked baseline path %q: %w", path, err)
	}

	kind := ""
	mode := uint32(0)
	var content []byte
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		kind = UntrackedBaselineKindSymlink
		target, readErr := os.Readlink(fullPath)
		if readErr != nil {
			return untrackedBaselineManifestEntry{}, nil, fmt.Errorf("read untracked baseline symlink %q: %w", path, readErr)
		}
		content = []byte(target)
	case info.Mode().IsRegular():
		kind = UntrackedBaselineKindFile
		mode = uint32(info.Mode().Perm())
		content, err = readStableUntrackedBaselineFile(fullPath, info)
		if err != nil {
			return untrackedBaselineManifestEntry{}, nil, fmt.Errorf("read untracked baseline file %q: %w", path, err)
		}
	default:
		return untrackedBaselineManifestEntry{}, nil, fmt.Errorf("unsupported untracked baseline file type for %q", path)
	}
	if len(content) > untrackedBaselineMaxEntryBytes {
		return untrackedBaselineManifestEntry{}, nil, fmt.Errorf("untracked baseline path %q exceeds per-entry limit of %d bytes", path, untrackedBaselineMaxEntryBytes)
	}

	digest := sha256.Sum256(content)
	hexDigest := hex.EncodeToString(digest[:])
	return untrackedBaselineManifestEntry{
		Path:   path,
		Kind:   kind,
		Mode:   mode,
		Size:   int64(len(content)),
		SHA256: hexDigest,
		Blob:   hexDigest,
	}, content, nil
}

func readStableUntrackedBaselineFile(path string, initial os.FileInfo) ([]byte, error) {
	if initial.Size() > untrackedBaselineMaxEntryBytes {
		return nil, fmt.Errorf("file exceeds per-entry limit of %d bytes", untrackedBaselineMaxEntryBytes)
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
	if !opened.Mode().IsRegular() || !os.SameFile(initial, opened) {
		return nil, fmt.Errorf("file changed type or identity while opening")
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
	if !os.SameFile(opened, after) || opened.Size() != after.Size() || opened.Mode() != after.Mode() || int64(len(data)) != after.Size() {
		return nil, fmt.Errorf("file changed while capturing baseline")
	}
	return data, nil
}

func writeUntrackedBaselineBlob(blobDir, name string, content []byte) error {
	path := filepath.Join(blobDir, name)
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect untracked baseline blob: %w", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("write untracked baseline blob: %w", err)
	}
	return nil
}

func (s *StateStore) ReadUntrackedBaseline() ([]UntrackedBaselineEntry, error) {
	root := s.Path(baselineUntrackedFile)
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("read untracked baseline snapshot: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("untracked baseline snapshot is not a directory")
	}

	manifest, err := readUntrackedBaselineManifest(root)
	if err != nil {
		return nil, err
	}
	entries := make([]UntrackedBaselineEntry, 0, len(manifest.Entries))
	totalBytes := int64(0)
	seen := make(map[string]struct{}, len(manifest.Entries))
	for _, item := range manifest.Entries {
		if err := validateUntrackedBaselineManifestEntry(item, seen); err != nil {
			return nil, err
		}
		seen[item.Path] = struct{}{}
		content, err := readUntrackedBaselineBlob(root, item)
		if err != nil {
			return nil, err
		}
		totalBytes += int64(len(content))
		if totalBytes > untrackedBaselineMaxTotalBytes {
			return nil, fmt.Errorf("untracked baseline content exceeds total limit of %d bytes", untrackedBaselineMaxTotalBytes)
		}
		entries = append(entries, UntrackedBaselineEntry{
			Path:    item.Path,
			Kind:    item.Kind,
			Mode:    item.Mode,
			Size:    item.Size,
			SHA256:  item.SHA256,
			Content: content,
		})
	}
	return entries, nil
}

func readUntrackedBaselineManifest(root string) (untrackedBaselineManifest, error) {
	path := filepath.Join(root, untrackedBaselineManifestFile)
	info, err := os.Lstat(path)
	if err != nil {
		return untrackedBaselineManifest{}, fmt.Errorf("read untracked baseline manifest: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return untrackedBaselineManifest{}, fmt.Errorf("untracked baseline manifest is not a regular file")
	}
	if info.Size() > untrackedBaselineMaxManifestBytes {
		return untrackedBaselineManifest{}, fmt.Errorf("untracked baseline manifest exceeds limit of %d bytes", untrackedBaselineMaxManifestBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return untrackedBaselineManifest{}, err
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(io.LimitReader(file, int64(untrackedBaselineMaxManifestBytes)+1))
	decoder.DisallowUnknownFields()
	var manifest untrackedBaselineManifest
	if err := decoder.Decode(&manifest); err != nil {
		return untrackedBaselineManifest{}, fmt.Errorf("decode untracked baseline manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return untrackedBaselineManifest{}, fmt.Errorf("untracked baseline manifest has trailing data")
	}
	if manifest.Version != untrackedBaselineVersion {
		return untrackedBaselineManifest{}, fmt.Errorf("unsupported untracked baseline version %d", manifest.Version)
	}
	if len(manifest.Entries) > untrackedBaselineMaxEntries {
		return untrackedBaselineManifest{}, fmt.Errorf("untracked baseline has %d paths; limit is %d", len(manifest.Entries), untrackedBaselineMaxEntries)
	}
	return manifest, nil
}

func validateUntrackedBaselineManifestEntry(
	entry untrackedBaselineManifestEntry,
	seen map[string]struct{},
) error {
	if !validUntrackedBaselineRelativePath(entry.Path) {
		return fmt.Errorf("invalid untracked baseline path %q", entry.Path)
	}
	if _, exists := seen[entry.Path]; exists {
		return fmt.Errorf("duplicate untracked baseline path %q", entry.Path)
	}
	if entry.Kind != UntrackedBaselineKindFile && entry.Kind != UntrackedBaselineKindSymlink {
		return fmt.Errorf("invalid untracked baseline kind for %q", entry.Path)
	}
	if entry.Kind == UntrackedBaselineKindSymlink && entry.Mode != 0 {
		return fmt.Errorf("symlink untracked baseline has unexpected mode for %q", entry.Path)
	}
	if entry.Size < 0 || entry.Size > untrackedBaselineMaxEntryBytes {
		return fmt.Errorf("invalid untracked baseline size for %q", entry.Path)
	}
	if !validUntrackedBaselineDigest(entry.SHA256) || entry.Blob != entry.SHA256 {
		return fmt.Errorf("invalid untracked baseline digest for %q", entry.Path)
	}
	return nil
}

func readUntrackedBaselineBlob(root string, entry untrackedBaselineManifestEntry) ([]byte, error) {
	path := filepath.Join(root, untrackedBaselineBlobDir, entry.Blob)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("read untracked baseline blob for %q: %w", entry.Path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("untracked baseline blob is not a regular file for %q", entry.Path)
	}
	if info.Size() != entry.Size {
		return nil, fmt.Errorf("untracked baseline blob size mismatch for %q", entry.Path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read untracked baseline blob for %q: %w", entry.Path, err)
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != entry.SHA256 {
		return nil, fmt.Errorf("untracked baseline blob digest mismatch for %q", entry.Path)
	}
	return data, nil
}

func safeUntrackedBaselinePath(repoRoot, path string) (string, error) {
	if !validUntrackedBaselineRelativePath(path) {
		return "", fmt.Errorf("invalid untracked baseline path %q", path)
	}
	fullPath := filepath.Join(repoRoot, filepath.FromSlash(path))
	relative, err := filepath.Rel(repoRoot, fullPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("untracked baseline path escapes repository: %q", path)
	}
	return fullPath, nil
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
	if err := os.RemoveAll(state.Path(baselineUntrackedFile)); err != nil {
		return fmt.Errorf("remove untracked baseline snapshot: %w", err)
	}
	return nil
}
