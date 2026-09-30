package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type RepositoryIdentity struct {
	LineageID   string `json:"lineage_id"`
	CommonDir   string `json:"common_dir"`
	PrimaryRoot string `json:"primary_root"`
}

type WorkspaceIdentity struct {
	ID           string `json:"id"`
	RepositoryID string `json:"repository_id"`
	Root         string `json:"root"`
	GitDir       string `json:"git_dir"`
}

type WorkspaceSnapshot struct {
	ID             string `json:"id"`
	Head           string `json:"head"`
	IndexDigest    string `json:"index_digest"`
	WorktreeDigest string `json:"worktree_digest"`
}

func ResolveRepositoryIdentity(repoRoot string) (RepositoryIdentity, error) {
	root, err := canonicalPath(repoRoot)
	if err != nil {
		return RepositoryIdentity{}, fmt.Errorf("resolve repository root: %w", err)
	}
	commonRaw, err := gitTrimmed(root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return RepositoryIdentity{}, fmt.Errorf("resolve git common dir: %w", err)
	}
	commonDir, err := canonicalPath(commonRaw)
	if err != nil {
		return RepositoryIdentity{}, fmt.Errorf("canonicalize git common dir: %w", err)
	}
	primaryRoot, err := primaryWorktree(commonDir)
	if err != nil {
		return RepositoryIdentity{}, err
	}
	return RepositoryIdentity{
		LineageID:   digestStrings("git-common-lineage-v1", commonDir),
		CommonDir:   commonDir,
		PrimaryRoot: primaryRoot,
	}, nil
}

func ResolveWorkspaceIdentity(repoRoot string, repository RepositoryIdentity) (WorkspaceIdentity, error) {
	root, err := canonicalPath(repoRoot)
	if err != nil {
		return WorkspaceIdentity{}, fmt.Errorf("resolve workspace root: %w", err)
	}
	commonRaw, err := gitTrimmed(root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return WorkspaceIdentity{}, fmt.Errorf("resolve workspace common dir: %w", err)
	}
	commonDir, err := canonicalPath(commonRaw)
	if err != nil {
		return WorkspaceIdentity{}, fmt.Errorf("canonicalize workspace common dir: %w", err)
	}
	if digestStrings("git-common-lineage-v1", commonDir) != repository.LineageID {
		return WorkspaceIdentity{}, fmt.Errorf("workspace belongs to a different repository lineage")
	}
	gitDirRaw, err := gitTrimmed(root, "rev-parse", "--path-format=absolute", "--git-dir")
	if err != nil {
		return WorkspaceIdentity{}, fmt.Errorf("resolve workspace git dir: %w", err)
	}
	gitDir, err := canonicalPath(gitDirRaw)
	if err != nil {
		return WorkspaceIdentity{}, fmt.Errorf("canonicalize workspace git dir: %w", err)
	}
	return WorkspaceIdentity{
		ID:           digestStrings("workspace-v1", repository.LineageID, root, gitDir),
		RepositoryID: repository.LineageID,
		Root:         root,
		GitDir:       gitDir,
	}, nil
}

func CaptureWorkspaceSnapshot(repoRoot string) (WorkspaceSnapshot, error) {
	snapshot, err := state.CaptureGitSnapshot(repoRoot)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	result := WorkspaceSnapshot{
		Head:           snapshot.Head,
		IndexDigest:    snapshot.IndexDigest,
		WorktreeDigest: snapshot.WorktreeDigest,
	}
	result.ID = digestStrings("workspace-snapshot-v1", result.Head, result.IndexDigest, result.WorktreeDigest)
	return result, nil
}

func ResolveSemanticTaskRef(repoRoot, pinnedPath string) (SemanticTaskRef, error) {
	path := filepath.ToSlash(strings.TrimSpace(pinnedPath))
	if path == "" {
		plan, err := os.ReadFile(filepath.Join(repoRoot, "IMPLEMENTATION_PLAN.local.md"))
		if err != nil {
			if os.IsNotExist(err) {
				return SemanticTaskRef{}, nil
			}
			return SemanticTaskRef{}, fmt.Errorf("read implementation plan: %w", err)
		}
		path, err = taskcontract.ParsePlanSchedule(string(plan)).ActiveTask()
		if err != nil {
			return SemanticTaskRef{}, err
		}
	}
	if path == "" {
		return SemanticTaskRef{}, nil
	}
	if err := taskcontract.ValidateActiveTaskPath(path); err != nil {
		return SemanticTaskRef{}, err
	}
	content, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(path)))
	if err != nil {
		return SemanticTaskRef{}, fmt.Errorf("read semantic task %s: %w", path, err)
	}
	return SemanticTaskRef{TaskPath: path, ContractDigest: digestBytes(content)}, nil
}

func primaryWorktree(commonDir string) (string, error) {
	command := exec.Command("git", "--git-dir", commonDir, "worktree", "list", "--porcelain")
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("list git worktrees: %w", err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		if !strings.HasPrefix(line, "worktree ") {
			continue
		}
		root, err := canonicalPath(strings.TrimPrefix(line, "worktree "))
		if err != nil {
			return "", fmt.Errorf("resolve primary worktree: %w", err)
		}
		return root, nil
	}
	return "", fmt.Errorf("git common lineage has no primary worktree")
}

func gitTrimmed(repoRoot string, args ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func canonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(absolute)
}

func digestStrings(values ...string) string {
	hash := sha256.New()
	for _, value := range values {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
