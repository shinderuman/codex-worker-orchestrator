package cliinstall

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const stateVersion = 1

var managedNames = []string{
	"glm-worker",
	"glm-parent-action",
	"glm-codex-context",
	"commentlint",
	"harnesslint",
}

var managedNameSet = func() map[string]struct{} {
	result := make(map[string]struct{}, len(managedNames))
	for _, name := range managedNames {
		result[name] = struct{}{}
	}
	return result
}()

type installState struct {
	Version  int               `json:"version"`
	Binaries map[string]string `json:"binaries"`
}

type action struct {
	name       string
	source     string
	target     string
	sourceHash string
	status     string
	change     bool
	hadTarget  bool
}

type appliedChange struct {
	target string
	backup string
}

type Result struct {
	Name   string
	Status string
}

func Install(buildDir, binDir string) ([]Result, error) {
	if buildDir == "" || binDir == "" {
		return nil, fmt.Errorf("build directory and binary directory are required")
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return nil, fmt.Errorf("create binary directory: %w", err)
	}
	statePath := filepath.Join(binDir, ".codex-worker-orchestrator", "cli-install-state.json")
	state, err := loadState(statePath)
	if err != nil {
		return nil, err
	}
	actions, nextState, err := planInstall(buildDir, binDir, state)
	if err != nil {
		return nil, err
	}
	if err := applyInstall(actions, statePath, nextState); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(actions))
	for _, item := range actions {
		results = append(results, Result{Name: item.name, Status: item.status})
	}
	return results, nil
}

func Retire(binDir string) ([]Result, error) {
	if binDir == "" {
		return nil, fmt.Errorf("binary directory is required")
	}
	statePath := filepath.Join(binDir, ".codex-worker-orchestrator", "cli-install-state.json")
	state, err := loadState(statePath)
	if err != nil {
		return nil, err
	}
	if len(state.Binaries) == 0 {
		return nil, nil
	}

	names := make([]string, 0, len(state.Binaries))
	for name := range state.Binaries {
		names = append(names, name)
	}
	sort.Strings(names)
	results := make([]Result, 0, len(names))
	for _, name := range names {
		target := filepath.Join(binDir, name)
		owned, err := targetMatchesOwnership(target, state.Binaries[name])
		if err != nil {
			return nil, err
		}
		if !owned {
			results = append(results, Result{Name: name, Status: "preserved"})
			continue
		}
		if err := os.Remove(target); err != nil {
			return nil, fmt.Errorf("remove owned binary %s: %w", target, err)
		}
		results = append(results, Result{Name: name, Status: "removed"})
	}
	if err := removeState(statePath); err != nil {
		return nil, err
	}
	return results, nil
}

func planInstall(buildDir, binDir string, state installState) ([]action, installState, error) {
	nextState := installState{Version: stateVersion, Binaries: cloneMap(state.Binaries)}
	actions := make([]action, 0, len(managedNames))
	for _, name := range managedNames {
		source := filepath.Join(buildDir, name)
		sourceInfo, err := regularFileInfo(source)
		if err != nil {
			return nil, installState{}, fmt.Errorf("source binary %s: %w", source, err)
		}
		if sourceInfo.Mode().Perm()&0o111 == 0 {
			return nil, installState{}, fmt.Errorf("source binary is not executable: %s", source)
		}
		sourceHash, err := hashFile(source)
		if err != nil {
			return nil, installState{}, err
		}
		target := filepath.Join(binDir, name)
		targetInfo, targetExists, err := lstat(target)
		if err != nil {
			return nil, installState{}, err
		}
		ownedHash, hasOwnership := state.Binaries[name]
		item := action{name: name, source: source, target: target, sourceHash: sourceHash, hadTarget: targetExists}

		if hasOwnership {
			if !targetExists {
				item.status = "restored"
				item.change = true
				nextState.Binaries[name] = sourceHash
				actions = append(actions, item)
				continue
			}
			if err := requireOwnedTarget(target, targetInfo, ownedHash); err != nil {
				return nil, installState{}, err
			}
			if ownedHash == sourceHash {
				item.status = "unchanged"
			} else {
				item.status = "upgraded"
				item.change = true
				nextState.Binaries[name] = sourceHash
			}
			actions = append(actions, item)
			continue
		}

		if !targetExists {
			item.status = "installed"
			item.change = true
			nextState.Binaries[name] = sourceHash
			actions = append(actions, item)
			continue
		}
		if !isRegularExecutable(targetInfo) {
			return nil, installState{}, collisionError(target, "preexisting path is not a regular executable")
		}
		targetHash, err := hashFile(target)
		if err != nil {
			return nil, installState{}, err
		}
		if targetHash != sourceHash {
			return nil, installState{}, collisionError(target, "preexisting executable is not installer-owned")
		}
		item.status = "unchanged-unowned"
		actions = append(actions, item)
	}
	return actions, nextState, nil
}

