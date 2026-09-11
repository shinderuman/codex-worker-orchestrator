package settingsmerge

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/claudeoverride"
)

type managedState struct {
	Version int                 `json:"version"`
	Values  []managedValueState `json:"values"`
}

type managedValueState struct {
	Path     []string      `json:"path"`
	Baseline valueBaseline `json:"baseline"`
	Applied  any           `json:"applied"`
}

type valueBaseline struct {
	Exists             bool `json:"exists"`
	Value              any  `json:"value,omitempty"`
	FirstMissingPrefix int  `json:"first_missing_prefix,omitempty"`
}

type managedLeaf struct {
	Path  []string
	Value any
}

const managedStateVersion = 1
const managedStateDir = ".codex-worker-orchestrator"
const managedStateSuffix = ".managed-settings-state.json"

func ManagedStatePath(targetPath string) string {
	return filepath.Join(filepath.Dir(targetPath), managedStateDir, filepath.Base(targetPath)+managedStateSuffix)
}

func loadManagedState(path string) (managedState, error) {
	empty := managedState{Version: managedStateVersion, Values: []managedValueState{}}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return managedState{}, err
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(file)
	decoder.UseNumber()
	var state managedState
	if err := decoder.Decode(&state); err != nil {
		return managedState{}, fmt.Errorf("state JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return managedState{}, fmt.Errorf("state: multiple JSON values")
		}
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

func validateManagedState(state managedState) error {
	seen := make(map[string]bool, len(state.Values))
	for _, record := range state.Values {
		if len(record.Path) == 0 {
			return fmt.Errorf("managed state contains an empty path")
		}
		if !record.Baseline.Exists && (record.Baseline.FirstMissingPrefix < 1 || record.Baseline.FirstMissingPrefix > len(record.Path)) {
			return fmt.Errorf("managed state contains invalid baseline at %s", managedPathDisplay(record.Path))
		}
		if record.Baseline.Exists && record.Baseline.FirstMissingPrefix != 0 {
			return fmt.Errorf("managed state contains inconsistent baseline at %s", managedPathDisplay(record.Path))
		}
		key := managedPathKey(record.Path)
		if seen[key] {
			return fmt.Errorf("managed state contains duplicate path %s", managedPathDisplay(record.Path))
		}
		seen[key] = true
	}
	return nil
}

func reconcileManagedValues(target map[string]any, previous managedState, fragment map[string]any) (managedState, error) {
	desired := flattenManagedObject(fragment)
	desiredByPath := make(map[string]managedLeaf, len(desired))
	for _, leaf := range desired {
		desiredByPath[managedPathKey(leaf.Path)] = leaf
	}
	previousByPath := make(map[string]managedValueState, len(previous.Values))
	for _, record := range previous.Values {
		previousByPath[managedPathKey(record.Path)] = record
	}

	for _, record := range previous.Values {
		key := managedPathKey(record.Path)
		_, remainsManaged := desiredByPath[key]
		current, exists, err := managedValueAt(target, record.Path)
		if err != nil {
			return managedState{}, err
		}
		unchanged := exists && reflect.DeepEqual(current, record.Applied)
		if remainsManaged && !unchanged {
			return managedState{}, fmt.Errorf("managed Claude setting was modified after install; refusing to overwrite: %s", managedPathDisplay(record.Path))
		}
		if !unchanged {
			continue
		}
		if err := restoreManagedBaseline(target, record.Path, record.Baseline); err != nil {
			return managedState{}, err
		}
	}

	next := managedState{Version: managedStateVersion, Values: make([]managedValueState, 0, len(desired))}
	for _, leaf := range desired {
		key := managedPathKey(leaf.Path)
		baseline := valueBaseline{}
		if previousRecord, ok := previousByPath[key]; ok {
			baseline = previousRecord.Baseline
		} else {
			var err error
			baseline, err = captureManagedBaseline(target, leaf.Path)
			if err != nil {
				return managedState{}, err
			}
		}
		if err := setManagedValue(target, leaf.Path, cloneJSONValue(leaf.Value)); err != nil {
			return managedState{}, err
		}
		next.Values = append(next.Values, managedValueState{
			Path:     append([]string(nil), leaf.Path...),
			Baseline: baseline,
			Applied:  cloneJSONValue(leaf.Value),
		})
	}
	return next, nil
}

func flattenManagedObject(fragment map[string]any) []managedLeaf {
	var leaves []managedLeaf
	for _, key := range sortedMapKeys(fragment) {
		flattenManagedValue([]string{key}, fragment[key], &leaves)
	}
	return leaves
}

func flattenManagedValue(path []string, value any, leaves *[]managedLeaf) {
	object, ok := value.(map[string]any)
	if !ok || len(object) == 0 {
		*leaves = append(*leaves, managedLeaf{Path: append([]string(nil), path...), Value: value})
		return
	}
	for _, key := range sortedMapKeys(object) {
		flattenManagedValue(append(path, key), object[key], leaves)
	}
}

func sortedMapKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func captureManagedBaseline(target map[string]any, path []string) (valueBaseline, error) {
	current := any(target)
	for index, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return valueBaseline{}, fmt.Errorf("cannot manage Claude setting below non-object path %s", managedPathDisplay(path[:index]))
		}
		value, exists := object[key]
		if !exists {
			return valueBaseline{FirstMissingPrefix: index + 1}, nil
		}
		current = value
	}
	return valueBaseline{Exists: true, Value: cloneJSONValue(current)}, nil
}

