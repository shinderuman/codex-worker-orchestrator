package cliinstall

import (
	"bytes"
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

type installState struct {
	Version          int               `json:"version"`
	Binaries         map[string]string `json:"binaries"`
	ExpectedBinaries map[string]string `json:"expected_binaries"`
}

type action struct {
	name       string
	source     string
	target     string
	sourceHash string
	status     string
	change     bool
	owned      bool
	hadTarget  bool
}

type stagedAction struct {
	action      action
	replacement string
	backup      string
}

type appliedChange struct {
	target string
	backup string
}

type Result struct {
	Name   string
	Status string
}

const (
	stateVersion  = 1
	stateDirName  = ".codex-worker-orchestrator"
	stateFileName = "cli-install-state.json"
)

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

func installUnlocked(buildDir, binDir string) ([]Result, error) {
	if buildDir == "" || binDir == "" {
		return nil, fmt.Errorf("build directory and binary directory are required")
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return nil, fmt.Errorf("create binary directory: %w", err)
	}
	statePath := ownershipStatePath(binDir)
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
	return actionResults(actions), nil
}

func retireUnlocked(binDir string) ([]Result, error) {
	if binDir == "" {
		return nil, fmt.Errorf("binary directory is required")
	}
	statePath := ownershipStatePath(binDir)
	state, err := loadState(statePath)
	if err != nil {
		return nil, err
	}
	results, removals, err := planRetire(binDir, state)
	if err != nil {
		return nil, err
	}
	if len(state.Binaries) == 0 {
		if len(state.ExpectedBinaries) == 0 {
			return results, nil
		}
		if err := removeState(statePath); err != nil {
			return nil, err
		}
		return results, nil
	}
	for _, target := range removals {
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove owned binary %s: %w", target, err)
		}
	}
	if err := removeState(statePath); err != nil {
		return nil, err
	}
	return results, nil
}

func planInstall(buildDir, binDir string, state installState) ([]action, installState, error) {
	nextState := installState{
		Version:          stateVersion,
		Binaries:         cloneMap(state.Binaries),
		ExpectedBinaries: make(map[string]string, len(managedNames)),
	}
	actions := make([]action, 0, len(managedNames))
	for _, name := range managedNames {
		item, err := planBinary(buildDir, binDir, name, state.Binaries[name])
		if err != nil {
			return nil, installState{}, err
		}
		if item.owned {
			nextState.Binaries[name] = item.sourceHash
		} else {
			delete(nextState.Binaries, name)
		}
		nextState.ExpectedBinaries[name] = item.sourceHash
		actions = append(actions, item)
	}
	return actions, nextState, nil
}

func planBinary(buildDir, binDir, name, ownedHash string) (action, error) {
	source := filepath.Join(buildDir, name)
	sourceHash, err := sourceDigest(source)
	if err != nil {
		return action{}, err
	}
	target := filepath.Join(binDir, name)
	targetInfo, targetExists, err := lstat(target)
	if err != nil {
		return action{}, err
	}
	item := action{
		name:       name,
		source:     source,
		target:     target,
		sourceHash: sourceHash,
		hadTarget:  targetExists,
	}
	if ownedHash != "" {
		return planOwnedBinary(item, targetInfo, targetExists, ownedHash)
	}
	return planUnownedBinary(item, targetInfo, targetExists)
}

func planOwnedBinary(item action, targetInfo os.FileInfo, targetExists bool, ownedHash string) (action, error) {
	item.owned = true
	if !targetExists {
		item.status = "restored"
		item.change = true
		return item, nil
	}
	if err := requireOwnedTarget(item.target, targetInfo, ownedHash); err != nil {
		return action{}, err
	}
	if ownedHash == item.sourceHash {
		item.status = "unchanged"
		return item, nil
	}
	item.status = "upgraded"
	item.change = true
	return item, nil
}

func planUnownedBinary(item action, targetInfo os.FileInfo, targetExists bool) (action, error) {
	if !targetExists {
		item.status = "installed"
		item.change = true
		item.owned = true
		return item, nil
	}
	if !isRegularExecutable(targetInfo) {
		return action{}, collisionError(item.target, "preexisting path is not a regular executable")
	}
	targetHash, err := hashFile(item.target)
	if err != nil {
		return action{}, err
	}
	if targetHash != item.sourceHash {
		return action{}, collisionError(item.target, "preexisting executable is not installer-owned")
	}
	item.status = "unchanged-unowned"
	return item, nil
}

