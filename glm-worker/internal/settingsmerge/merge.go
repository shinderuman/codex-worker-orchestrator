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

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/claudeoverride"
)

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

const overrideStateFile = ".codex-config-claude-env-state.json"
const overrideStateVersion = 1

func MergeFiles(targetPath, fragmentPath, overridePath string) (bool, error) {
	return mergeFilesWithWriter(targetPath, fragmentPath, overridePath, writeAtomic)
}

func mergeFilesWithWriter(targetPath, fragmentPath, overridePath string, writeFn writeFileFunc) (bool, error) {
	if err := recoverSettingsTransaction(targetPath, writeFn); err != nil {
		return false, err
	}
	target, targetMode, err := readObjectOrEmpty(targetPath)
	if err != nil {
		return false, fmt.Errorf("target JSON: %w", err)
	}
	fragment, _, err := readObject(fragmentPath)
	if err != nil {
		return false, fmt.Errorf("fragment JSON: %w", err)
	}
	override, err := claudeoverride.Load(overridePath)
	if err != nil {
		return false, fmt.Errorf("env override: %w", err)
	}
	overrideStatePath := statePathFor(targetPath)
	previousOverride, err := loadOverrideState(overrideStatePath)
	if err != nil {
		return false, fmt.Errorf("env override state: %w", err)
	}
	managedStatePath := ManagedStatePath(targetPath)
	previousManaged, err := loadManagedState(managedStatePath)
	if err != nil {
		return false, fmt.Errorf("managed state: %w", err)
	}

	before := cloneMap(target)
	restoreEnvBaselines(target, previousOverride)
	nextManaged, err := reconcileManagedValues(target, previousManaged, fragment)
	if err != nil {
		return false, err
	}
	nextOverride := snapshotEnvBaselines(target, override)
	applyEnvPatch(target, override)
	plans, targetChanged, err := planWrites(
		targetPath,
		overrideStatePath,
		managedStatePath,
		targetMode,
		target,
		nextOverride,
		nextManaged,
		before,
		previousOverride,
		previousManaged,
	)
	if err != nil {
		return false, err
	}
	if len(plans) == 0 {
		return false, nil
	}
	if err := commitRecoverableTransaction(targetPath, plans, writeFn); err != nil {
		return false, err
	}
	return targetChanged, nil
}

func planWrites(
	targetPath string,
	overrideStatePath string,
	managedStatePath string,
	targetMode os.FileMode,
	target map[string]any,
	nextOverride overrideState,
	nextManaged managedState,
	before map[string]any,
	previousOverride overrideState,
	previousManaged managedState,
) ([]plannedWrite, bool, error) {
	targetChanged := !reflect.DeepEqual(before, target)
	overrideStateChanged := !reflect.DeepEqual(nextOverride, previousOverride)
	managedStateChanged := !reflect.DeepEqual(nextManaged, previousManaged)
	plans := make([]plannedWrite, 0, 3)
	if targetChanged {
		data, err := marshalObject(target)
		if err != nil {
			return nil, false, err
		}
		plans = append(plans, plannedWrite{path: targetPath, data: data, mode: targetMode})
	}
	if overrideStateChanged {
		data, err := marshalObject(nextOverride)
		if err != nil {
			return nil, false, err
		}
		plans = append(plans, plannedWrite{path: overrideStatePath, data: data, mode: 0o600})
	}
	if managedStateChanged {
		data, err := marshalObject(nextManaged)
		if err != nil {
			return nil, false, err
		}
		plans = append(plans, plannedWrite{path: managedStatePath, data: data, mode: 0o600})
	}
	return plans, targetChanged, nil
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
	defer func() { _ = file.Close() }()
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

func cloneMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		if child, ok := item.(map[string]any); ok {
			result[key] = cloneMap(child)
			continue
		}
		if child, ok := item.([]any); ok {
			result[key] = cloneJSONValue(child)
			continue
		}
		result[key] = item
	}
	return result
}

func applyEnvPatch(target map[string]any, override claudeoverride.EnvOverride) {
	if len(override.Sets) == 0 && len(override.Deletes) == 0 {
		return
	}
	env := ensureEnvMap(target)
	for _, key := range override.Deletes {
		delete(env, key)
	}
	for key, value := range override.Sets {
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
	defer func() { _ = file.Close() }()
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

func snapshotEnvBaselines(target map[string]any, override claudeoverride.EnvOverride) overrideState {
	state := overrideState{Version: overrideStateVersion, Env: map[string]envBaseline{}}
	if len(override.Sets) == 0 && len(override.Deletes) == 0 {
		return state
	}
	env, _ := target["env"].(map[string]any)
	for key := range override.Sets {
		state.Env[key] = envBaselineOf(env, key)
	}
	for _, key := range override.Deletes {
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
	defer func() { _ = os.Remove(tempPath) }()
	if _, err := bytes.NewReader(data).WriteTo(file); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
