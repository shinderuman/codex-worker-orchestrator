package settingsmerge

import (
	"bytes"
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
			assertContains(t, target, `"KEY": "managed"`, `"LOCAL": "keep"`, `"other": true`)

			writeTestFile(t, fragment, `{}`)
			if _, err := MergeFiles(target, fragment, ""); err != nil {
				t.Fatal(err)
			}
			assertContains(t, target, `"KEY": "`+original+`"`, `"LOCAL": "keep"`, `"other": true`)
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
	assertContains(t, target, `"KEY": "two"`)

	writeTestFile(t, fragment, `{}`)
	if _, err := MergeFiles(target, fragment, ""); err != nil {
		t.Fatal(err)
	}
	assertContains(t, target, `"KEY": "user"`)
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
	assertContains(t, target, `"KEY": "manual"`, `"LOCAL": "keep"`)
	state, err := loadManagedState(ManagedStatePath(target))
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Values) != 0 {
		t.Fatalf("retired user-modified value remained tool-owned: %+v", state.Values)
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
