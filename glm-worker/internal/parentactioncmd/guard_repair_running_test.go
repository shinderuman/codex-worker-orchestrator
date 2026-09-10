package parentactioncmd

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/guardrepair"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPrepareGuardRepairForResumeReclaimsRunningOwner(t *testing.T) {
	cfg, st, record := newGuardRepairLifecycleState(t)
	digest, err := guardrepair.RelevantDigest(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	record.Status = state.GuardRepairRunning
	record.RelevantDigest = digest
	if err := st.SaveGuardRepairRecord(record); err != nil {
		t.Fatal(err)
	}

	_, err = prepareGuardRepairForResume(cfg, st, record)
	if err == nil || strings.Contains(err.Error(), "already running") {
		t.Fatalf("stale running owner was not reclaimed: %v", err)
	}
	if !strings.Contains(err.Error(), "original guard-recoverable checkpoint") {
		t.Fatalf("reclaimed repair did not continue through normal repair validation: %v", err)
	}
	got, loadErr := st.LoadGuardRepairRecord()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if got.Status != state.GuardRepairFailed {
		t.Fatalf("normal repair validation failure was not recorded: %#v", got)
	}
}

func TestReusableGuardRepairRunningRequiresUnchangedSource(t *testing.T) {
	cfg, st, record := newGuardRepairLifecycleState(t)
	digest, err := guardrepair.RelevantDigest(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	record.Status = state.GuardRepairRunning
	record.RelevantDigest = digest
	if err := st.SaveGuardRepairRecord(record); err != nil {
		t.Fatal(err)
	}
	if _, ok := reusableGuardRepairRecord(cfg, st); !ok {
		t.Fatal("unchanged interrupted repair should be reclaimable")
	}

	writeGuardRepairTestFile(t, cfg.RepoRoot, "glm-worker/internal/workflow/guard_recovery.go", "package workflow\n")
	if _, ok := reusableGuardRepairRecord(cfg, st); ok {
		t.Fatal("changed repair source must not reuse the interrupted strategy")
	}
}
