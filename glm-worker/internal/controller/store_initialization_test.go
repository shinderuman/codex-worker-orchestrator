package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitializeControllerStoreDoesNotReplaceAmbiguousExistingTarget(t *testing.T) {
	parent := t.TempDir()
	store := &Store{
		dir:      filepath.Join(parent, "controller"),
		identity: RepositoryIdentity{LineageID: "lineage-test"},
	}
	if err := os.Mkdir(store.dir, 0o700); err != nil {
		t.Fatal(err)
	}

	err := initializeControllerStore(store)
	if err == nil || !strings.Contains(err.Error(), "incomplete or ambiguous") {
		t.Fatalf("ambiguous existing controller target was not rejected: %v", err)
	}
	entries, readErr := os.ReadDir(store.dir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("ambiguous controller target was overwritten: %#v", entries)
	}
	if _, statErr := os.Stat(store.headPath()); !os.IsNotExist(statErr) {
		t.Fatalf("ambiguous target gained controller authority: %v", statErr)
	}
}
