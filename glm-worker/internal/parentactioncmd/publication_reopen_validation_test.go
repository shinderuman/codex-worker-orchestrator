package parentactioncmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPublicationValidationRejectsPriorPublicationSnapshot(t *testing.T) {
	cfg, st, first := preparePublicationRuntimeCandidate(t)
	runID := strings.Repeat("b", 32)
	completed := time.Now().UTC()
	dir := filepath.Join(st.Path("quality-gate-runs"), runID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "gate.log")
	if err := os.WriteFile(logPath, []byte("pass\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := publicationQualityRunRecord{
		ValidationRunID:               runID,
		Form:                          "go-test",
		Repository:                    cfg.RepoRoot,
		Head:                          first.Snapshot.Head,
		IndexDigest:                   first.Snapshot.IndexDigest,
		WorktreeDigest:                first.Snapshot.WorktreeDigest,
		WorktreeDigestExcludingParent: first.Snapshot.WorktreeDigestExcludingParent,
		TaskID:                        first.TaskID,
		CompletedAt:                   &completed,
		Status:                        publicationGatePass,
		ExitCode:                      0,
		ExitSource:                    state.ValidationExitSourceTarget,
		Log:                           logPath,
	}
	writePublicationQualityRun(t, filepath.Join(dir, "run.json"), record)
	if err := verifyPublicationQualityRun(st, cfg.RepoRoot, first, record.Form, runID); err != nil {
		t.Fatalf("first publication validation rejected: %v", err)
	}

	second := first
	second.Snapshot.IndexDigest = strings.Repeat("f", 64)
	second.SnapshotID = state.ValidationSnapshotID(second.Snapshot.Head, second.Snapshot.IndexDigest, second.Snapshot.WorktreeDigest)
	if second.SnapshotID == first.SnapshotID {
		t.Fatal("second publication snapshot identity did not change")
	}
	if err := verifyPublicationQualityRun(st, cfg.RepoRoot, second, record.Form, runID); err == nil || !strings.Contains(err.Error(), "snapshot") {
		t.Fatalf("prior publication validation was reused for new snapshot: %v", err)
	}
}