func managedValueAt(target map[string]any, path []string) (any, bool, error) {
	current := any(target)
	for index, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("managed Claude setting parent is not an object: %s", managedPathDisplay(path[:index]))
		}
		value, exists := object[key]
		if !exists {
			return nil, false, nil
		}
		current = value
	}
	return current, true, nil
}

func setManagedValue(target map[string]any, path []string, value any) error {
	object := target
	for index, key := range path[:len(path)-1] {
		child, exists := object[key]
		if !exists {
			next := map[string]any{}
			object[key] = next
			object = next
			continue
		}
		next, ok := child.(map[string]any)
		if !ok {
			return fmt.Errorf("cannot manage Claude setting below non-object path %s", managedPathDisplay(path[:index+1]))
		}
		object = next
	}
	object[path[len(path)-1]] = value
	return nil
}

func restoreManagedBaseline(target map[string]any, path []string, baseline valueBaseline) error {
	if baseline.Exists {
		return setManagedValue(target, path, cloneJSONValue(baseline.Value))
	}
	if err := deleteManagedValue(target, path); err != nil {
		return err
	}
	for depth := len(path) - 1; depth >= baseline.FirstMissingPrefix && baseline.FirstMissingPrefix > 0; depth-- {
		prefix := path[:depth]
		value, exists, err := managedValueAt(target, prefix)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		object, ok := value.(map[string]any)
		if !ok || len(object) != 0 {
			break
		}
		if err := deleteManagedValue(target, prefix); err != nil {
			return err
		}
	}
	return nil
}

func deleteManagedValue(target map[string]any, path []string) error {
	object := target
	for index, key := range path[:len(path)-1] {
		child, exists := object[key]
		if !exists {
			return nil
		}
		next, ok := child.(map[string]any)
		if !ok {
			return fmt.Errorf("managed Claude setting parent is not an object: %s", managedPathDisplay(path[:index+1]))
		}
		object = next
	}
	delete(object, path[len(path)-1])
	return nil
}

func managedPathKey(path []string) string {
	data, _ := json.Marshal(path)
	return string(data)
}

func managedPathDisplay(path []string) string {
	if len(path) == 0 {
		return "<root>"
	}
	data, _ := json.Marshal(path)
	return string(data)
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		clone := make([]any, len(typed))
		for index, item := range typed {
			clone[index] = cloneJSONValue(item)
		}
		return clone
	default:
		return typed
	}
}

func VerifyManagedInstallation(targetPath, fragmentPath, overridePath string) error {
	target, _, err := readObject(targetPath)
	if err != nil {
		return fmt.Errorf("target JSON: %w", err)
	}
	fragment, _, err := readObject(fragmentPath)
	if err != nil {
		return fmt.Errorf("fragment JSON: %w", err)
	}
	state, err := loadManagedState(ManagedStatePath(targetPath))
	if err != nil {
		return fmt.Errorf("managed state: %w", err)
	}
	override, err := claudeoverride.Load(overridePath)
	if err != nil {
		return fmt.Errorf("env override: %w", err)
	}
	return verifyManagedInstallation(target, fragment, state, override)
}

func verifyManagedInstallation(target, fragment map[string]any, state managedState, override claudeoverride.EnvOverride) error {
	desired := flattenManagedObject(fragment)
	if len(state.Values) != len(desired) {
		return fmt.Errorf("managed state path count=%d, expected=%d", len(state.Values), len(desired))
	}
	stateByPath := make(map[string]managedValueState, len(state.Values))
	for _, record := range state.Values {
		stateByPath[managedPathKey(record.Path)] = record
	}
	deleted := make(map[string]bool, len(override.Deletes))
	for _, key := range override.Deletes {
		deleted[key] = true
	}
	for _, leaf := range desired {
		record, ok := stateByPath[managedPathKey(leaf.Path)]
		if !ok {
			return fmt.Errorf("managed state is missing path %s", managedPathDisplay(leaf.Path))
		}
		if !reflect.DeepEqual(record.Applied, leaf.Value) {
			return fmt.Errorf("managed state applied value mismatch at %s", managedPathDisplay(leaf.Path))
		}
		expected := leaf.Value
		expectedExists := true
		if len(leaf.Path) == 2 && leaf.Path[0] == "env" {
			key := leaf.Path[1]
			if deleted[key] {
				expectedExists = false
			} else if value, overridden := override.Sets[key]; overridden {
				expected = value
			}
		}
		actual, exists, err := managedValueAt(target, leaf.Path)
		if err != nil {
			return err
		}
		if exists != expectedExists || exists && !reflect.DeepEqual(actual, expected) {
			return fmt.Errorf("installed managed Claude setting mismatch at %s", managedPathDisplay(leaf.Path))
		}
	}
	return nil
}
