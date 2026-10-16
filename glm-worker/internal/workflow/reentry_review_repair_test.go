package workflow

import (
	"encoding/json"
	"strings"
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
	digest := state.SnapshotDigest{Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest}
	bare := packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh, Targets: []string{"review_target.go:ImportParentSource"}, SolQuestion: "target proof"}
	if err := w.state.RecordSolResultWithReviewSnapshot(bare, state.ParentReviewProducer{Role: "reviewer", Model: "sonnet"}, digest); err == nil || !strings.Contains(err.Error(), "review target admission rejected before binding") || !strings.Contains(err.Error(), "[symbol-not-declared]") {
		t.Fatalf("unaddressable method target admitted: %v", err)
	}
	if binding, err := w.state.CurrentParentReviewBinding(); err != nil || binding != nil {
		t.Fatalf("rejected report persisted a review binding: %#v %v", binding, err)
	}
	result := packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh, Targets: []string{"review_target.go:Store.ImportParentSource"}, SolQuestion: "target proof"}
	if err := w.state.RecordSolResultWithReviewSnapshot(result, state.ParentReviewProducer{Role: "reviewer", Model: "sonnet"}, digest); err != nil {
		t.Fatal(err)
	}
	corrections, err := w.parentSourceReviewTargetCorrections()
	if err != nil || len(corrections) != 0 {
		t.Fatalf("addressable Type.Member produced corrections: %v %v", corrections, err)
	}
	writeGitTestFile(t, w.config.RepoRoot, "review_target.go", "package sample\ntype Store struct{}\n")
	corrections, err = w.parentSourceReviewTargetCorrections()
	if err != nil || len(corrections) != 1 {
		t.Fatalf("drifted source did not surface a correction: %v %v", corrections, err)
	}
	binding, err := w.state.CurrentParentReviewBinding()
	if err != nil || binding.Targets[0] != "review_target.go:Store.ImportParentSource" {
		t.Fatal("source proof check rewrote the model packet")
	}
}
