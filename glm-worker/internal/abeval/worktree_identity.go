package abeval

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/gitexec"
)

type WorktreeIdentity struct {
	ResolvedPath string `json:"resolved_path"`
	Device       uint64 `json:"device"`
	Inode        uint64 `json:"inode"`
}

func CaptureWorktreeIdentity(path string) (WorktreeIdentity, error) {
	if !filepath.IsAbs(path) {
		return WorktreeIdentity{}, fmt.Errorf("worktree_pathは絶対pathである必要があります: %s", path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return WorktreeIdentity{}, fmt.Errorf("worktree identityのpath解決: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return WorktreeIdentity{}, fmt.Errorf("worktree identityのstat: %w", err)
	}
	if !info.IsDir() {
		return WorktreeIdentity{}, fmt.Errorf("worktree identityはdirectoryである必要があります: %s", path)
	}
	if err := verifyGitWorktreeRoot(resolved); err != nil {
		return WorktreeIdentity{}, err
	}
	device, inode, err := directoryFileIdentity(resolved, info)
	if err != nil {
		return WorktreeIdentity{}, err
	}
	if device == 0 || inode == 0 {
		return WorktreeIdentity{}, fmt.Errorf("worktree identityのdevice/inodeが零です: %s", path)
	}
	return WorktreeIdentity{
		ResolvedPath: filepath.Clean(resolved),
		Device:       device,
		Inode:        inode,
	}, nil
}

func verifyGitWorktreeRoot(path string) error {
	env, err := gitexec.WithoutLocalEnvironment(os.Environ())
	if err != nil {
		return fmt.Errorf("worktree root採取用のGit環境: %w", err)
	}
	command := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel")
	command.Env = env
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("worktree rootをGitで確認できません: %w", err)
	}
	root, err := filepath.EvalSymlinks(strings.TrimSuffix(strings.TrimSuffix(string(output), "\n"), "\r"))
	if err != nil {
		return fmt.Errorf("worktree rootのpath解決: %w", err)
	}
	if root != path {
		return fmt.Errorf("worktree_pathにGit worktreeのrootを指定してください: %s", path)
	}
	return nil
}

func validateWorktreeIdentity(record RunRecord) error {
	id := record.WorktreeIdentity
	if !filepath.IsAbs(id.ResolvedPath) || filepath.Clean(id.ResolvedPath) != id.ResolvedPath || id.Device == 0 || id.Inode == 0 {
		return fmt.Errorf("worktree_identityに採取時のresolved_path/device/inodeが必要です")
	}
	if record.WorktreePath != id.ResolvedPath {
		return fmt.Errorf("worktree_pathは採取したworktree_identityのresolved_pathと一致する必要があります")
	}
	live, err := CaptureWorktreeIdentity(record.WorktreePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("worktree_identityを検証できません: %w", err)
	}
	if live != id {
		return fmt.Errorf("worktree_identityが現在のworktreeと一致しません: recorded=%+v current=%+v", id, live)
	}
	return nil
}

func independentWorktreeIdentity(a, b WorktreeIdentity) bool {
	return (a.Device != b.Device || a.Inode != b.Inode) && a.ResolvedPath != b.ResolvedPath
}
