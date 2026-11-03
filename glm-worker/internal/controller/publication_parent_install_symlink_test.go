package controller

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
)

func TestParentOnlyInstallSmokeRejectsSymlinkedLog(t *testing.T) {
	t.Parallel()
	f := newParentOnlyInstallSmokeFixture(t)
	logPath := f.runtime.Path(filepath.FromSlash(f.smoke.Log))
	external := filepath.Join(t.TempDir(), "foreign-smoke.log")
	if err := os.WriteFile(external, []byte("unrelated smoke evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, logPath); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.fixture.store.readCandidateInstallSmoke(f.candidate, f.root, f.runID); err == nil {
		t.Fatal("external smoke evidence accepted through a symlinked log")
	}
	data, err := os.ReadFile(external)
	if err != nil || string(data) != "unrelated smoke evidence" {
		t.Fatalf("external log was modified: %q %v", data, err)
	}
}

func TestParentOnlyInstallSmokeRejectsSymlinkedRecord(t *testing.T) {
	t.Parallel()
	f := newParentOnlyInstallSmokeFixture(t)
	recordPath := f.runtime.Path(filepath.Join("install-smoke-runs", f.runID, qualitygate.RunFile))
	external := filepath.Join(t.TempDir(), "foreign-run.json")
	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(external, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(recordPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, recordPath); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.fixture.store.readCandidateInstallSmoke(f.candidate, f.root, f.runID); err == nil {
		t.Fatal("external run record accepted via symlink")
	}
}

func TestParentOnlyInstallSmokeRejectsSymlinkedTaskIdentity(t *testing.T) {
	t.Parallel()
	f := newParentOnlyInstallSmokeFixture(t)
	identityPath := f.runtime.Path("task.id")
	external := filepath.Join(t.TempDir(), "foreign-task-id")
	if err := os.WriteFile(external, []byte(f.candidate.AttemptID), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(identityPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, identityPath); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.fixture.store.readCandidateInstallSmoke(f.candidate, f.root, f.runID); err == nil {
		t.Fatal("external parent-only task identity accepted through symlink")
	}
	attempt, err := f.fixture.store.loadAttempt(f.candidate.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.fixture.store.bindParentOnlySmokeTaskID(attempt); err == nil {
		t.Fatal("parent-only materialization accepted a symlinked task identity")
	}
}
