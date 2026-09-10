package parentactioncmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/guardrepair"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type guardRepairWorktreeSnapshot struct {
	dirty  []state.StopDirtyFile
	parent state.ParentFileStates
	git    state.GitSnapshot
}

func createGuardRepairWorktree(cfg config.AppConfig) (string, func(), error) {
	id, err := state.NewUUID()
	if err != nil {
		return "", func() {}, err
	}
	worktree := filepath.Join(cfg.WorktreeBase, cfg.RepoShort, "guard-repair-"+id)
	if err := os.MkdirAll(filepath.Dir(worktree), 0o700); err != nil {
		return "", func() {}, err
	}
	command := exec.Command("git", "-C", cfg.RepoRoot, "worktree", "add", "--quiet", "--detach", worktree, "HEAD")
	if output, err := command.CombinedOutput(); err != nil {
		return "", func() {}, fmt.Errorf("create guard repair worktree: %w: %s", err, strings.TrimSpace(string(output)))
	}
	cleanup := func() {
		_ = exec.Command("git", "-C", cfg.RepoRoot, "worktree", "remove", "--force", worktree).Run()
	}
	return worktree, cleanup, nil
}

func overlayGuardRepairWorktree(repoRoot, worktree string) error {
	args := []string{"-C", repoRoot, "diff", "--binary", "HEAD", "--"}
	args = append(args, state.ParentExcludePathspecs()...)
	patch, err := exec.Command("git", args...).Output()
	if err != nil {
		return fmt.Errorf("capture current worktree overlay: %w", err)
	}
	if err := applyGuardRepairOverlay(worktree, patch); err != nil {
		return err
	}
	return copyUntrackedOverlay(repoRoot, worktree)
}

