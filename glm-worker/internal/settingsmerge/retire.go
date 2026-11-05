package settingsmerge

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"syscall"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/claudeoverride"
)

func RetireFiles(targetPath string) error {
	lock, err := acquireSettingsMergeLock(targetPath)
	if err != nil {
		return err
	}
	err = retireFilesUnlocked(targetPath)
	return joinSettingsMergeLockError(err, lock.Close())
}

func retireFilesUnlocked(targetPath string) error {
	for _, path := range []string{targetPath, statePathFor(targetPath), ManagedStatePath(targetPath), mergeTransactionPath(targetPath)} {
		if err := rejectMergeSymlinks(path); err != nil {
			return err
		}
	}
	if err := recoverSettingsTransaction(targetPath, writeAtomic); err != nil {
		return err
	}
	paths := []string{targetPath, statePathFor(targetPath), ManagedStatePath(targetPath)}
	inputs := make(map[string]fileRestore, len(paths))
	for _, path := range paths {
		if err := rejectMergeSymlinks(path); err != nil {
			return err
		}
		snapshot, err := captureMergeTransactionFile(path)
		if err != nil {
			return err
		}
		inputs[path] = snapshot
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
	deleted := make(map[string]bool, len(override.Deletes))
	for _, key := range override.Deletes {
		deleted[key] = true
	}
	for key := range previousOverride.Env {
		value, exists := env[key]
		if expected, ok := override.Sets[key]; ok {
			if !exists || !reflect.DeepEqual(value, expected) {
				return fmt.Errorf("refusing user-modified Claude override env key %q", key)
			}
		} else if deleted[key] {
			if exists {
				return fmt.Errorf("refusing externally restored Claude override env key %q", key)
			}
		} else {
			return fmt.Errorf("cannot verify applied Claude override for %q (override source changed)", key)
		}
	}
	restoreEnvBaselines(target, previousOverride)
	for _, record := range managed.Values {
		value, exists, err := managedValueAt(target, record.Path)
		if err != nil {
			return err
		}
		if !exists || !reflect.DeepEqual(value, record.Applied) {
			return fmt.Errorf("refusing user-modified managed Claude setting %s", managedPathDisplay(record.Path))
		}
	}
	if err := restorePreviousManagedValues(target, managed.Values, map[string]managedLeaf{}); err != nil {
		return err
	}
	plans := make([]plannedWrite, 0, 3)
	if !reflect.DeepEqual(before, target) {
		data, err := marshalObject(target)
		if err != nil {
			return err
		}
		plans = append(plans, plannedWrite{path: targetPath, data: data, mode: mode})
	}
	emptyOverride := overrideState{Version: overrideStateVersion, Env: map[string]envBaseline{}}
	emptyManaged := managedState{Version: managedStateVersion, Values: []managedValueState{}}
	for _, entry := range []struct {
		path  string
		state any
	}{
		{statePathFor(targetPath), emptyOverride},
		{ManagedStatePath(targetPath), emptyManaged},
	} {
		if !inputs[entry.path].existed {
			continue
		}
		data, err := marshalObject(entry.state)
		if err != nil {
			return err
		}
		if !bytes.Equal(inputs[entry.path].data, data) {
			plans = append(plans, plannedWrite{path: entry.path, data: data, mode: 0o600})
		}
	}
	if len(plans) != 0 {
		if err := commitRecoverableTransaction(targetPath, plans, inputs, writeAtomic); err != nil {
			return err
		}
	}
	for _, entry := range []struct {
		path  string
		state any
	}{
		{statePathFor(targetPath), emptyOverride},
		{ManagedStatePath(targetPath), emptyManaged},
	} {
		if !inputs[entry.path].existed {
			continue
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
			return fmt.Errorf("Claude ownership state changed before cleanup: %s", entry.path)
		}
		if err := os.Remove(entry.path); err != nil {
			return err
		}
	}
	dir := filepath.Join(filepath.Dir(targetPath), managedStateDir)
	if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) &&
		!errors.Is(err, syscall.ENOTEMPTY) && !errors.Is(err, syscall.EEXIST) {
		return err
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
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing Claude symlink path: %s", current)
		}
	}
	return nil
}
