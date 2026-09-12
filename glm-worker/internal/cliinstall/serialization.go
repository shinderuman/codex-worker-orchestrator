package cliinstall

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
)

const installLockFileName = ".codex-worker-orchestrator-cli-install.lock"

func Install(buildDir, binDir string) ([]Result, error) {
	if buildDir == "" || binDir == "" {
		return installUnlocked(buildDir, binDir)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return nil, fmt.Errorf("create binary directory: %w", err)
	}
	lock, err := acquireInstallLock(binDir)
	if err != nil {
		return nil, err
	}
	results, installErr := installUnlocked(buildDir, binDir)
	return results, joinCloseError(installErr, lock.Close())
}

func Retire(binDir string) ([]Result, error) {
	if binDir == "" {
		return retireUnlocked(binDir)
	}
	lock, err := acquireInstallLock(binDir)
	if err != nil {
		return nil, err
	}
	results, retireErr := retireUnlocked(binDir)
	return results, joinCloseError(retireErr, lock.Close())
}

func acquireInstallLock(binDir string) (*repolock.Lock, error) {
	path := installLockPath(binDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create CLI install lock directory: %w", err)
	}
	lock, err := repolock.AcquireWait(path)
	if err != nil {
		return nil, fmt.Errorf("acquire CLI install lock: %w", err)
	}
	return lock, nil
}

func installLockPath(binDir string) string {
	clean := filepath.Clean(binDir)
	return filepath.Join(filepath.Dir(clean), "."+filepath.Base(clean)+installLockFileName)
}

func joinCloseError(operationErr, closeErr error) error {
	if operationErr != nil {
		if closeErr != nil {
			return errors.Join(operationErr, fmt.Errorf("release CLI install lock: %w", closeErr))
		}
		return operationErr
	}
	if closeErr != nil {
		return fmt.Errorf("release CLI install lock: %w", closeErr)
	}
	return nil
}

func cleanupStaleStateTemps(dir string) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list CLI ownership state directory: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, ".cli-install-state-") || !strings.HasSuffix(name, ".tmp") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect stale CLI ownership state temp %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove stale CLI ownership state temp %s: %w", name, err)
		}
	}
	return nil
}
