package settingsmerge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
)

const overrideStateFile = ".codex-config-claude-env-state.json"
const overrideStateVersion = 1

type envOverride struct {
	sets    map[string]string
	deletes []string
}

type overrideState struct {
	Version int                    `json:"version"`
	Env     map[string]envBaseline `json:"env"`
}

type envBaseline struct {
	Exists bool `json:"exists"`
	Value  any  `json:"value,omitempty"`
}

type writeFileFunc func(path string, data []byte, mode os.FileMode) error

type plannedWrite struct {
	path string
	data []byte
	mode os.FileMode
}

type fileRestore struct {
	existed bool
	data    []byte
	mode    os.FileMode
}

type plannedRestore struct {
	path    string
	restore fileRestore
}

func MergeFiles(targetPath, fragmentPath, overridePath string) (bool, error) {
	return mergeFilesWithWriter(targetPath, fragmentPath, overridePath, writeAtomic)
}

func mergeFilesWithWriter(targetPath, fragmentPath, overridePath string, writeFn writeFileFunc) (bool, error) {
	target, targetMode, err := readObjectOrEmpty(targetPath)
	if err != nil {
		return false, fmt.Errorf("target JSON: %w", err)
	}
	fragment, _, err := readObject(fragmentPath)
	if err != nil {
		return false, fmt.Errorf("fragment JSON: %w", err)
	}
	override, err := parseEnvOverride(overridePath)
	if err != nil {
		return false, fmt.Errorf("env override: %w", err)
	}
	statePath := statePathFor(targetPath)
	previous, err := loadOverrideState(statePath)
	if err != nil {
		return false, fmt.Errorf("env override state: %w", err)
	}
	before := cloneMap(target)
	restoreEnvBaselines(target, previous)
	deepMerge(target, fragment)
	next := snapshotEnvBaselines(target, override)
	applyEnvPatch(target, override)
	plans, targetChanged, stateChanged, err := planWrites(targetPath, statePath, targetMode, target, next, before, previous)
	if err != nil {
		return false, err
	}
	if !targetChanged && !stateChanged {
		return false, nil
	}
	if err := commitTransaction(plans, writeFn); err != nil {
		return false, err
	}
	return targetChanged, nil
}

func planWrites(targetPath, statePath string, targetMode os.FileMode, target map[string]any, next overrideState, before map[string]any, previous overrideState) ([]plannedWrite, bool, bool, error) {
	targetChanged := !reflect.DeepEqual(before, target)
	stateChanged := !reflect.DeepEqual(next, previous)
	plans := make([]plannedWrite, 0, 2)
	if targetChanged {
		data, err := marshalObject(target)
		if err != nil {
			return nil, false, false, err
		}
		plans = append(plans, plannedWrite{path: targetPath, data: data, mode: targetMode})
	}
	if stateChanged {
		data, err := marshalObject(next)
		if err != nil {
			return nil, false, false, err
		}
		plans = append(plans, plannedWrite{path: statePath, data: data, mode: 0o600})
	}
	return plans, targetChanged, stateChanged, nil
}

func marshalObject(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func readObjectOrEmpty(path string) (map[string]any, os.FileMode, error) {
	object, mode, err := readObject(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, 0o600, nil
	}
	return object, mode, err
}

func readObject(path string) (map[string]any, os.FileMode, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	decoder := json.NewDecoder(file)
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil, 0, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, 0, fmt.Errorf("multiple JSON values")
		}
		return nil, 0, err
	}
	if object == nil {
		return nil, 0, fmt.Errorf("top-level value must be an object")
	}
	return object, stat.Mode().Perm(), nil
}

func deepMerge(target, fragment map[string]any) {
	for key, fragmentValue := range fragment {
		fragmentMap, fragmentIsMap := fragmentValue.(map[string]any)
		targetMap, targetIsMap := target[key].(map[string]any)
		if fragmentIsMap && targetIsMap {
			deepMerge(targetMap, fragmentMap)
			continue
		}
		target[key] = fragmentValue
	}
}

func cloneMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		if child, ok := item.(map[string]any); ok {
			result[key] = cloneMap(child)
			continue
		}
		result[key] = item
	}
	return result
}

func parseEnvOverride(path string) (envOverride, error) {
	if path == "" {
		return envOverride{}, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return envOverride{}, nil
	}
	if err != nil {
		return envOverride{}, err
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return envOverride{}, fmt.Errorf("override JSON: %w", err)
	}
	return parseEnvOverrideValue(raw)
}

func parseEnvOverrideValue(value any) (envOverride, error) {
	raw, ok := value.(map[string]any)
	if !ok {
		if value == nil {
			return envOverride{}, fmt.Errorf("override JSON: top-level nullは許可されません")
		}
		return envOverride{}, fmt.Errorf("override JSON: top-levelはobjectのみ許可されます")
	}
	for key := range raw {
		if key != "env" {
			return envOverride{}, fmt.Errorf("top-level key %qは許可されません (envのみ)", key)
		}
	}
	envValue, ok := raw["env"]
	if !ok {
		return envOverride{}, nil
	}
	if envValue == nil {
		return envOverride{}, fmt.Errorf("override env: nullは許可されません (objectまたは空object)")
	}
	entries, ok := envValue.(map[string]any)
	if !ok {
		return envOverride{}, fmt.Errorf("override env: objectのみ許可されます")
	}
	return parseEnvEntries(entries)
}

