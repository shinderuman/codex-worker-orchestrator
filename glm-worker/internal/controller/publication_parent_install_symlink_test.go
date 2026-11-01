package controller

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
)

// A valid log locator must not allow the smoke reader to follow a symlink
// outside the task-scoped evidence directory.
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