func applyInstall(actions []action, statePath string, nextState installState) error {
	changed := make([]action, 0, len(actions))
	for _, item := range actions {
		if item.change {
			changed = append(changed, item)
		}
	}
	if len(changed) == 0 {
		return nil
	}
	stateDir := filepath.Dir(statePath)
	if err := ensureStateDir(stateDir); err != nil {
		return err
	}
	stateTemp, err := stageState(stateDir, nextState)
	if err != nil {
		return err
	}
	defer os.Remove(stateTemp)

	staged := make(map[string]string, len(changed))
	backups := make(map[string]string, len(changed))
	defer cleanupPaths(staged)
	defer cleanupPaths(backups)
	for _, item := range changed {
		path, err := stageBinary(item.source, filepath.Dir(item.target), item.name)
		if err != nil {
			return err
		}
		staged[item.name] = path
		if item.hadTarget {
			backup, err := stageBinary(item.target, filepath.Dir(item.target), item.name+"-backup")
			if err != nil {
				return err
			}
			backups[item.name] = backup
		}
	}

	applied := make([]appliedChange, 0, len(changed))
	for _, item := range changed {
		if err := os.Rename(staged[item.name], item.target); err != nil {
			rollbackErr := rollback(applied)
			return errors.Join(fmt.Errorf("install binary %s: %w", item.target, err), rollbackErr)
		}
		delete(staged, item.name)
		applied = append(applied, appliedChange{target: item.target, backup: backups[item.name]})
	}
	if err := os.Rename(stateTemp, statePath); err != nil {
		rollbackErr := rollback(applied)
		return errors.Join(fmt.Errorf("commit binary ownership state: %w", err), rollbackErr)
	}
	return nil
}

func rollback(applied []appliedChange) error {
	var result error
	for i := len(applied) - 1; i >= 0; i-- {
		item := applied[i]
		if item.backup == "" {
			if err := os.Remove(item.target); err != nil && !errors.Is(err, os.ErrNotExist) {
				result = errors.Join(result, fmt.Errorf("rollback remove %s: %w", item.target, err))
			}
			continue
		}
		if err := os.Rename(item.backup, item.target); err != nil {
			result = errors.Join(result, fmt.Errorf("rollback restore %s: %w", item.target, err))
		}
	}
	return result
}

