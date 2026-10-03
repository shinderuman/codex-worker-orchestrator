package app

import (
	"os"

	"path/filepath"

	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const (
	codexTestParentThreadID   = "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	codexTestParentSessionID  = "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	codexTestGuardianThreadID = "01a04f5d-bbf3-7773-9792-61d5aa28e2f9"
	codexTestOtherThreadID    = "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
)

func newCodexBundleTestState(t *testing.T) (config.AppConfig, *state.StateStore, string) {
	cfg, st := newBundleTestState(t)
	codexHome := filepath.Join(t.TempDir(), ".codex")
	cfg.CodexConfigDir = codexHome
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	return cfg, st, codexHome
}
