package settingsmerge

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
)

const settingsMergeLockDirName = "settings-merge-locks"

func acquireSettingsMergeLock(targetPath string) (*repolock.Lock, error) {
	path, err := settingsMergeLockPath(targetPath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create settings merge lock directory: %w", err)
	}
	lock, err := repolock.AcquireWait(path)
	if err != nil {
		return nil, fmt.Errorf("acquire settings merge lock: %w", err)
	}
	return lock, nil
}

func settingsMergeLockPath(targetPath string) (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache directory: %w", err)
	}
	absoluteTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return "", fmt.Errorf("resolve settings merge target path: %w", err)
	}
	digest := sha256.Sum256([]byte(filepath.Clean(absoluteTarget)))
	return filepath.Join(
		cacheDir,
		"codex-worker-orchestrator",
		settingsMergeLockDirName,
		hex.EncodeToString(digest[:])+".lock",
	), nil
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
