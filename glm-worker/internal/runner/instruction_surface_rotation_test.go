package runner

import "testing"

func TestRotateInstructionSurfaceBaselineKeepsTaskWorkAndInvalidatesSessions(t *testing.T) {
	root := t.TempDir()
	writeInstructionGuardFile(t, root, "AGENTS.local.md", "before")
	writeInstructionGuardFile(t, root, "ordinary.go", "package before")
	r := newInstructionGuardRunner(t, root, "task-one")
	if _, err := r.prepareInstructionSurfaceGuard(); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ key, value string }{
		{"worker.id", "worker-session"},
		{"worker.ready", "1"},
		{"reviewer.id", "reviewer-session"},
		{"reviewer.ready", "1"},
	} {
		if err := r.state.Write(item.key, item.value); err != nil {
			t.Fatal(err)
		}
	}
	writeInstructionGuardFile(t, root, "AGENTS.local.md", "parent-applied")
	writeInstructionGuardFile(t, root, "ordinary.go", "package changed")

	rotation, err := RotateInstructionSurfaceBaseline(r.config, r.state)
	if err != nil {
		t.Fatal(err)
	}
	if rotation.PreviousDigest == rotation.CurrentDigest {
		t.Fatal("rotation kept the old instruction digest")
	}
	if r.state.ReadOr("task.id", "") != "task-one" {
		t.Fatal("rotation changed task identity")
	}
	if got := readInstructionGuardFile(t, root, "ordinary.go"); got != "package changed" {
		t.Fatalf("rotation changed ordinary worktree content: %q", got)
	}
	if r.state.Exists("worker.id") || r.state.Exists("worker.ready") || r.state.Exists("reviewer.id") || r.state.Exists("reviewer.ready") {
		t.Fatal("rotation left reusable model sessions")
	}
	if _, err := r.prepareInstructionSurfaceGuard(); err != nil {
		t.Fatalf("rotated instruction baseline was not accepted by the active task: %v", err)
	}
}

func TestRotateInstructionSurfaceBaselineRejectsNoChange(t *testing.T) {
	root := t.TempDir()
	writeInstructionGuardFile(t, root, "AGENTS.md", "same")
	r := newInstructionGuardRunner(t, root, "task-one")
	if _, err := r.prepareInstructionSurfaceGuard(); err != nil {
		t.Fatal(err)
	}
	if _, err := RotateInstructionSurfaceBaseline(r.config, r.state); err == nil {
		t.Fatal("rotation accepted an unchanged instruction surface")
	}
}
