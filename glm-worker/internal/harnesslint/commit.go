package harnesslint

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func RunCommit(root, oid string) (Report, error) {
	workspace, err := newCommitWorkspace(root, oid)
	if err != nil {
		return Report{}, err
	}
	defer func() { _ = os.RemoveAll(workspace) }()
	before, err := state.CaptureGitSnapshot(workspace)
	if err != nil {
		return Report{}, err
	}
	report, err := Run(workspace, false)
	if err != nil {
		return report, err
	}
	after, err := state.CaptureGitSnapshot(workspace)
	if err != nil {
		return report, err
	}
	if !state.EqualGitSnapshot(before, after) {
		return Report{}, fmt.Errorf("commit lint changed its immutable input")
	}
	return report, nil
}

func newCommitWorkspace(root, oid string) (string, error) {
	if len(oid) != 40 && len(oid) != 64 {
		return "", fmt.Errorf("commit lint requires an immutable Git object ID")
	}
	if _, err := hex.DecodeString(oid); err != nil {
		return "", fmt.Errorf("invalid commit lint object ID: %w", err)
	}
	workspace, err := os.MkdirTemp("", "harnesslint-commit-*")
	if err != nil {
		return "", err
	}
	if data, err := exec.Command("git", "clone", "--shared", "--no-checkout", "--", root, workspace).CombinedOutput(); err != nil {
		_ = os.RemoveAll(workspace)
		return "", fmt.Errorf("create commit lint workspace: %w: %s", err, data)
	}
	if data, err := exec.Command("git", "-C", workspace, "checkout", "--detach", "--quiet", oid).CombinedOutput(); err != nil {
		_ = os.RemoveAll(workspace)
		return "", fmt.Errorf("checkout exact lint commit: %w: %s", err, data)
	}
	actual, err := exec.Command("git", "-C", workspace, "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(actual)) != oid {
		_ = os.RemoveAll(workspace)
		return "", fmt.Errorf("commit lint checkout identity differs: %w", err)
	}
	return workspace, nil
}
