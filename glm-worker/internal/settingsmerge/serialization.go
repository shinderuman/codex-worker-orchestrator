package settingsmerge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
)

const settingsMergeLockSuffix = ".settings-merge.lock"

func acquireSettingsMergeLock(targetPath string) (*repolock.Lock, error) {
	path := settingsMergeLockPath(targetPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create settings merge lock directory: %w", err)
	}
	lock, err := repolock.AcquireWait(path)
	if err != nil {
		return nil, fmt.Errorf("acquire settings merge lock: %w", err)
	}
	return lock, nil
}

func settingsMergeLockPath(targetPath string) string {
	return filepath.Join(filepath.Dir(targetPath), managedStateDir, filepath.Base(targetPath)+settingsMergeLockSuffix)
}

func joinSettingsMergeLockError(operationErr, closeErr error) error {
	if operationErr != nil {
		if closeErr != nil {
			return errors.Join(operationErr, fmt.Errorf("release settings merge lock: %w", closeErr))
		}
		return operationErr
	}
	if closeErr != nil {
		return fmt.Errorf("release settings merge lock: %w", closeErr)
	}
	return nil
}