func planRetire(binDir string, state installState) ([]Result, []string, error) {
	names := make([]string, 0, len(state.Binaries))
	for name := range state.Binaries {
		names = append(names, name)
	}
	sort.Strings(names)
	results := make([]Result, 0, len(names))
	removals := make([]string, 0, len(names))
	for _, name := range names {
		result, target, remove, err := planRetireBinary(binDir, name, state.Binaries[name])
		if err != nil {
			return nil, nil, err
		}
		results = append(results, result)
		if remove {
			removals = append(removals, target)
		}
	}
	return results, removals, nil
}

func planRetireBinary(binDir, name, digest string) (Result, string, bool, error) {
	target := filepath.Join(binDir, name)
	owned, err := targetMatchesOwnership(target, digest)
	if err != nil {
		return Result{}, "", false, err
	}
	if !owned {
		return Result{Name: name, Status: "preserved"}, target, false, nil
	}
	return Result{Name: name, Status: "removed"}, target, true, nil
}

func applyInstall(actions []action, statePath string, nextState installState) error {
	changed := changedActions(actions)
	stateDir := filepath.Dir(statePath)
	if err := ensureStateDir(stateDir); err != nil {
		return err
	}
	stateTemp, err := stageState(stateDir, nextState)
	if err != nil {
		return err
	}
	staged, err := stageActions(changed)
	if err != nil {
		return errors.Join(err, removeIfExists(stateTemp))
	}
	applied, err := commitActions(staged)
	if err != nil {
		return errors.Join(err, cleanupStaged(staged), removeIfExists(stateTemp))
	}
	if err := os.Rename(stateTemp, statePath); err != nil {
		return errors.Join(
			fmt.Errorf("commit binary ownership state: %w", err),
			rollback(applied),
			cleanupStaged(staged),
			removeIfExists(stateTemp),
		)
	}
	return cleanupStaged(staged)
}

func changedActions(actions []action) []action {
	changed := make([]action, 0, len(actions))
	for _, item := range actions {
		if item.change {
			changed = append(changed, item)
		}
	}
	return changed
}

func stageActions(actions []action) ([]stagedAction, error) {
	staged := make([]stagedAction, 0, len(actions))
	for _, item := range actions {
		current, err := stageAction(item)
		if err != nil {
			return nil, errors.Join(err, cleanupStaged(staged))
		}
		staged = append(staged, current)
	}
	return staged, nil
}

func stageAction(item action) (stagedAction, error) {
	dir := filepath.Dir(item.target)
	replacement, err := stageBinary(item.source, dir, item.name)
	if err != nil {
		return stagedAction{}, err
	}
	current := stagedAction{action: item, replacement: replacement}
	if !item.hadTarget {
		return current, nil
	}
	backup, err := stageBinary(item.target, dir, item.name+"-backup")
	if err != nil {
		return stagedAction{}, errors.Join(err, removeIfExists(replacement))
	}
	current.backup = backup
	return current, nil
}

func commitActions(staged []stagedAction) ([]appliedChange, error) {
	applied := make([]appliedChange, 0, len(staged))
	for index := range staged {
		item := &staged[index]
		if err := os.Rename(item.replacement, item.action.target); err != nil {
			return nil, errors.Join(
				fmt.Errorf("install binary %s: %w", item.action.target, err),
				rollback(applied),
			)
		}
		item.replacement = ""
		applied = append(applied, appliedChange{target: item.action.target, backup: item.backup})
	}
	return applied, nil
}

func rollback(applied []appliedChange) error {
	var result error
	for index := len(applied) - 1; index >= 0; index-- {
		item := applied[index]
		if item.backup == "" {
			result = errors.Join(result, removeIfExists(item.target))
			continue
		}
		if err := os.Rename(item.backup, item.target); err != nil {
			result = errors.Join(result, fmt.Errorf("rollback restore %s: %w", item.target, err))
		}
	}
	return result
}

func loadState(statePath string) (installState, error) {
	if err := validateStateDirectory(filepath.Dir(statePath)); err != nil {
		return installState{}, err
	}
	info, exists, err := lstat(statePath)
	if err != nil {
		return installState{}, err
	}
	if !exists {
		return emptyState(), nil
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return installState{}, fmt.Errorf("CLI ownership state is not a regular file: %s", statePath)
	}
	state, err := decodeState(statePath)
	if err != nil {
		return installState{}, err
	}
	if err := validateState(state); err != nil {
		return installState{}, err
	}
	return state, nil
}

