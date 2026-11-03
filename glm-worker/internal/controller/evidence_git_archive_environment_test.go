package controller

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestGitArchiveOperationsFilterInheritedGitLocalEnvironment(t *testing.T) {
	source, _ := newControllerLinkedWorktree(t)
	store := newEvidenceTestStore(t)
	commit := controllerGitOutput(t, source, "rev-parse", "HEAD")
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "unrelated-git-dir"))
	t.Setenv("GIT_WORK_TREE", filepath.Join(t.TempDir(), "unrelated-worktree"))
	t.Setenv("GIT_INDEX_FILE", filepath.Join(t.TempDir(), "unrelated-index"))
	ref, expected, err := store.CaptureGitObjectArchive(source, "isolated-git-environment", []string{commit})
	if err != nil {
		t.Fatalf("source-bound object archive capture retained inherited Git location: %v", err)
	}
	actual, err := store.VerifyGitObjectArchive(ref)
	if err != nil {
		t.Fatalf("bare repository pack verification retained inherited Git location: %v", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("archive content changed by inherited Git environment: actual=%v expected=%v", actual, expected)
	}
	if _, err := os.Stat(filepath.Join(source, ".git")); err != nil {
		t.Fatalf("source repository unexpectedly changed: %v", err)
	}
}