func loadState(statePath string) (installState, error) {
	stateDir := filepath.Dir(statePath)
	if info, exists, err := lstat(stateDir); err != nil {
		return installState{}, err
	} else if exists && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		return installState{}, fmt.Errorf("CLI ownership state directory is not a regular directory: %s", stateDir)
	}
	info, exists, err := lstat(statePath)
	if err != nil {
		return installState{}, err
	}
	if !exists {
		return installState{Version: stateVersion, Binaries: map[string]string{}}, nil
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return installState{}, fmt.Errorf("CLI ownership state is not a regular file: %s", statePath)
	}
	file, err := os.Open(statePath)
	if err != nil {
		return installState{}, fmt.Errorf("open CLI ownership state: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var state installState
	if err := decoder.Decode(&state); err != nil {
		return installState{}, fmt.Errorf("decode CLI ownership state: %w", err)
	}
	if state.Version != stateVersion || state.Binaries == nil {
		return installState{}, fmt.Errorf("invalid CLI ownership state")
	}
	for name, digest := range state.Binaries {
		if _, ok := managedNameSet[name]; !ok || !validDigest(digest) {
			return installState{}, fmt.Errorf("invalid CLI ownership state entry: %s", name)
		}
	}
	return state, nil
}

func ensureStateDir(path string) error {
	if info, exists, err := lstat(path); err != nil {
		return err
	} else if exists {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("CLI ownership state directory is not a regular directory: %s", path)
		}
		return nil
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return fmt.Errorf("create CLI ownership state directory: %w", err)
	}
	return nil
}

func stageState(dir string, state installState) (string, error) {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode CLI ownership state: %w", err)
	}
	data = append(data, '\n')
	file, err := os.CreateTemp(dir, ".cli-install-state-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create CLI ownership state temp file: %w", err)
	}
	path := file.Name()
	if _, err := file.Write(data); err != nil {
		file.Close()
		os.Remove(path)
		return "", fmt.Errorf("write CLI ownership state: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		os.Remove(path)
		return "", fmt.Errorf("chmod CLI ownership state: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("close CLI ownership state: %w", err)
	}
	return path, nil
}

func stageBinary(source, dir, label string) (string, error) {
	input, err := os.Open(source)
	if err != nil {
		return "", fmt.Errorf("open binary %s: %w", source, err)
	}
	defer input.Close()
	output, err := os.CreateTemp(dir, "."+label+"-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create binary temp file: %w", err)
	}
	path := output.Name()
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		os.Remove(path)
		return "", fmt.Errorf("copy binary %s: %w", source, err)
	}
	if err := output.Chmod(0o755); err != nil {
		output.Close()
		os.Remove(path)
		return "", fmt.Errorf("chmod binary temp file: %w", err)
	}
	if err := output.Close(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("close binary temp file: %w", err)
	}
	return path, nil
}

func removeState(statePath string) error {
	if err := os.Remove(statePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove CLI ownership state: %w", err)
	}
	stateDir := filepath.Dir(statePath)
	if err := os.Remove(stateDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove CLI ownership state directory: %w", err)
	}
	return nil
}

func targetMatchesOwnership(path, digest string) (bool, error) {
	info, exists, err := lstat(path)
	if err != nil || !exists {
		return false, err
	}
	if !isRegularExecutable(info) || info.Mode().Perm() != 0o755 {
		return false, nil
	}
	observed, err := hashFile(path)
	if err != nil {
		return false, err
	}
	return observed == digest, nil
}

func requireOwnedTarget(path string, info os.FileInfo, digest string) error {
	if !isRegularExecutable(info) || info.Mode().Perm() != 0o755 {
		return collisionError(path, "installer-owned binary shape or mode changed externally")
	}
	observed, err := hashFile(path)
	if err != nil {
		return err
	}
	if observed != digest {
		return collisionError(path, "installer-owned binary content changed externally")
	}
	return nil
}

func regularFileInfo(path string) (os.FileInfo, error) {
	info, exists, err := lstat(path)
	if err != nil {
		return nil, err
	}
	if !exists || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	return info, nil
}

func isRegularExecutable(info os.FileInfo) bool {
	return info.Mode()&os.ModeSymlink == 0 && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func lstat(path string) (os.FileInfo, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect %s: %w", path, err)
	}
	return info, true, nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s for digest: %w", path, err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("digest %s: %w", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func collisionError(path, reason string) error {
	return fmt.Errorf("repository CLI collision: %s: %s; preserve or move the existing executable before installing", path, reason)
}

func cloneMap(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func cleanupPaths(paths map[string]string) {
	for _, path := range paths {
		os.Remove(path)
	}
}
