package app

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/abeval"
)

func TestExecuteEvalABWorktreeIdentityReadOnly(t *testing.T) {
	cfg := evalABTestConfig(t)
	path := t.TempDir()
	runControllerActivationGit(t, path, "init", "-q")
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeEvalABWorktreeIdentity, Payload: path}, cfg, nil, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var result struct {
		WorktreePath string                  `json:"worktree_path"`
		Identity     abeval.WorktreeIdentity `json:"worktree_identity"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.WorktreePath != canonical || result.Identity.ResolvedPath != canonical || result.Identity.Device == 0 || result.Identity.Inode == 0 {
		t.Fatalf("worktree identity capture: %+v", result)
	}
}
