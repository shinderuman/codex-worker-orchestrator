package settingsmerge

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeFilesPreservesAndMerges(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	fragment := filepath.Join(dir, "managed.json")
	writeTestFile(t, target, `{"permissions":{"allow":["x"]},"env":{"LOCAL":"keep"}}`)
	writeTestFile(t, fragment, `{"env":{"MANAGED":"yes"}}`)
	changed, err := MergeFiles(target, fragment, "")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	text := string(readTestFile(t, target))
	for _, want := range []string{`"permissions"`, `"LOCAL": "keep"`, `"MANAGED": "yes"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in %s", want, text)
		}
	}
	changed, err = MergeFiles(target, fragment, "")
	if err != nil || changed {
		t.Fatalf("second changed=%v err=%v", changed, err)
	}
}

func TestMergeFilesOverrideLifecycle(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	fragment := filepath.Join(dir, "managed.json")
	override := filepath.Join(dir, "override.json")
	writeTestFile(t, target, `{"env":{"LOCAL":"original"}}`)
	writeTestFile(t, fragment, `{"env":{"MANAGED":"yes","BASE":"managed"}}`)
	writeTestFile(t, override, `{"env":{"LOCAL":"override","BASE":null,"EXTRA":"set"}}`)
	if _, err := MergeFiles(target, fragment, override); err != nil {
		t.Fatal(err)
	}
	assertContains(t, target, `"LOCAL": "override"`, `"EXTRA": "set"`)
	assertNotContains(t, target, `"BASE"`)
	writeTestFile(t, override, `{}`)
	if _, err := MergeFiles(target, fragment, override); err != nil {
		t.Fatal(err)
	}
	assertContains(t, target, `"LOCAL": "original"`, `"BASE": "managed"`, `"MANAGED": "yes"`)
	assertNotContains(t, target, `"EXTRA"`)
}

func TestMergeFilesRejectsInvalidOverrideWithoutWriting(t *testing.T) {
	cases := []string{`null`, `[]`, `{"other":1}`, `{"env":null}`, `{"env":{"K":1}}`}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "settings.json")
			fragment := filepath.Join(dir, "managed.json")
			override := filepath.Join(dir, "override.json")
			writeTestFile(t, target, `{"env":{"LOCAL":"keep"}}`)
			writeTestFile(t, fragment, `{"env":{"MANAGED":"yes"}}`)
			writeTestFile(t, override, input)
			before := readTestFile(t, target)
			if _, err := MergeFiles(target, fragment, override); err == nil {
				t.Fatal("expected error")
			}
			if !bytes.Equal(before, readTestFile(t, target)) {
				t.Fatal("target changed on invalid override")
			}
		})
	}
}

func TestMergeFilesPreservesModes(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	fragment := filepath.Join(dir, "managed.json")
	override := filepath.Join(dir, "override.json")
	writeTestFile(t, target, `{}`)
	if err := os.Chmod(target, 0o640); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, fragment, `{}`)
	writeTestFile(t, override, `{"env":{"EXTRA":"set"}}`)
	if _, err := MergeFiles(target, fragment, override); err != nil {
		t.Fatal(err)
	}
	if mode := fileMode(t, target); mode != 0o640 {
		t.Fatalf("target mode=%o", mode)
	}
	if mode := fileMode(t, statePathFor(target)); mode != 0o600 {
		t.Fatalf("state mode=%o", mode)
	}
}

func TestMergeFilesRollsBackPairedWrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	fragment := filepath.Join(dir, "managed.json")
	override := filepath.Join(dir, "override.json")
	writeTestFile(t, target, `{"env":{"LOCAL":"keep"}}`)
	writeTestFile(t, fragment, `{"env":{"MANAGED":"yes"}}`)
	writeTestFile(t, override, `{"env":{"EXTRA":"set"}}`)
	before := append([]byte(nil), readTestFile(t, target)...)
	statePath := statePathFor(target)
	writer := func(path string, data []byte, mode os.FileMode) error {
		if path == statePath {
			return errors.New("injected")
		}
		return writeAtomic(path, data, mode)
	}
	if _, err := mergeFilesWithWriter(target, fragment, override, writer); err == nil {
		t.Fatal("expected error")
	}
	if !bytes.Equal(before, readTestFile(t, target)) {
		t.Fatal("target was not rolled back")
	}
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state exists after rollback: %v", err)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func assertContains(t *testing.T, path string, values ...string) {
	t.Helper()
	text := string(readTestFile(t, path))
	for _, value := range values {
		if !strings.Contains(text, value) {
			t.Fatalf("missing %s in %s", value, text)
		}
	}
}

func assertNotContains(t *testing.T, path string, value string) {
	t.Helper()
	if text := string(readTestFile(t, path)); strings.Contains(text, value) {
		t.Fatalf("unexpected %s in %s", value, text)
	}
}
