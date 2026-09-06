package taskdiff

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type FileIdentity struct {
	Path        string `json:"path"`
	HeadBlob    string `json:"head_blob"`
	IndexBlob   string `json:"index_blob"`
	WorktreeSHA string `json:"worktree_sha256"`
}

func FileIdentities(repoRoot string, paths []string) ([]FileIdentity, error) {
	identities := make([]FileIdentity, 0, len(paths))
	for _, path := range paths {
		identity := FileIdentity{Path: path}
		headBlob, err := trimmedGitOutput(repoRoot, "rev-parse", "--verify", "HEAD:"+path)
		if err != nil {
			headBlob = ""
		}
		identity.HeadBlob = headBlob
		identity.IndexBlob = indexBlobIdentity(repoRoot, path)
		worktreeSHA, err := worktreeContentSHA(repoRoot, path)
		if err != nil {
			return nil, err
		}
		identity.WorktreeSHA = worktreeSHA
		identities = append(identities, identity)
	}
	return identities, nil
}

func SameFileIdentity(before, after FileIdentity) bool {
	return before.HeadBlob == after.HeadBlob &&
		before.IndexBlob == after.IndexBlob &&
		before.WorktreeSHA == after.WorktreeSHA
}

func indexBlobIdentity(repoRoot string, path string) string {
	command := exec.Command("git", "-C", repoRoot, "ls-files", "-s", "--", path)
	output, err := command.Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(output))
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}

func worktreeContentSHA(repoRoot string, rel string) (string, error) {
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
	if !info.Mode().IsRegular() {
		return "", nil
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
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
	if err != nil {
		return "", err
	}
	if canonical != root && !strings.HasPrefix(canonical, root+string(filepath.Separator)) {
		return "", fmt.Errorf("path %qがrepository境界を越えています", rel)
	}
	return canonical, nil
}

func trimmedGitOutput(dir string, args ...string) (string, error) {
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
