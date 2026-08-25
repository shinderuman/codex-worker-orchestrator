package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
)

func main() {
	target := flag.String("target", "", "merge target JSON file")
	fragment := flag.String("fragment", "", "managed JSON fragment")
	envOverride := flag.String("env-override", "", "local Claude settings env override JSON (string=set, null=delete)")
	flag.Parse()

	if *target == "" || *fragment == "" {
		fail("-target and -fragment are required")
	}

	changed, err := mergeFiles(*target, *fragment, *envOverride)
	if err != nil {
		fail(err.Error())
	}

	if changed {
		fmt.Println("updated")
	} else {
		fmt.Println("unchanged")
	}
}

func mergeFiles(targetPath string, fragmentPath string, overridePath string) (bool, error) {
	return mergeFilesWithWriter(targetPath, fragmentPath, overridePath, writeAtomic)
}

func mergeFilesWithWriter(targetPath string, fragmentPath string, overridePath string, writeFn writeFileFunc) (bool, error) {
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
	prevState, err := loadOverrideState(statePath)
	if err != nil {
		return false, fmt.Errorf("env override state: %w", err)
	}

	before := cloneMap(target)
	restoreEnvBaselines(target, prevState)
	deepMerge(target, fragment)
	newState := snapshotEnvBaselines(target, override)
	applyEnvPatch(target, override)

	targetChanged := !reflect.DeepEqual(before, target)
	stateChanged := !reflect.DeepEqual(newState, prevState)
	if !targetChanged && !stateChanged {
		return false, nil
	}

	var plans []plannedWrite
	if targetChanged {
		data, err := json.MarshalIndent(target, "", "  ")
		if err != nil {
			return false, err
		}
		plans = append(plans, plannedWrite{path: targetPath, data: append(data, '\n'), mode: targetMode})
	}
	if stateChanged {
		data, err := json.MarshalIndent(newState, "", "  ")
		if err != nil {
			return false, err
		}
		plans = append(plans, plannedWrite{path: statePath, data: append(data, '\n'), mode: 0o600})
	}

	if err := commitTransaction(plans, writeFn); err != nil {
		return false, err
	}
	return targetChanged, nil
}

func readObjectOrEmpty(path string) (map[string]any, os.FileMode, error) {
	object, mode, err := readObject(path)
	if os.IsNotExist(err) {
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

func deepMerge(target map[string]any, fragment map[string]any) {
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

type envOverride struct {
	sets    map[string]string
	deletes []string
}

func parseEnvOverride(path string) (envOverride, error) {
	var override envOverride
	if path == "" {
		return override, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return override, nil
		}
		return override, err
	}

	var topLevel any
	if err := json.Unmarshal(data, &topLevel); err != nil {
		return override, fmt.Errorf("override JSON: %w", err)
	}
	if topLevel == nil {
		return override, fmt.Errorf("override JSON: top-level nullは許可されません")
	}
	raw, ok := topLevel.(map[string]any)
	if !ok {
		return override, fmt.Errorf("override JSON: top-levelはobjectのみ許可されます")
	}
	for key := range raw {
		if key != "env" {
			return override, fmt.Errorf("top-level key %qは許可されません (envのみ)", key)
		}
	}

	envValue, hasEnv := raw["env"]
	if !hasEnv {
		return override, nil
	}
	if envValue == nil {
		return override, fmt.Errorf("override env: nullは許可されません (objectまたは空object)")
	}
	entries, ok := envValue.(map[string]any)
	if !ok {
		return override, fmt.Errorf("override env: objectのみ許可されます")
	}

	override.sets = make(map[string]string, len(entries))
	for key, value := range entries {
		switch typed := value.(type) {
		case string:
			override.sets[key] = typed
		case nil:
			override.deletes = append(override.deletes, key)
		default:
			return override, fmt.Errorf("override env %qはstringかnullのみ許可されます", key)
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

const overrideStateFile = ".codex-config-claude-env-state.json"

const overrideStateVersion = 1

type overrideState struct {
	Version int                    `json:"version"`
	Env     map[string]envBaseline `json:"env"`
}

type envBaseline struct {
	Exists bool `json:"exists"`
	Value  any  `json:"value,omitempty"`
}

func statePathFor(targetPath string) string {
	return filepath.Join(filepath.Dir(targetPath), overrideStateFile)
}

func loadOverrideState(path string) (overrideState, error) {
	empty := overrideState{Version: overrideStateVersion, Env: map[string]envBaseline{}}

	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return empty, nil
		}
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

func commitTransaction(plans []plannedWrite, writeFn writeFileFunc) error {
	restores := make([]plannedRestore, 0, len(plans))
	seen := make(map[string]bool, len(plans))
	for _, plan := range plans {
		if seen[plan.path] {
			continue
		}
		seen[plan.path] = true
		restores = append(restores, plannedRestore{path: plan.path, restore: captureFileRestore(plan.path)})
	}

	for _, plan := range plans {
		if err := writeFn(plan.path, plan.data, plan.mode); err != nil {
			if rollbackErr := rollbackFiles(restores, writeFn); rollbackErr != nil {
				return fmt.Errorf("%w (rollback失敗: %v)", err, rollbackErr)
			}
			return err
		}
	}
	return nil
}

func captureFileRestore(path string) fileRestore {
	data, err := os.ReadFile(path)
	if err != nil {
		return fileRestore{existed: false}
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
		return envBaseline{Exists: false}
	}
	return envBaseline{Exists: true, Value: value}
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
