package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func recordParentReviewTaskChange(t *testing.T, cfg config.AppConfig) {
	t.Helper()
	path := filepath.Join(cfg.RepoRoot, "parent-review-task-change.txt")
	if err := os.WriteFile(path, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
