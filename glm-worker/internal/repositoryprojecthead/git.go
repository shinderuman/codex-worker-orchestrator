package repositoryprojecthead

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type planSnapshot struct {
	Plan    string
	Head    string
	Status  string
	Present bool
}

func finalHeadPlan(root string) (planSnapshot, error) {
	repository, err := finalHeadRepository(root)
	if err != nil {
		return planSnapshot{}, err
	}
	if !repository {
		return planSnapshot{Status: "skipped (not a git repository)"}, nil
	}
	head, err := state.ResolveGitHeadAuthority("git", root)
	if err != nil {
		return planSnapshot{}, fmt.Errorf("final HEAD authorityを確認できません: %w", err)
	}
	if head.Unborn {
		return planSnapshot{Status: "skipped (no commits)"}, nil
	}
	if _, err := gitOutput(root, "ls-files", "--error-unmatch", "--", state.ParentPlanFile); err != nil {
		if gitExitCode(err) == 1 {
			return planSnapshot{Status: "skipped (IMPLEMENTATION_PLAN.local.md is untracked)"}, nil
		}
		return planSnapshot{}, fmt.Errorf("IMPLEMENTATION_PLAN.local.mdのindex追跡状態を確認できません: %w", err)
	}
	entry, err := gitOutput(root, "ls-tree", head.Head, "--", state.ParentPlanFile)
	if err != nil {
		return planSnapshot{}, fmt.Errorf("HEADのIMPLEMENTATION_PLAN.local.md存在状態を確認できません: %w", err)
	}
	if strings.TrimSpace(entry) == "" {
		return planSnapshot{Head: head.Head, Status: "skipped (IMPLEMENTATION_PLAN.local.md is not in HEAD yet)"}, nil
	}
	plan, err := gitOutput(root, "show", head.Head+":"+state.ParentPlanFile)
	if err != nil {
		return planSnapshot{}, fmt.Errorf("HEADのIMPLEMENTATION_PLAN.local.mdを読めません: %w", err)
	}
	return planSnapshot{Plan: plan, Head: head.Head, Present: true}, nil
}

func finalHeadRepository(root string) (bool, error) {
	if _, err := gitOutput(root, "rev-parse", "--git-dir"); err == nil {
		return true, nil
	} else {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return false, err
		}
		metadata, metadataErr := gitMetadataPresent(root)
		if metadataErr != nil {
			return false, metadataErr
		}
		if metadata {
			return false, err
		}
	}
	return false, nil
}

func gitMetadataPresent(root string) (bool, error) {
	dir, err := filepath.Abs(root)
	if err != nil {
		return false, fmt.Errorf("repository rootを解決できません: %w", err)
	}
	for {
		gitPath := filepath.Join(dir, ".git")
		if _, err := os.Lstat(gitPath); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("git metadataを確認できません (%s): %w", gitPath, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false, nil
		}
		dir = parent
	}
}

func gitExitCode(err error) int {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return -1
	}
	return exitErr.ExitCode()
}

func gitOutput(root string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", root}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimRight(string(output), "\n"), nil
}