func parseEnvEntries(entries map[string]any) (envOverride, error) {
	override := envOverride{sets: make(map[string]string, len(entries))}
	for key, value := range entries {
		switch typed := value.(type) {
		case string:
			override.sets[key] = typed
		case nil:
			override.deletes = append(override.deletes, key)
		default:
			return envOverride{}, fmt.Errorf("override env %qはstringかnullのみ許可されます", key)
		}
	}
	return override, nil
}

func applyEnvPatch(target map[string]any, override envOverride) {
	if len(override.sets) == 0 && len(override.deletes) == 0 {
		return
	}
	env := ensureEnvMap(target)
	for _, key := range override.deletes {
		delete(env, key)
	}
	for key, value := range override.sets {
		env[key] = value
	}
	target["env"] = env
}

func ensureEnvMap(target map[string]any) map[string]any {
	if env, ok := target["env"].(map[string]any); ok {
		return env
	}
	return map[string]any{}
}

func statePathFor(targetPath string) string {
	return filepath.Join(filepath.Dir(targetPath), overrideStateFile)
}

func loadOverrideState(path string) (overrideState, error) {
	empty := overrideState{Version: overrideStateVersion, Env: map[string]envBaseline{}}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return overrideState{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.UseNumber()
	var state overrideState
	if err := decoder.Decode(&state); err != nil {
		return overrideState{}, fmt.Errorf("state JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return overrideState{}, fmt.Errorf("state: multiple JSON values")
		}
		return overrideState{}, fmt.Errorf("state JSON: %w", err)
	}
	if state.Version != overrideStateVersion {
		return overrideState{}, fmt.Errorf("state version %dは未対応 (期待 %d)", state.Version, overrideStateVersion)
	}
	if state.Env == nil {
		state.Env = map[string]envBaseline{}
	}
	return state, nil
}

func restoreEnvBaselines(target map[string]any, state overrideState) {
	if len(state.Env) == 0 {
		return
	}
	env := ensureEnvMap(target)
	for key, baseline := range state.Env {
		if baseline.Exists {
			env[key] = baseline.Value
		} else {
			delete(env, key)
		}
	}
	target["env"] = env
}

func snapshotEnvBaselines(target map[string]any, override envOverride) overrideState {
	state := overrideState{Version: overrideStateVersion, Env: map[string]envBaseline{}}
	if len(override.sets) == 0 && len(override.deletes) == 0 {
		return state
	}
	env, _ := target["env"].(map[string]any)
	for key := range override.sets {
		state.Env[key] = envBaselineOf(env, key)
	}
	for _, key := range override.deletes {
		state.Env[key] = envBaselineOf(env, key)
	}
	return state
}

func envBaselineOf(env map[string]any, key string) envBaseline {
	value, ok := env[key]
	if !ok {
		return envBaseline{}
	}
	return envBaseline{Exists: true, Value: value}
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o600
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".merge-json-*")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if _, err := bytes.NewReader(data).WriteTo(file); err != nil {
		file.Close()
		return err
	}
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func commitTransaction(plans []plannedWrite, writeFn writeFileFunc) error {
	restores := captureRestores(plans)
	for _, plan := range plans {
		if err := writeFn(plan.path, plan.data, plan.mode); err != nil {
			if rollbackErr := rollbackFiles(restores, writeFn); rollbackErr != nil {
				return fmt.Errorf("%w (rollback失敗: %w)", err, rollbackErr)
			}
			return err
		}
	}
	return nil
}

func captureRestores(plans []plannedWrite) []plannedRestore {
	restores := make([]plannedRestore, 0, len(plans))
	seen := make(map[string]bool, len(plans))
	for _, plan := range plans {
		if seen[plan.path] {
			continue
		}
		seen[plan.path] = true
		restores = append(restores, plannedRestore{path: plan.path, restore: captureFileRestore(plan.path)})
	}
	return restores
}

func captureFileRestore(path string) fileRestore {
	data, err := os.ReadFile(path)
	if err != nil {
		return fileRestore{}
	}
	info, err := os.Stat(path)
	if err != nil {
		return fileRestore{existed: true, data: data, mode: 0o600}
	}
	return fileRestore{existed: true, data: data, mode: info.Mode().Perm()}
}

func rollbackFiles(restores []plannedRestore, writeFn writeFileFunc) error {
	var errs []error
	for _, entry := range restores {
		if !entry.restore.existed {
			if err := os.Remove(entry.path); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("remove %s: %w", entry.path, err))
			}
			continue
		}
		if err := writeFn(entry.path, entry.restore.data, entry.restore.mode); err != nil {
			errs = append(errs, fmt.Errorf("restore %s: %w", entry.path, err))
		}
	}
	return errors.Join(errs...)
}
