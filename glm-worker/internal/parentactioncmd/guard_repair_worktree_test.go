package parentactioncmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyGuardRepairChangesRollsBackPartialFailure(t *testing.T) {
	repo := t.TempDir()
	worktree := t.TempDir()
	first := "glm-worker/internal/workflow/guard_recovery.go"
	second := "glm-worker/internal/workflow/guard_recovery_test.go"
	writeGuardRepairTestFile(t, repo, first, "original source\n")
	writeGuardRepairTestFile(t, repo, second, "original test\n")
	writeGuardRepairTestFile(t, worktree, first, "repaired source\n")

	if _, err := copyGuardRepairChangesWithRollback(worktree, repo, []string{first, second}); err == nil {
		t.Fatal("partial repair copy unexpectedly succeeded")
	}
	assertGuardRepairTestFile(t, repo, first, "original source\n")
	assertGuardRepairTestFile(t, repo, second, "original test\n")
}

func TestCopyGuardRepairChangesRejectsOriginalSymlink(t *testing.T) {
	repo := t.TempDir()
	worktree := t.TempDir()
	path := "glm-worker/internal/workflow/guard_recovery.go"
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(repo, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, full); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	writeGuardRepairTestFile(t, worktree, path, "repaired\n")

	if _, err := copyGuardRepairChangesWithRollback(worktree, repo, []string{path}); err == nil {
		t.Fatal("symlink repair destination unexpectedly accepted")
	}
	assertGuardRepairTestFile(t, filepath.Dir(outside), filepath.Base(outside), "outside\n")
}

func writeGuardRepairTestFile(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertGuardRepairTestFile(t *testing.T, root, path, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("file content = %q want %q", data, want)
	}
}
