package settingsmerge

import (
	"bytes"
	"encoding/json"
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

type preparedMerge struct {
	target            map[string]any
	targetMode        os.FileMode
	fragment          map[string]any
	override          claudeoverride.EnvOverride
	previousOverride  overrideState
	previousManaged   managedState
	overrideStatePath string
	managedStatePath  string
	inputs            map[string]fileRestore
}

const overrideStateFile = ".codex-config-claude-env-state.json"
const overrideStateVersion = 1

func MergeFiles(targetPath, fragmentPath, overridePath string) (bool, error) {
	return mergeFilesWithWriter(targetPath, fragmentPath, overridePath, writeAtomic)
}

func mergeFilesWithWriter(targetPath, fragmentPath, overridePath string, writeFn writeFileFunc) (bool, error) {
	lock, err := acquireSettingsMergeLock(targetPath)
	if err != nil {
		return false, err
	}
	changed, mergeErr := mergeFilesUnlocked(targetPath, fragmentPath, overridePath, writeFn)
	return changed, joinSettingsMergeLockError(mergeErr, lock.Close())
}

func mergeFilesUnlocked(targetPath, fragmentPath, overridePath string, writeFn writeFileFunc) (bool, error) {
	if err := recoverSettingsTransaction(targetPath, writeFn); err != nil {
		return false, err
	}
	prepared, err := prepareMergeInputs(targetPath, fragmentPath, overridePath)
	if err != nil {
		return false, err
	}
	before := cloneMap(prepared.target)
	restoreEnvBaselines(prepared.target, prepared.previousOverride)
	nextManaged, err := reconcileManagedValues(prepared.target, prepared.previousManaged, prepared.fragment)
	if err != nil {
		return false, err
	}
	nextOverride := snapshotEnvBaselines(prepared.target, prepared.override)
	applyEnvPatch(prepared.target, prepared.override)
	plans, targetChanged, err := planWrites(
		targetPath,
		prepared.overrideStatePath,
		prepared.managedStatePath,
		prepared.targetMode,
		prepared.target,
		nextOverride,
		nextManaged,
		before,
		prepared.previousOverride,
		prepared.previousManaged,
	)
	if err != nil {
		return false, err
	}
	if len(plans) == 0 {
		return false, nil
	}
	if err := commitRecoverableTransaction(targetPath, plans, prepared.inputs, writeFn); err != nil {
		return false, err
	}
	return targetChanged, nil
}

func prepareMergeInputs(targetPath, fragmentPath, overridePath string) (preparedMerge, error) {
	targetSnapshot, err := captureMergeTransactionFile(targetPath)
	if err != nil {
		return preparedMerge{}, fmt.Errorf("target JSON: %w", err)
	}
	target, targetMode, err := objectFromSnapshot(targetSnapshot)
	if err != nil {
		return preparedMerge{}, fmt.Errorf("target JSON: %w", err)
	}
	fragment, _, err := readObject(fragmentPath)
	if err != nil {
		return preparedMerge{}, fmt.Errorf("fragment JSON: %w", err)
	}
	override, err := claudeoverride.Load(overridePath)
	if err != nil {
		return preparedMerge{}, fmt.Errorf("env override: %w", err)
	}
	overrideStatePath := statePathFor(targetPath)
	overrideSnapshot, err := captureMergeTransactionFile(overrideStatePath)
	if err != nil {
		return preparedMerge{}, fmt.Errorf("env override state: %w", err)
	}
	previousOverride, err := overrideStateFromSnapshot(overrideSnapshot)
	if err != nil {
		return preparedMerge{}, fmt.Errorf("env override state: %w", err)
	}
	managedStatePath := ManagedStatePath(targetPath)
	managedSnapshot, err := captureMergeTransactionFile(managedStatePath)
	if err != nil {
		return preparedMerge{}, fmt.Errorf("managed state: %w", err)
	}
	previousManaged, err := managedStateFromSnapshot(managedSnapshot)
	if err != nil {
		return preparedMerge{}, fmt.Errorf("managed state: %w", err)
	}
	return preparedMerge{
		target:            target,
		targetMode:        targetMode,
		fragment:          fragment,
		override:          override,
		previousOverride:  previousOverride,
		previousManaged:   previousManaged,
		overrideStatePath: overrideStatePath,
		managedStatePath:  managedStatePath,
		inputs: map[string]fileRestore{
			targetPath:        targetSnapshot,
			overrideStatePath: overrideSnapshot,
			managedStatePath:  managedSnapshot,
		},
	}, nil
}

func objectFromSnapshot(snapshot fileRestore) (map[string]any, os.FileMode, error) {
	if !snapshot.existed {
		return map[string]any{}, 0o600, nil
	}
	object, err := decodeObjectBytes(snapshot.data)
	if err != nil {
		return nil, 0, err
	}
	return object, snapshot.mode.Perm(), nil
}

func overrideStateFromSnapshot(snapshot fileRestore) (overrideState, error) {
	empty := overrideState{Version: overrideStateVersion, Env: map[string]envBaseline{}}
	if !snapshot.existed {
		return empty, nil
	}
	var state overrideState
	if err := decodeSingleJSON(snapshot.data, &state); err != nil {
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

func managedStateFromSnapshot(snapshot fileRestore) (managedState, error) {
	empty := managedState{Version: managedStateVersion, Values: []managedValueState{}}
	if !snapshot.existed {
		return empty, nil
	}
	var state managedState
	if err := decodeSingleJSON(snapshot.data, &state); err != nil {
		return managedState{}, fmt.Errorf("state JSON: %w", err)
	}
	if state.Version != managedStateVersion {
		return managedState{}, fmt.Errorf("managed state version %d is unsupported (expected %d)", state.Version, managedStateVersion)
	}
	if state.Values == nil {
		state.Values = []managedValueState{}
	}
	if err := validateManagedState(state); err != nil {
		return managedState{}, err
	}
	return state, nil
}

func decodeObjectBytes(data []byte) (map[string]any, error) {
	var object map[string]any
	if err := decodeSingleJSON(data, &object); err != nil {
		return nil, err
	}
	if object == nil {
		return nil, fmt.Errorf("top-level value must be an object")
	}
	return object, nil
}

func decodeSingleJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
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
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, 0, err
	}
	object, err := decodeObjectBytes(data)
	if err != nil {
		return nil, 0, err
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
