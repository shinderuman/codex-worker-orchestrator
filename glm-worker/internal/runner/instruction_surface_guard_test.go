package runner

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestInstructionSurfaceSnapshotTracksLocalAndNestedAgents(t *testing.T) {
	root := t.TempDir()
	writeInstructionGuardFile(t, root, "AGENTS.local.md", "local-one")
	writeInstructionGuardFile(t, root, "nested/AGENTS.md", "nested-one")
	writeInstructionGuardFile(t, root, "ordinary.go", "package ordinary")

	before, err := captureInstructionSurfaceSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	writeInstructionGuardFile(t, root, "ordinary.go", "package changed")
	afterSourceEdit, err := captureInstructionSurfaceSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if before.digest != afterSourceEdit.digest {
		t.Fatal("ordinary source edit changed instruction surface identity")
	}

	writeInstructionGuardFile(t, root, "nested/AGENTS.md", "nested-two")
	afterInstructionEdit, err := captureInstructionSurfaceSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if before.digest == afterInstructionEdit.digest {
		t.Fatal("nested AGENTS.md mutation did not change instruction surface identity")
	}
}

func TestInstructionSurfaceGuardRejectsBetweenCallMutation(t *testing.T) {
	root := t.TempDir()
	writeInstructionGuardFile(t, root, "AGENTS.local.md", "accepted")
	r := newInstructionGuardRunner(t, root, "task-one")

	if _, err := r.prepareInstructionSurfaceGuard(); err != nil {
		t.Fatal(err)
	}
	writeInstructionGuardFile(t, root, "AGENTS.local.md", "mutated")
	_, err := r.prepareInstructionSurfaceGuard()
	var guardErr *InstructionSurfaceGuardError
	if !errors.As(err, &guardErr) || guardErr.Stage != "before-call-mismatch" {
		t.Fatalf("between-call mutation error = %#v", err)
	}
}

func TestInstructionSurfaceGuardRestoresCallTimeMutation(t *testing.T) {
	root := t.TempDir()
	writeInstructionGuardFile(t, root, "AGENTS.local.md", "accepted")
	r := newInstructionGuardRunner(t, root, "task-one")

	before, err := r.prepareInstructionSurfaceGuard()
	if err != nil {
		t.Fatal(err)
	}
	writeInstructionGuardFile(t, root, "AGENTS.local.md", "mutated")
	writeInstructionGuardFile(t, root, "new/AGENTS.md", "self-authored")

	err = r.verifyInstructionSurfaceGuard(before)
	var guardErr *InstructionSurfaceGuardError
	if !errors.As(err, &guardErr) || guardErr.Stage != "after-call-mutation" || !guardErr.Restored {
		t.Fatalf("call-time mutation error = %#v", err)
	}
	if got := readInstructionGuardFile(t, root, "AGENTS.local.md"); got != "accepted" {
		t.Fatalf("restored AGENTS.local.md = %q", got)
	}
	if _, err := os.Lstat(filepath.Join(root, "new", "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("self-authored AGENTS.md remains after restore: %v", err)
	}
}

func TestInstructionSurfaceGuardAllowsParentChangeForNewTask(t *testing.T) {
	root := t.TempDir()
	writeInstructionGuardFile(t, root, "AGENTS.local.md", "task-one")
	r := newInstructionGuardRunner(t, root, "task-one")

	if _, err := r.prepareInstructionSurfaceGuard(); err != nil {
		t.Fatal(err)
	}
	writeInstructionGuardFile(t, root, "AGENTS.local.md", "parent-task-two")
	if err := r.state.Write("task.id", "task-two"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.prepareInstructionSurfaceGuard(); err != nil {
		t.Fatalf("new task must accept parent-chosen instruction baseline: %v", err)
	}
	baseline, err := r.state.Read(instructionSurfaceBaselineStateKey)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(baseline, "task-two ") {
		t.Fatalf("baseline = %q", baseline)
	}
}

func TestInstructionSurfaceGuardRejectsInstructionSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture is Unix-oriented")
	}
	root := t.TempDir()
	writeInstructionGuardFile(t, root, "target", "instruction")
	if err := os.Symlink("target", filepath.Join(root, "AGENTS.local.md")); err != nil {
		t.Fatal(err)
	}
	_, err := captureInstructionSurfaceSnapshot(root)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("instruction symlink error = %v", err)
	}
}

func newInstructionGuardRunner(t *testing.T, root, taskID string) *ClaudeRunner {
	t.Helper()
	st := newTestStateStore(t)
	if err := st.Write("task.id", taskID); err != nil {
		t.Fatal(err)
	}
	return NewClaudeRunner(config.AppConfig{RepoRoot: root}, st)
}

func writeInstructionGuardFile(t *testing.T, root, relative, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readInstructionGuardFile(t *testing.T, root, relative string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
