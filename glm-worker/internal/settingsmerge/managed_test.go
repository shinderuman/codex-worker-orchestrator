package settingsmerge

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedSettingsRestoreAbsentBaselineOnRetire(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	fragment := filepath.Join(dir, "managed.json")
	writeTestFile(t, target, `{"other":"keep"}`)
	writeTestFile(t, fragment, `{"env":{"MANAGED":"yes"}}`)

	if _, err := MergeFiles(target, fragment, ""); err != nil {
		t.Fatal(err)
	}
	assertContains(t, target, `"MANAGED": "yes"`, `"other": "keep"`)
	if _, err := os.Stat(ManagedStatePath(target)); err != nil {
		t.Fatalf("managed state missing: %v", err)
	}

	writeTestFile(t, fragment, `{}`)
	if _, err := MergeFiles(target, fragment, ""); err != nil {
		t.Fatal(err)
	}
	assertNotContains(t, target, `"MANAGED"`)
	assertNotContains(t, target, `"env"`)
	assertContains(t, target, `"other": "keep"`)
}

func TestManagedSettingsRetireSiblingKeysRemovesToolCreatedParent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	fragment := filepath.Join(dir, "managed.json")
	writeTestFile(t, target, `{"other":"keep"}`)
	writeTestFile(t, fragment, `{"env":{"A":"one","B":"two"}}`)

	if _, err := MergeFiles(target, fragment, ""); err != nil {
		t.Fatal(err)
	}
	state, err := loadManagedState(ManagedStatePath(target))
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Values) != 2 {
		t.Fatalf("managed values=%d, want 2", len(state.Values))
	}
	for _, record := range state.Values {
		if record.Baseline.Exists || record.Baseline.FirstMissingPrefix != 1 {
			t.Fatalf("sibling baseline at %v = %+v, want missing parent baseline", record.Path, record.Baseline)
		}
	}

	writeTestFile(t, fragment, `{}`)
	if _, err := MergeFiles(target, fragment, ""); err != nil {
		t.Fatal(err)
	}
	assertJSONMissing(t, target, []string{"env"})
	assertJSONValue(t, target, []string{"other"}, "keep")
}

func TestManagedSettingsRestorePreexistingValueOnRetire(t *testing.T) {
	for _, original := range []string{"user", "managed"} {
		t.Run(original, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "settings.json")
			fragment := filepath.Join(dir, "managed.json")
			writeTestFile(t, target, `{"env":{"KEY":"`+original+`","LOCAL":"keep"},"other":true}`)
			writeTestFile(t, fragment, `{"env":{"KEY":"managed"}}`)
			if _, err := MergeFiles(target, fragment, ""); err != nil {
				t.Fatal(err)
			}
			assertJSONValue(t, target, []string{"env", "KEY"}, "managed")
			assertJSONValue(t, target, []string{"env", "LOCAL"}, "keep")
			assertJSONValue(t, target, []string{"other"}, true)

			writeTestFile(t, fragment, `{}`)
			if _, err := MergeFiles(target, fragment, ""); err != nil {
				t.Fatal(err)
			}
			assertJSONValue(t, target, []string{"env", "KEY"}, original)
			assertJSONValue(t, target, []string{"env", "LOCAL"}, "keep")
			assertJSONValue(t, target, []string{"other"}, true)
		})
	}
}

func TestManagedSettingsUpgradeKeepsOriginalBaseline(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	fragment := filepath.Join(dir, "managed.json")
	writeTestFile(t, target, `{"env":{"KEY":"user"}}`)
	writeTestFile(t, fragment, `{"env":{"KEY":"one"}}`)
	if _, err := MergeFiles(target, fragment, ""); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, fragment, `{"env":{"KEY":"two"}}`)
	if _, err := MergeFiles(target, fragment, ""); err != nil {
		t.Fatal(err)
	}
	assertJSONValue(t, target, []string{"env", "KEY"}, "two")

	writeTestFile(t, fragment, `{}`)
	if _, err := MergeFiles(target, fragment, ""); err != nil {
		t.Fatal(err)
	}
	assertJSONValue(t, target, []string{"env", "KEY"}, "user")
}

func TestManagedSettingsRefuseOverwriteAfterUserModification(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	fragment := filepath.Join(dir, "managed.json")
	writeTestFile(t, target, `{"env":{"KEY":"user"}}`)
	writeTestFile(t, fragment, `{"env":{"KEY":"one"}}`)
	if _, err := MergeFiles(target, fragment, ""); err != nil {
		t.Fatal(err)
	}
	stateBefore := append([]byte(nil), readTestFile(t, ManagedStatePath(target))...)
	writeTestFile(t, target, `{"env":{"KEY":"manual","LOCAL":"keep"}}`)
	before := append([]byte(nil), readTestFile(t, target)...)
	writeTestFile(t, fragment, `{"env":{"KEY":"two"}}`)

	if _, err := MergeFiles(target, fragment, ""); err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("expected ownership conflict, got %v", err)
	}
	if !bytes.Equal(before, readTestFile(t, target)) {
		t.Fatal("user-modified target changed on ownership conflict")
	}
	if !bytes.Equal(stateBefore, readTestFile(t, ManagedStatePath(target))) {
		t.Fatal("managed state changed on ownership conflict")
	}
}

