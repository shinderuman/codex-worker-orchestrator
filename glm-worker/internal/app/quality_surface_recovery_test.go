package app

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func seedQualitySurfaceDecisionWaitLeftover(t *testing.T, st *state.StateStore) {
	t.Helper()
	if err := st.Write("last-decision", "decision-body"); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	completed := packet.Result{Status: packet.StatusImplemented, Risk: packet.RiskLow}
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:                         state.ResumeStageWorker,
		Phase:                         "worker-decision",
		Role:                          state.WorkerRole,
		Model:                         "opus",
		Request:                       "request",
		Decision:                      "decision-body",
		QualitySurfaceApprovalPending: true,
		CompletedResult:               &completed,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh}, state.ParentReviewProducer{Role: "worker", Model: "opus"}); err != nil {
		t.Fatal(err)
	}
}

func seedStaleApprovedQualitySurfaceReview(t *testing.T, st *state.StateStore) {
	t.Helper()
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:          state.ResumeStageAutoFix,
		Phase:          "worker-auto-fix-1",
		Role:           state.WorkerRole,
		Model:          "opus",
		Request:        "request",
		StopKind:       state.ResumeStopRateLimited,
		ResetAtRFC3339: "2026-09-09T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh}, state.ParentReviewProducer{Role: "worker", Model: "opus"}); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverQualitySurfaceCommandRepairsDecisionWaitLeftover(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	seedQualitySurfaceDecisionWaitLeftover(t, st)

	command, err := ParseCommand([]string{"--recover-quality-surface", taskID})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != modeRecoverQualitySurface || command.Payload != taskID {
		t.Fatalf("command = %#v", command)
	}
	if _, err := st.ParentActionPlan(); err == nil {
		t.Fatal("leftover marker must keep the handoff inconsistent before recovery")
	}

	var out bytes.Buffer
	if err := Execute(command, cfg, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var output qualitySurfaceRecoveryOutput
	if err := json.Unmarshal(out.Bytes(), &output); err != nil {
		t.Fatalf("recovery出力がmachine JSONではありません: %v: %q", err, out.String())
	}
	if output.Status != "recovered" || output.TaskID != taskID ||
		output.TaskStatus != string(state.TaskStatusWaitingSolReview) ||
		output.Repair != qualitySurfaceRepairDecisionWait {
		t.Fatalf("recovery output = %#v", output)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview || st.Exists("pending-decision") {
		t.Fatalf("recovered state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
	plan, planErr := st.ParentActionPlan()
	if planErr != nil || plan.RequiredAction != state.ParentActionApproveSurface ||
		plan.RequiredActionParameters["accepted-scope"] != "current-diff" {
		t.Fatalf("recovered plan = %#v err=%v", plan, planErr)
	}

	if err := Execute(command, cfg, nil, io.Discard, io.Discard); err == nil {
		t.Fatal("recovery must be rejected once the canonical wait state is restored")
	}
}

func TestRecoverQualitySurfaceCommandClosesStaleApprovedReview(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	seedStaleApprovedQualitySurfaceReview(t, st)

	command, err := ParseCommand([]string{"--recover-quality-surface", taskID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ParentActionPlan(); err == nil {
		t.Fatal("stale open review must keep the stopped handoff inconsistent before recovery")
	}

	var out bytes.Buffer
	if err := Execute(command, cfg, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var output qualitySurfaceRecoveryOutput
	if err := json.Unmarshal(out.Bytes(), &output); err != nil {
		t.Fatalf("recovery出力がmachine JSONではありません: %v: %q", err, out.String())
	}
	if output.Status != "recovered" || output.TaskStatus != string(state.TaskStatusRateLimited) ||
		output.Repair != qualitySurfaceRepairApprovedReview {
		t.Fatalf("recovery output = %#v", output)
	}
	if st.TaskStatus() != state.TaskStatusRateLimited || st.OpenParentReviewLabel() != "none" {
		t.Fatalf("recovered state: status=%s review=%s", st.TaskStatus(), st.OpenParentReviewLabel())
	}
	plan, planErr := st.ParentActionPlan()
	if planErr != nil || plan.RequiredAction != state.ParentActionResume {
		t.Fatalf("recovered plan = %#v err=%v", plan, planErr)
	}
}

func TestRecoverQualitySurfaceCommandRejectsTaskIDMismatch(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	seedQualitySurfaceDecisionWaitLeftover(t, st)

	if _, err := ParseCommand([]string{"--recover-quality-surface"}); err == nil {
		t.Fatal("missing task ID must be a usage error")
	}
	command, err := ParseCommand([]string{"--recover-quality-surface", "00000000-0000-4000-8000-000000000000"})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(command, cfg, nil, io.Discard, io.Discard); err == nil {
		t.Fatal("task ID mismatch must be rejected")
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview || !st.Exists("pending-decision") {
		t.Fatalf("rejected recovery changed the state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
}

func TestRecoverQualitySurfaceCommandRejectsUncoveredStates(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}

	command, err := ParseCommand([]string{"--recover-quality-surface", taskID})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(command, cfg, nil, io.Discard, io.Discard); err == nil {
		t.Fatal("active task without a quality-surface leftover must be rejected")
	}
	if st.TaskStatus() != state.TaskStatusActive {
		t.Fatalf("rejected recovery changed the status: %s", st.TaskStatus())
	}
}
