package taskdiff

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type FileIdentity struct {
	Path           string `json:"path"`
	HeadDigest     string `json:"head_digest"`
	IndexDigest    string `json:"index_digest"`
	WorktreeDigest string `json:"worktree_digest"`
}

func FileIdentities(repoRoot string, paths []string) ([]FileIdentity, error) {
	identities := make([]FileIdentity, 0, len(paths))
	for _, path := range paths {
		identity := FileIdentity{Path: path}
		identity.HeadDigest = headEntryDigest(repoRoot, path)
		identity.IndexDigest = indexEntriesDigest(repoRoot, path)
		worktreeDigest, err := worktreeContentDigest(repoRoot, path)
		if err != nil {
			return nil, err
		}
		identity.WorktreeDigest = worktreeDigest
		identities = append(identities, identity)
	}
	return identities, nil
}

func SameFileIdentity(before, after FileIdentity) bool {
	return before.HeadDigest == after.HeadDigest &&
		before.IndexDigest == after.IndexDigest &&
		before.WorktreeDigest == after.WorktreeDigest
}

func headEntryDigest(repoRoot string, path string) string {
	output, err := exec.Command("git", "-C", repoRoot, "ls-tree", "-z", "HEAD", "--", path).Output()
	if err != nil || len(output) == 0 {
		return ""
	}
	return contentDigest(output)
}

func indexEntriesDigest(repoRoot string, path string) string {
	output, err := exec.Command("git", "-C", repoRoot, "ls-files", "-s", "-z", "--", path).Output()
	if err != nil || len(output) == 0 {
		return ""
	}
	return contentDigest(output)
}

func worktreeContentDigest(repoRoot string, rel string) (string, error) {
	abs, err := joinWithinRepo(repoRoot, rel)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	hasher := sha256.New()
	hasher.Write(strconv.AppendUint(make([]byte, 0, 12), uint64(info.Mode()), 10))
	hasher.Write([]byte{0})
	mode := info.Mode()
	if mode&os.ModeSymlink != 0 {
		target, err := os.Readlink(abs)
		if err != nil {
			return "", err
		}
		hasher.Write([]byte(target))
		return hex.EncodeToString(hasher.Sum(nil)), nil
	}
	if mode.IsRegular() {
		content, err := os.ReadFile(abs)
		if err != nil {
			return "", err
		}
		hasher.Write(content)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func contentDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func joinWithinRepo(repoRoot string, rel string) (string, error) {
	root, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return "", fmt.Errorf("repo rootを解決できません: %w", err)
	}
	clean := filepath.ToSlash(filepath.Clean(rel))
	if clean == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("path %qがrepository相対ではありません", rel)
	}
	abs := filepath.Join(root, filepath.FromSlash(clean))
	canonical, err := filepath.EvalSymlinks(abs)
	if err == nil {
		if !withinRepoRoot(root, canonical) {
			return "", fmt.Errorf("path %qがrepository境界を越えています", rel)
		}
		return abs, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return resolveDeletedPathWithinRepo(root, abs, rel)
}

func resolveDeletedPathWithinRepo(root string, abs string, rel string) (string, error) {
	current := abs
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return abs, nil
		}
		canonicalParent, err := filepath.EvalSymlinks(parent)
		if err == nil {
			if !withinRepoRoot(root, canonicalParent) {
				return "", fmt.Errorf("path %qがrepository境界を越えています", rel)
			}
			return abs, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		current = parent
	}
}

func withinRepoRoot(root string, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