func validateStateDirectory(path string) error {
	info, exists, err := lstat(path)
	if err != nil || !exists {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("CLI ownership state directory is not a regular directory: %s", path)
	}
	return nil
}

func decodeState(path string) (installState, error) {
	file, err := os.Open(path)
	if err != nil {
		return installState{}, fmt.Errorf("open CLI ownership state: %w", err)
	}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var state installState
	decodeErr := decoder.Decode(&state)
	closeErr := file.Close()
	if decodeErr != nil {
		return installState{}, errors.Join(fmt.Errorf("decode CLI ownership state: %w", decodeErr), closeErr)
	}
	if closeErr != nil {
		return installState{}, fmt.Errorf("close CLI ownership state: %w", closeErr)
	}
	return state, nil
}

func validateState(state installState) error {
	if state.Version != stateVersion || state.Binaries == nil {
		return fmt.Errorf("invalid CLI ownership state")
	}
	for name, digest := range state.Binaries {
		if _, ok := managedNameSet[name]; !ok || !validDigest(digest) {
			return fmt.Errorf("invalid CLI ownership state entry: %s", name)
		}
	}
	for name, digest := range state.ExpectedBinaries {
		if _, ok := managedNameSet[name]; !ok || !validDigest(digest) {
			return fmt.Errorf("invalid CLI expected identity state entry: %s", name)
		}
	}
	return nil
}

func ensureStateDir(path string) error {
	info, exists, err := lstat(path)
	if err != nil {
		return err
	}
	if exists {
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
	return writeTempFile(dir, ".cli-install-state-*.tmp", bytes.NewReader(data), 0o600)
}

func stageBinary(source, dir, label string) (string, error) {
	input, err := os.Open(source)
	if err != nil {
		return "", fmt.Errorf("open binary %s: %w", source, err)
	}
	path, stageErr := writeTempFile(dir, "."+label+"-*.tmp", input, 0o755)
	closeErr := input.Close()
	if stageErr == nil && closeErr == nil {
		return path, nil
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("close binary %s: %w", source, closeErr)
	}
	return "", errors.Join(stageErr, closeErr, removeIfExists(path))
}

func writeTempFile(dir, pattern string, source io.Reader, mode os.FileMode) (string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	path := file.Name()
	if _, err := io.Copy(file, source); err != nil {
		return "", discardOpenTemp(file, path, fmt.Errorf("write temp file: %w", err))
	}
	if err := file.Chmod(mode); err != nil {
		return "", discardOpenTemp(file, path, fmt.Errorf("chmod temp file: %w", err))
	}
	if err := file.Close(); err != nil {
		return "", errors.Join(fmt.Errorf("close temp file: %w", err), removeIfExists(path))
	}
	return path, nil
}

func discardOpenTemp(file *os.File, path string, cause error) error {
	return errors.Join(cause, file.Close(), removeIfExists(path))
}

func removeState(statePath string) error {
	if err := removeIfExists(statePath); err != nil {
		return fmt.Errorf("remove CLI ownership state: %w", err)
	}
	stateDir := filepath.Dir(statePath)
	if err := cleanupStaleStateTemps(stateDir); err != nil {
		return err
	}
	if err := removeIfExists(stateDir); err != nil {
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

func sourceDigest(path string) (string, error) {
	info, err := regularFileInfo(path)
	if err != nil {
		return "", fmt.Errorf("source binary %s: %w", path, err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("source binary is not executable: %s", path)
	}
	return hashFile(path)
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
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return "", errors.Join(fmt.Errorf("digest %s: %w", path, copyErr), closeErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close %s after digest: %w", path, closeErr)
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

func ownershipStatePath(binDir string) string {
	return filepath.Join(binDir, stateDirName, stateFileName)
}

func emptyState() installState {
	return installState{
		Version:          stateVersion,
		Binaries:         map[string]string{},
		ExpectedBinaries: map[string]string{},
	}
}

func cloneMap(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func actionResults(actions []action) []Result {
	results := make([]Result, 0, len(actions))
	for _, item := range actions {
		results = append(results, Result{Name: item.name, Status: item.status})
	}
	return results
}

func cleanupStaged(staged []stagedAction) error {
	var result error
	for _, item := range staged {
		result = errors.Join(result, removeIfExists(item.replacement), removeIfExists(item.backup))
	}
	return result
}

func removeIfExists(path string) error {
	if path == "" {
		return nil
	}
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
