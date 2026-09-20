package parentactioncmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPublicationParentValidationGateAcceptsExactPromotedSnapshot(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, "README.md"), []byte("promoted validation\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	publicationGit(t, cfg.RepoRoot, "add", "README.md")
	candidate, failure := preparePublicationCandidate(cfg, st, "promoted validation publication")
	if failure != nil {
		t.Fatalf("prepare failed: %#v", failure)
	}
	promotion := promotePublicationCandidate(cfg, st)
	if promotion.Status != publicationPromotionStatusPromoted || promotion.Failure != nil {
		t.Fatalf("promotion = %#v", promotion)
	}

	currentRaw, err := state.CaptureGitSnapshot(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	current := state.SnapshotDigest{
		Head:                          currentRaw.Head,
		IndexDigest:                   currentRaw.IndexDigest,
		WorktreeDigest:                currentRaw.WorktreeDigest,
		WorktreeDigestExcludingParent: currentRaw.WorktreeDigestExcludingParent,
	}
	promotedSnapshotID := state.ValidationSnapshotID(current.Head, current.IndexDigest, current.WorktreeDigest)
	if promotedSnapshotID == "" || promotedSnapshotID == candidate.SnapshotID {
		t.Fatalf("promoted snapshot id = %q candidate = %q", promotedSnapshotID, candidate.SnapshotID)
	}

	form := "go-test"
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Model:            "opus",
		ParentValidation: &packet.ParentValidationRequest{Form: form},
	}); err != nil {
		t.Fatal(err)
	}
	runID := strings.Repeat("b", 32)
	st.RecordValidationEvent(state.TaskValidationEvent{
		Source:          "quality-gate",
		Form:            form,
		ValidationRunID: runID,
		SnapshotID:      promotedSnapshotID,
		Result:          state.ValidationResultPass,
		ExitCode:        0,
		ExitSource:      state.ValidationExitSourceTarget,
	})

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
		Form:                          form,
		Repository:                    cfg.RepoRoot,
		Head:                          current.Head,
		IndexDigest:                   current.IndexDigest,
		WorktreeDigest:                current.WorktreeDigest,
		WorktreeDigestExcludingParent: current.WorktreeDigestExcludingParent,
		TaskID:                        candidate.TaskID,
		CompletedAt:                   &completed,
		Status:                        publicationGatePass,
		ExitCode:                      0,
		ExitSource:                    state.ValidationExitSourceTarget,
		Log:                           logPath,
	}
	writePublicationQualityRun(t, filepath.Join(dir, "run.json"), record)

	gate, required := publicationParentValidationGate(st, cfg.RepoRoot, candidate)
	if !required || gate.Status != publicationGatePass || gate.ValidationRunID != runID || gate.SnapshotID != promotedSnapshotID {
		t.Fatalf("promoted validation gate = %#v required=%v", gate, required)
	}

	record.IndexDigest = strings.Repeat("f", 64)
	writePublicationQualityRun(t, filepath.Join(dir, "run.json"), record)
	gate, required = publicationParentValidationGate(st, cfg.RepoRoot, candidate)
	if !required || gate.Status != publicationGateStale || !strings.Contains(gate.Reason, "snapshot") {
		t.Fatalf("tampered promoted validation gate = %#v required=%v", gate, required)
	}
}
