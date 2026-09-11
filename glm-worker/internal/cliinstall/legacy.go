package cliinstall

import (
	"debug/buildinfo"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const repositoryModulePath = "github.com/shinderuman/codex-worker-orchestrator/glm-worker"

func MigrateLegacy(binDir string) (bool, error) {
	if binDir == "" {
		return false, fmt.Errorf("binary directory is required")
	}
	statePath := ownershipStatePath(binDir)
	_, stateExists, err := lstat(statePath)
	if err != nil {
		return false, err
	}
	if stateExists {
		return false, nil
	}
	state, owned, err := legacyOwnershipState(binDir)
	if err != nil || !owned {
		return false, err
	}
	if err := commitLegacyState(statePath, state); err != nil {
		return false, err
	}
	return true, nil
}

func legacyOwnershipState(binDir string) (installState, bool, error) {
	state := emptyState()
	for _, name := range managedNames {
		target := filepath.Join(binDir, name)
		info, exists, err := lstat(target)
		if err != nil {
			return installState{}, false, err
		}
		if !exists || !isRegularExecutable(info) || info.Mode().Perm() != 0o755 {
			return installState{}, false, nil
		}
		owned, err := legacyRepositoryBinary(target, name)
		if err != nil {
			return installState{}, false, err
		}
		if !owned {
			return installState{}, false, nil
		}
		digest, err := hashFile(target)
		if err != nil {
			return installState{}, false, err
		}
		state.Binaries[name] = digest
	}
	return state, true, nil
}

func commitLegacyState(statePath string, state installState) error {
	stateDir := filepath.Dir(statePath)
	if err := ensureStateDir(stateDir); err != nil {
		return err
	}
	stateTemp, err := stageState(stateDir, state)
	if err != nil {
		return err
	}
	if err := os.Rename(stateTemp, statePath); err != nil {
		return errors.Join(
			fmt.Errorf("commit legacy CLI ownership state: %w", err),
			removeIfExists(stateTemp),
		)
	}
	return nil
}

func legacyRepositoryBinary(path, name string) (bool, error) {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return false, nil
	}
	return legacyBuildInfoMatches(info.Path, info.Main.Path, name), nil
}

func legacyBuildInfoMatches(commandPath, modulePath, name string) bool {
	expectedCommand := repositoryModulePath + "/cmd/" + name
	return commandPath == expectedCommand && modulePath == repositoryModulePath
}
