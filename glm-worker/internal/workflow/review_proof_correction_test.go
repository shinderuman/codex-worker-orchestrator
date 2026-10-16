package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func reviewProofPacket(target string) string {
	return strings.Replace(needsSolReviewPacket(), repositoryharness.MarkerPath+":1", target, 1)
}

func reviewProofWorkflow(t *testing.T, r *scriptedRunner) (*Workflow, *state.StateStore) {
	t.Helper()
	st := newStateStoreT(t)
	w := newWorkflowT(t, st, r)
	writeGitTestFile(t, w.config.RepoRoot, "review_target.go", "package sample\ntype Store struct{}\nfunc (s *Store) ImportParentSource() {}\n")
	gitIn(t, w.config.RepoRoot, "add", "review_target.go")
	gitIn(t, w.config.RepoRoot, "commit", "-qm", "review source")
	return w, st
}

func TestReviewProofCorrectsInvalidTargetBeforeBinding(t *testing.T) {
	for _, target := range []string{"review_target.go:ImportParentSource", "missing.go:Missing", "review_target.go:99", "review_target.go:@diff"} {
		t.Run(target, func(t *testing.T) {
			r := &scriptedRunner{steps: []runnerStep{
				{structured: implementedPacketWithRisk("work", "HIGH")},
				{structured: reviewProofPacket(target)},
				{structured: reviewProofPacket("review_target.go:Store.ImportParentSource")},
			}}
			w, st := reviewProofWorkflow(t, r)
			r.onRun = func() {
				if len(r.prompts) != 3 {
					return
				}
				cp, err := st.LoadResumeCheckpoint()
				if err != nil || !cp.ResultCorrection || !cp.ReadOnly || cp.Role != state.ReviewerRole {
					t.Fatalf("read-only correction checkpoint = %#v, %v", cp, err)
				}
				if binding, err := st.CurrentParentReviewBinding(); err != nil || binding != nil {
					t.Fatalf("invalid report created a binding: %#v, %v", binding, err)
				}
			}
			if err := w.ExecuteNewTask("request"); err != nil {
				t.Fatal(err)
			}
			binding, err := st.CurrentParentReviewBinding()
			if err != nil || binding == nil || binding.Targets[0] != "review_target.go:Store.ImportParentSource" {
				t.Fatalf("corrected binding = %#v, %v", binding, err)
			}
			taskID, _ := st.TaskID()
			logs, err := st.ReadModelCallLogs(taskID)
			if err != nil || len(logs) != 3 || logs[1].Outcome != "invalid_packet" || logs[1].SessionID != logs[2].SessionID {
				t.Fatalf("same-session correction evidence = %#v, %v", logs, err)
			}
			if len(r.readOnlyCalls) != 3 || !r.readOnlyCalls[1] || !r.readOnlyCalls[2] || r.models[1] != r.models[2] {
				t.Fatalf("Reviewer identity changed: read-only=%v, models=%v", r.readOnlyCalls, r.models)
			}
		})
	}
}

func TestReviewProofCorrectionRetainsCheckpointAcrossRateLimit(t *testing.T) {
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("work", "HIGH")},
		{structured: reviewProofPacket("review_target.go:ImportParentSource")},
		{output: zaiFiveHourLog, runErr: errors.New("exit status 1")},
		{structured: reviewProofPacket("review_target.go:Store.ImportParentSource")},
	}}
	w, st := reviewProofWorkflow(t, r)
	err := w.ExecuteNewTask("request")
	var limit runner.ZaiRateLimitError
	if !errors.As(err, &limit) {
		t.Fatalf("correction quota stop = %v", err)
	}
	cp, err := st.LoadResumeCheckpoint()
	if err != nil || !cp.ResultCorrection || !cp.ReadOnly || cp.Role != state.ReviewerRole {
		t.Fatalf("retained Reviewer checkpoint = %#v, %v", cp, err)
	}
	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if binding, err := st.CurrentParentReviewBinding(); err != nil || binding == nil {
		t.Fatalf("resumed Review binding = %#v, %v", binding, err)
	}
}

func TestReviewProofRepeatedInvalidTargetStopsWithoutBinding(t *testing.T) {
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("work", "HIGH")},
		{structured: reviewProofPacket("review_target.go:ImportParentSource")},
		{structured: reviewProofPacket("review_target.go:AnotherMissing")},
	}}
	w, st := reviewProofWorkflow(t, r)
	err := w.ExecuteNewTask("request")
	failure, ok := ResultCorrectionFailureFromError(err)
	if !ok || failure.Reason != "repeated_violation" {
		t.Fatalf("repeated proof violation did not stop: %v", err)
	}
	if binding, err := st.CurrentParentReviewBinding(); err != nil || binding != nil {
		t.Fatalf("invalid report persisted a binding: %#v, %v", binding, err)
	}
	assertResultFormatRecoveryAction(t, st)
}

func TestReviewProofValidTargetDoesNotAddCorrection(t *testing.T) {
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("work", "HIGH")},
		{structured: reviewProofPacket("review_target.go:Store.ImportParentSource")},
	}}
	w, st := reviewProofWorkflow(t, r)
	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 2 || currentStats(t, st).ResultCorrections != 0 || st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("valid Review lifecycle changed: calls=%d status=%s", len(r.prompts), st.TaskStatus())
	}
	if _, err := st.LoadResumeCheckpoint(); !errors.Is(err, state.ErrNoResumeCheckpoint) {
		t.Fatalf("accepted Review retained a checkpoint: %v", err)
	}
	if binding, err := st.CurrentParentReviewBinding(); err != nil || binding == nil || binding.PacketSHA256 == "" {
		t.Fatalf("valid Review lacks bound proof: %#v, %v", binding, err)
	}
}
