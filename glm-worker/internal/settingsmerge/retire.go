package settingsmerge

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"syscall"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/pathalias"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/claudeoverride"
)

type claudeEmptyState struct {
	path  string
	state any
}

func RetireFiles(targetPath string) error {
	lock, err := acquireSettingsMergeLock(targetPath)
	if err != nil {
		return err
	}
	err = retireFilesUnlocked(targetPath)
	return joinSettingsMergeLockError(err, lock.Close())
}

func retireFilesUnlocked(targetPath string) error {
	inputs, err := captureClaudeRetireInputs(targetPath)
	if err != nil {
		return err
	}
	previousOverride, err := overrideStateFromSnapshot(inputs[statePathFor(targetPath)])
	if err != nil {
		return err
	}
	managed, err := managedStateFromSnapshot(inputs[ManagedStatePath(targetPath)])
	if err != nil {
		return err
	}
	if !inputs[statePathFor(targetPath)].existed && !inputs[ManagedStatePath(targetPath)].existed {
		return nil
	}
	target, mode, err := objectFromSnapshot(inputs[targetPath])
	if err != nil {
		return err
	}
	before := cloneMap(target)
	if err := restoreClaudeRetireValues(target, previousOverride, managed); err != nil {
		return err
	}
	plans, err := planClaudeRetire(targetPath, target, before, mode, inputs)
	if err != nil {
		return err
	}
	if len(plans) != 0 {
		if err := commitRecoverableTransaction(targetPath, plans, inputs, writeAtomic); err != nil {
			return err
		}
	}
	return cleanupClaudeRetireStates(targetPath, inputs)
}

func captureClaudeRetireInputs(targetPath string) (map[string]fileRestore, error) {
	paths := []string{targetPath, statePathFor(targetPath), ManagedStatePath(targetPath)}
	for _, path := range append(paths, mergeTransactionPath(targetPath)) {
		if err := rejectMergeSymlinks(path); err != nil {
			return nil, err
		}
	}
	if err := recoverSettingsTransaction(targetPath, writeAtomic); err != nil {
		return nil, err
	}
	inputs := make(map[string]fileRestore, len(paths))
	for _, path := range paths {
		snapshot, err := captureMergeTransactionFile(path)
		if err != nil {
			return nil, err
		}
		inputs[path] = snapshot
	}
	return inputs, nil
}

func restoreClaudeRetireValues(target map[string]any, previousOverride overrideState, managed managedState) error {
	if err := verifyClaudeOverrideValues(target, previousOverride); err != nil {
		return err
	}
	restoreEnvBaselines(target, previousOverride)
	if err := verifyClaudeManagedValues(target, managed); err != nil {
		return err
	}
	return restorePreviousManagedValues(target, managed.Values, map[string]managedLeaf{})
}

func verifyClaudeOverrideValues(target map[string]any, previousOverride overrideState) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	overridePath := claudeoverride.ResolvePath(home)
	if err := rejectMergeSymlinks(overridePath); err != nil {
		return err
	}
	override, err := claudeoverride.Load(overridePath)
	if err != nil {
		return err
	}
	env, _ := target["env"].(map[string]any)
	for key := range previousOverride.Env {
		if err := verifyClaudeOverrideKey(key, env, override); err != nil {
			return err
		}
	}
	return nil
}

func verifyClaudeOverrideKey(key string, env map[string]any, override claudeoverride.EnvOverride) error {
	value, exists := env[key]
	if expected, ok := override.Sets[key]; ok {
		if !exists || !reflect.DeepEqual(value, expected) {
			return fmt.Errorf("refusing user-modified Claude override env key %q", key)
		}
		return nil
	}
	for _, deleted := range override.Deletes {
		if deleted != key {
			continue
		}
		if exists {
			return fmt.Errorf("refusing externally restored Claude override env key %q", key)
		}
		return nil
	}
	return fmt.Errorf("cannot verify applied Claude override for %q (override source changed)", key)
}

func verifyClaudeManagedValues(target map[string]any, managed managedState) error {
	for _, record := range managed.Values {
		value, exists, err := managedValueAt(target, record.Path)
		if err != nil {
			return err
		}
		if !exists || !reflect.DeepEqual(value, record.Applied) {
			return fmt.Errorf("refusing user-modified managed Claude setting %s", managedPathDisplay(record.Path))
		}
	}
	return nil
}

func claudeEmptyStates(targetPath string) []claudeEmptyState {
	return []claudeEmptyState{
		{statePathFor(targetPath), overrideState{Version: overrideStateVersion, Env: map[string]envBaseline{}}},
		{ManagedStatePath(targetPath), managedState{Version: managedStateVersion, Values: []managedValueState{}}},
	}
}

func planClaudeRetire(
	targetPath string, target, before map[string]any, mode os.FileMode, inputs map[string]fileRestore,
) ([]plannedWrite, error) {
	plans := make([]plannedWrite, 0, 3)
	if !reflect.DeepEqual(before, target) {
		data, err := marshalObject(target)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plannedWrite{path: targetPath, data: data, mode: mode})
	}
	for _, entry := range claudeEmptyStates(targetPath) {
		plan, needed, err := planClaudeEmptyState(entry, inputs[entry.path])
		if err != nil {
			return nil, err
		}
		if needed {
			plans = append(plans, plan)
		}
	}
	return plans, nil
}

func planClaudeEmptyState(entry claudeEmptyState, before fileRestore) (plannedWrite, bool, error) {
	if !before.existed {
		return plannedWrite{}, false, nil
	}
	data, err := marshalObject(entry.state)
	if err != nil {
		return plannedWrite{}, false, err
	}
	if bytes.Equal(before.data, data) {
		return plannedWrite{}, false, nil
	}
	return plannedWrite{path: entry.path, data: data, mode: 0o600}, true, nil
}

func cleanupClaudeRetireStates(targetPath string, inputs map[string]fileRestore) error {
	for _, entry := range claudeEmptyStates(targetPath) {
		if err := verifyClaudeEmptyState(entry, inputs[entry.path]); err != nil {
			return err
		}
	}
	for _, entry := range claudeEmptyStates(targetPath) {
		if inputs[entry.path].existed {
			if err := os.Remove(entry.path); err != nil {
				return err
			}
		}
	}
	dir := filepath.Join(filepath.Dir(targetPath), managedStateDir)
	if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) &&
		!errors.Is(err, syscall.ENOTEMPTY) && !errors.Is(err, syscall.EEXIST) {
		return err
	}
	return nil
}

func verifyClaudeEmptyState(entry claudeEmptyState, before fileRestore) error {
	if !before.existed {
		return nil
	}
	data, err := marshalObject(entry.state)
	if err != nil {
		return err
	}
	current, err := captureMergeTransactionFile(entry.path)
	if err != nil {
		return err
	}
	if !current.existed || !bytes.Equal(current.data, data) {
		return fmt.Errorf("claude ownership state changed before cleanup: %s", entry.path)
	}
	return nil
}

func rejectMergeSymlinks(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for current := absolute; current != filepath.Dir(current); current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 && !pathalias.IsStandardDarwinVar(current) {
			return fmt.Errorf("refusing Claude symlink path: %s", current)
		}
	}
	return nil
}
