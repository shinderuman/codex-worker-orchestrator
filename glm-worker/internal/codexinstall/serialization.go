package codexinstall

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
)

const codexInstallLockFileName = ".codex-worker-orchestrator-install.lock"

func acquireInstallLock(codexDir string) (*repolock.Lock, error) {
	path := codexInstallLockPath(codexDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create Codex install lock directory: %w", err)
	}
	lock, err := repolock.AcquireWait(path)
	if err != nil {
		return nil, fmt.Errorf("acquire Codex install lock: %w", err)
	}
	return lock, nil
}

func codexInstallLockPath(codexDir string) string {
	clean := filepath.Clean(codexDir)
	return filepath.Join(filepath.Dir(clean), "."+filepath.Base(clean)+codexInstallLockFileName)
}

func joinInstallLockError(operationErr, closeErr error) error {
	if operationErr != nil {
		if closeErr != nil {
			return errors.Join(operationErr, fmt.Errorf("release Codex install lock: %w", closeErr))
		}
		return operationErr
	}
	if closeErr != nil {
		return fmt.Errorf("release Codex install lock: %w", closeErr)
	}
	return nil
}
