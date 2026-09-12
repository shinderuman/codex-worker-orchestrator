package settingsmerge

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMergeFilesRecoversInterruptedTargetWriteBeforeCapturingBaseline(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "settings.json")
	fragmentPath := filepath.Join(dir, "managed.json")
	writeTestFile(t, targetPath, `{"env":{"KEY":"user"}}`)
	writeTestFile(t, fragmentPath, `{"env":{"KEY":"managed"}}`)

	target, targetMode, err := readObject(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	fragment, _, err := readObject(fragmentPath)
	if err != nil {
		t.Fatal(err)
	}
	before := cloneMap(target)
	nextManaged, err := reconcileManagedValues(target, managedState{Version: managedStateVersion, Values: []managedValueState{}}, fragment)
	if err != nil {
		t.Fatal(err)
	}
	plans, _, err := planWrites(
		targetPath,
		statePathFor(targetPath),
		ManagedStatePath(targetPath),
		targetMode,
		target,
		overrideState{Version: overrideStateVersion, Env: map[string]envBaseline{}},
		nextManaged,
		before,
		overrideState{Version: overrideStateVersion, Env: map[string]envBaseline{}},
		managedState{Version: managedStateVersion, Values: []managedValueState{}},
	)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := newMergeTransactionJournal(plans)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveMergeTransactionJournal(mergeTransactionPath(targetPath), journal); err != nil {
		t.Fatal(err)
	}
	if len(plans) < 2 || plans[0].path != targetPath {
		t.Fatalf("unexpected write plan: %#v", plans)
	}
	if err := writeAtomic(plans[0].path, plans[0].data, plans[0].mode); err != nil {
		t.Fatal(err)
	}
	assertJSONValue(t, targetPath, []string{"env", "KEY"}, "managed")
	if _, err := os.Stat(ManagedStatePath(targetPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed state unexpectedly written before simulated crash: %v", err)
	}

	if _, err := MergeFiles(targetPath, fragmentPath, ""); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, fragmentPath, `{}`)
	if _, err := MergeFiles(targetPath, fragmentPath, ""); err != nil {
		t.Fatal(err)
	}
	assertJSONValue(t, targetPath, []string{"env", "KEY"}, "user")
}

func TestMergeFilesRejectsUserEditAfterInterruptedTransaction(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "settings.json")
	writeTestFile(t, targetPath, `{"value":"before"}`)
	plan := plannedWrite{path: targetPath, data: []byte("{\n  \"value\": \"after\"\n}\n"), mode: 0o600}
	journal, err := newMergeTransactionJournal([]plannedWrite{plan})
	if err != nil {
		t.Fatal(err)
	}
	if err := saveMergeTransactionJournal(mergeTransactionPath(targetPath), journal); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, targetPath, `{"value":"user-edit"}`)

	if err := recoverSettingsTransaction(targetPath, writeAtomic); err == nil {
		t.Fatal("user edit after interrupted settings transaction was overwritten")
	}
	assertJSONValue(t, targetPath, []string{"value"}, "user-edit")
}

func TestMergeFilesReportsUnchangedWhenOnlyOwnershipStateChanges(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "settings.json")
	fragmentPath := filepath.Join(dir, "managed.json")
	writeTestFile(t, targetPath, `{"env":{"KEY":"managed"}}`)
	writeTestFile(t, fragmentPath, `{"env":{"KEY":"managed"}}`)

	changed, err := MergeFiles(targetPath, fragmentPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("ownership-state-only merge reported settings target as updated")
	}
	if _, err := os.Stat(ManagedStatePath(targetPath)); err != nil {
		t.Fatalf("ownership state was not persisted: %v", err)
	}
}
