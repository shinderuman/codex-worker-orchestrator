package workflow

import (
	"encoding/json"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestReviewedParentSourceRevisionCreatesCheckpointAndRecoversIdempotently(t *testing.T) {
	w, _ := acceptedResultRecoveryFixture(t)
	cp, err := w.state.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	result, err := w.previousImplementedReviewContext()
	if err != nil {
		t.Fatal(err)
	}
	cp.Stage, cp.Role, cp.Model, cp.Phase = state.ResumeStageReview, state.ReviewerRole, "sonnet", "reviewer-1-high-floor"
	cp.ReadOnly, cp.ResultCorrection, cp.ReportOnly = true, false, false
	cp.WorkerResult = &result
	cp.SetStopKind(state.ResumeStopInterrupted)
	if err := w.attachStopRepositoryBoundary(&cp); err != nil {
		t.Fatal(err)
	}
	if err := w.state.ClearResumeCheckpoint(); err != nil {
		t.Fatal(err)
	}
	if err := w.state.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	if err := w.state.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh}, state.ParentReviewProducer{Role: "reviewer", Model: "sonnet"}); err != nil {
		t.Fatal(err)
	}
	open, err := w.state.CurrentParentReview()
	if err != nil {
		t.Fatal(err)
	}
	binding := state.ControllerRuntimeBinding{AttemptID: "same-attempt", TaskPath: "IMPLEMENTATION_TASKS/active.md", TaskContractDigest: "same-contract"}
	if err := w.state.SaveControllerRuntimeBinding(binding); err != nil {
		t.Fatal(err)
	}
	taskID, err := w.state.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	source := ReentryReviewRepairEvidence{TaskID: taskID, Binding: binding, Checkpoint: cp, Open: open}
	foreign := source
	foreign.Binding.AttemptID = "foreign-attempt"
	data, err := json.Marshal(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.CompleteReentryReviewRepair(data); err == nil {
		t.Fatal("foreign Attempt admitted")
	}
	data, err = json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	before, err := w.state.ReadModelCallLogs(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.CompleteReentryReviewRepair(data); err != nil {
		t.Fatal(err)
	}
	if err := w.CompleteReentryReviewRepair(data); err != nil {
		t.Fatal(err)
	}
	saved, err := w.state.LoadResumeCheckpoint()
	if err != nil || !saved.ReadOnly || saved.ResultCorrection || saved.Role != state.ReviewerRole {
		t.Fatalf("Reviewer checkpoint: %#v %v", saved, err)
	}
	after, err := w.state.ReadModelCallLogs(taskID)
	if err != nil || len(before) != len(after) {
		t.Fatal("runtime recovery invoked a model")
	}
}

func TestParentSourceReviewReportsUnprovableMethodTargetWithoutRewritingPacket(t *testing.T) {
	w, _ := acceptedResultRecoveryFixture(t)
	writeGitTestFile(t, w.config.RepoRoot, "review_target.go", "package sample\ntype Store struct{}\nfunc (s *Store) ImportParentSource() {}\n")
	snapshot, err := state.CaptureGitSnapshot(w.config.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"review_target.go:ImportParentSource", "review_target.go:Store.ImportParentSource"} {
		result := packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh, Targets: []string{target}, SolQuestion: "target proof"}
		if err := w.state.RecordSolResultWithReviewSnapshot(result, state.ParentReviewProducer{Role: "reviewer", Model: "sonnet"}, state.SnapshotDigest{Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest}); err != nil {
			t.Fatal(err)
		}
		corrections, err := w.parentSourceReviewTargetCorrections()
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if target == "review_target.go:ImportParentSource" {
			want = 1
		}
		if len(corrections) != want {
			t.Fatalf("target %s corrections=%v", target, corrections)
		}
		binding, err := w.state.CurrentParentReviewBinding()
		if err != nil || binding.Targets[0] != target {
			t.Fatal("source proof check rewrote the model packet")
		}
	}
}