func TestManagedSettingsRetirePreservesUserModifiedValue(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	fragment := filepath.Join(dir, "managed.json")
	writeTestFile(t, target, `{"env":{"KEY":"user"}}`)
	writeTestFile(t, fragment, `{"env":{"KEY":"managed"}}`)
	if _, err := MergeFiles(target, fragment, ""); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, target, `{"env":{"KEY":"manual","LOCAL":"keep"}}`)
	writeTestFile(t, fragment, `{}`)

	if _, err := MergeFiles(target, fragment, ""); err != nil {
		t.Fatal(err)
	}
	assertJSONValue(t, target, []string{"env", "KEY"}, "manual")
	assertJSONValue(t, target, []string{"env", "LOCAL"}, "keep")
	state, err := loadManagedState(ManagedStatePath(target))
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Values) != 0 {
		t.Fatalf("retired user-modified value remained tool-owned: %+v", state.Values)
	}
}

func TestManagedSettingsEmptyFragmentRestoresAllManagedBaselines(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	fragment := filepath.Join(dir, "managed.json")
	writeTestFile(t, target, `{"env":{"KEEP":"user"},"other":"keep"}`)
	writeTestFile(t, fragment, `{"env":{"KEEP":"managed","NEW":"managed"},"top":"managed"}`)
	if _, err := MergeFiles(target, fragment, ""); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, fragment, `{}`)
	if _, err := MergeFiles(target, fragment, ""); err != nil {
		t.Fatal(err)
	}
	assertJSONValue(t, target, []string{"env", "KEEP"}, "user")
	assertJSONValue(t, target, []string{"other"}, "keep")
	assertJSONMissing(t, target, []string{"env", "NEW"})
	assertJSONMissing(t, target, []string{"top"})
	state, err := loadManagedState(ManagedStatePath(target))
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Values) != 0 {
		t.Fatalf("empty managed fragment retained ownership: %+v", state.Values)
	}
}

func TestManagedSettingsRollbackTargetAndOwnershipState(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	fragment := filepath.Join(dir, "managed.json")
	writeTestFile(t, target, `{"env":{"LOCAL":"keep"}}`)
	writeTestFile(t, fragment, `{"env":{"MANAGED":"yes"}}`)
	before := append([]byte(nil), readTestFile(t, target)...)
	managedPath := ManagedStatePath(target)
	writer := func(path string, data []byte, mode os.FileMode) error {
		if path == managedPath {
			return errors.New("injected managed state failure")
		}
		return writeAtomic(path, data, mode)
	}

	if _, err := mergeFilesWithWriter(target, fragment, "", writer); err == nil {
		t.Fatal("expected error")
	}
	if !bytes.Equal(before, readTestFile(t, target)) {
		t.Fatal("target was not rolled back")
	}
	if _, err := os.Stat(managedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed state exists after rollback: %v", err)
	}
}

func TestVerifyManagedInstallationRequiresOwnershipState(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	fragment := filepath.Join(dir, "managed.json")
	writeTestFile(t, target, `{"env":{"KEY":"managed"}}`)
	writeTestFile(t, fragment, `{"env":{"KEY":"managed"}}`)
	if err := VerifyManagedInstallation(target, fragment, ""); err == nil {
		t.Fatal("managed values without ownership state were accepted")
	}
	if _, err := MergeFiles(target, fragment, ""); err != nil {
		t.Fatal(err)
	}
	if err := VerifyManagedInstallation(target, fragment, ""); err != nil {
		t.Fatalf("valid managed ownership rejected: %v", err)
	}
}

func assertJSONValue(t *testing.T, path string, keys []string, want any) {
	t.Helper()
	value, exists := jsonValueAt(t, path, keys)
	if !exists || value != want {
		t.Fatalf("%s value at %v = %#v, exists=%v, want %#v", path, keys, value, exists, want)
	}
}

func assertJSONMissing(t *testing.T, path string, keys []string) {
	t.Helper()
	if value, exists := jsonValueAt(t, path, keys); exists {
		t.Fatalf("%s unexpectedly contains %v = %#v", path, keys, value)
	}
}

func jsonValueAt(t *testing.T, path string, keys []string) (any, bool) {
	t.Helper()
	var current any
	if err := json.Unmarshal(readTestFile(t, path), &current); err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, exists := object[key]
		if !exists {
			return nil, false
		}
		current = value
	}
	return current, true
}