func applyGuardRepairOverlay(worktree string, patch []byte) error {
	if len(patch) == 0 {
		return nil
	}
	command := exec.Command("git", "-C", worktree, "apply", "--binary", "--whitespace=nowarn", "-")
	command.Stdin = bytes.NewReader(patch)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("apply current worktree overlay: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func copyUntrackedOverlay(repoRoot, worktree string) error {
	args := []string{"-C", repoRoot, "ls-files", "-z", "--others", "--exclude-standard", "--"}
	args = append(args, state.ParentExcludePathspecs()...)
	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return fmt.Errorf("enumerate current untracked overlay: %w", err)
	}
	paths := strings.Split(strings.TrimRight(string(output), "\x00"), "\x00")
	for _, path := range paths {
		if path != "" {
			if err := copyOverlayPath(repoRoot, worktree, path); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyOverlayPath(repoRoot, worktree, path string) error {
	src, err := joinRepairRoot(repoRoot, path)
	if err != nil {
		return err
	}
	dst, err := joinRepairRoot(worktree, path)
	if err != nil {
		return err
	}
	if err := copyRepairEntry(src, dst, true); err != nil {
		return fmt.Errorf("copy untracked overlay %s: %w", path, err)
	}
	return nil
}

func captureGuardRepairWorktree(worktree string) (guardRepairWorktreeSnapshot, error) {
	dirty, err := state.CaptureStopDirtyFiles(worktree)
	if err != nil {
		return guardRepairWorktreeSnapshot{}, err
	}
	parent, err := state.CaptureParentFileStates(worktree)
	if err != nil {
		return guardRepairWorktreeSnapshot{}, err
	}
	git, err := state.CaptureGitSnapshot(worktree)
	if err != nil {
		return guardRepairWorktreeSnapshot{}, err
	}
	return guardRepairWorktreeSnapshot{dirty: dirty, parent: parent, git: git}, nil
}

func validateGuardRepairChanges(worktree string, changed []string, before, after guardRepairWorktreeSnapshot) error {
	if before.git.Head != after.git.Head || before.git.IndexDigest != after.git.IndexDigest {
		return fmt.Errorf("guard repair modified Git HEAD or index")
	}
	if !state.SameParentFileStates(before.parent, after.parent) {
		return fmt.Errorf("guard repair modified parent-managed implementation metadata")
	}
	if len(changed) == 0 {
		return fmt.Errorf("guard repair produced no source changes")
	}
	return validateGuardRepairPaths(worktree, changed)
}

func validateGuardRepairPaths(worktree string, changed []string) error {
	hasSource := false
	hasTest := false
	for _, path := range changed {
		isTest, err := validateGuardRepairPath(worktree, path)
		if err != nil {
			return err
		}
		if isTest {
			hasTest = true
		} else {
			hasSource = true
		}
	}
	if !hasSource || !hasTest {
		return fmt.Errorf("guard repair requires both production source and corresponding test changes")
	}
	return nil
}

func validateGuardRepairPath(worktree, path string) (bool, error) {
	if !guardrepair.IsAllowed(path) {
		return false, fmt.Errorf("guard repair changed out-of-scope path %s", path)
	}
	full, err := joinRepairRoot(worktree, path)
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(full)
	if err != nil || !info.Mode().IsRegular() {
		return false, fmt.Errorf("guard repair path must remain a regular file: %s", path)
	}
	if output, err := exec.Command("git", "-C", worktree, "ls-files", "--error-unmatch", "--", path).CombinedOutput(); err != nil {
		return false, fmt.Errorf("guard repair cannot create untracked repair files: %s: %s", path, strings.TrimSpace(string(output)))
	}
	return strings.HasSuffix(path, "_test.go"), nil
}

func copyGuardRepairChanges(worktree, repoRoot string, changed []string) error {
	for _, path := range changed {
		if err := copyGuardRepairPath(worktree, repoRoot, path); err != nil {
			return err
		}
	}
	return nil
}

func copyGuardRepairPath(worktree, repoRoot, path string) error {
	if !guardrepair.IsAllowed(path) {
		return fmt.Errorf("refuse out-of-scope guard repair integration: %s", path)
	}
	src, err := joinRepairRoot(worktree, path)
	if err != nil {
		return err
	}
	dst, err := joinRepairRoot(repoRoot, path)
	if err != nil {
		return err
	}
	if err := copyRepairEntry(src, dst, false); err != nil {
		return fmt.Errorf("integrate guard repair %s: %w", path, err)
	}
	return nil
}

func copyRepairEntry(src, dst string, allowSymlink bool) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if info.Mode().IsRegular() {
		return copyRegularRepairFile(src, dst, info.Mode().Perm())
	}
	if allowSymlink && info.Mode()&os.ModeSymlink != 0 {
		return copyRepairSymlink(src, dst)
	}
	return fmt.Errorf("unsupported repair entry type %s", info.Mode().Type())
}

func copyRegularRepairFile(src, dst string, mode os.FileMode) error {
	content, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, content, mode); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

func copyRepairSymlink(src, dst string) error {
	target, err := os.Readlink(src)
	if err != nil {
		return err
	}
	_ = os.Remove(dst)
	return os.Symlink(target, dst)
}

func joinRepairRoot(root, rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if invalidRepairRelativePath(clean) {
		return "", fmt.Errorf("invalid repository-relative path %q", rel)
	}
	full := filepath.Join(root, clean)
	relCheck, err := filepath.Rel(root, full)
	if err != nil || invalidRepairRelativePath(relCheck) {
		return "", fmt.Errorf("path escapes repository root: %q", rel)
	}
	return full, nil
}

func invalidRepairRelativePath(path string) bool {
	return path == "." || filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator))
}

func sameRepositoryBoundary(a, b state.GitSnapshot) bool {
	if a.Head != b.Head || a.IndexDigest != b.IndexDigest || a.WorktreeDigest != b.WorktreeDigest {
		return false
	}
	if a.ParentFiles == nil || b.ParentFiles == nil {
		return false
	}
	return state.SameParentFileStates(*a.ParentFiles, *b.ParentFiles)
}
