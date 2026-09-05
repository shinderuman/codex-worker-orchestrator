package app

import (
	"io"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

func seedQualitySurfaceApprovalState(t *testing.T, cfg config.AppConfig) *state.StateStore {
	t.Helper()
	st := startParentHandoffTask(t, cfg)
	result := packet.Result{
		Status: packet.StatusImplemented, Risk: packet.RiskLow, Summary: "implemented",
		RequirementCoverage: "covered", Tests: "pass", Unverified: "none",
	}
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:                         state.ResumeStageWorker,
		Phase:                         "worker-new",
		Role:                          state.WorkerRole,
		Model:                         "opus",
		Prompt:                        "p",
		Request:                       "r",
		CompletedResult:               &result,
		QualitySurfaceApprovalPending: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh}, state.ParentReviewProducer{Role: string(state.WorkerRole)})
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCallLog(state.ModelCallLog{
		CallID:       "call-quality-worker",
		CallType:     state.CallTypeTask,
		TaskID:       taskID,
		Phase:        "worker-new",
		Role:         state.WorkerRole,
		ModelAlias:   "opus",
		Outcome:      "success",
		PacketStatus: string(packet.StatusImplemented),
	})
	return st
}

func TestParentHandoffQualitySurfaceApprovalIsSoleRequiredAction(t *testing.T) {
	cfg := newAppConfig(t)
	seedQualitySurfaceApprovalState(t, cfg)

	var output parentHandoffOutput
	executeCommandOutput(t, cfg, ModeHandoff, &output, "--handoff")
	if !output.Consistent || output.RequiredAction == nil || *output.RequiredAction != string(state.ParentActionApproveSurface) {
		t.Fatalf("quality-surface approval handoff = %#v", output)
	}
	if len(output.AllowedActions) != 2 || output.AllowedActions[0] != string(state.ParentActionApproveSurface) || output.AllowedActions[1] != string(state.ParentActionFix) {
		t.Fatalf("quality-surface approval allowed actions = %#v", output.AllowedActions)
	}
	if output.RequiredActionParameters["accepted-scope"] != "current-diff" {
		t.Fatalf("quality-surface approval parameters = %#v", output.RequiredActionParameters)
	}
}

func TestAcceptIsDeniedWithZeroModelCallsWhileApprovalPending(t *testing.T) {
	cfg := newAppConfig(t)
	seedQualitySurfaceApprovalState(t, cfg)

	runnerCalls := 0
	rf := func(_ config.AppConfig, _ *state.StateStore, _ *runner.StopController) workflow.ModelRunner {
		runnerCalls++
		return nil
	}
	err := Execute(Command{Mode: ModeAccept}, cfg, rf, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("reviewer未実行のquality surface停止でacceptが受理されました")
	}
	if !strings.Contains(err.Error(), "approve-surface") {
		t.Fatalf("accept拒否がapprove-surfaceへ誘導していません: %v", err)
	}
	if runnerCalls != 0 {
		t.Fatalf("accept拒否はmodel call 0回であるべき: %d", runnerCalls)
	}
	st, stateErr := state.NewStateStore(cfg)
	if stateErr != nil {
		t.Fatal(stateErr)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %s; false-completeが再現されてしまいました", st.TaskStatus())
	}
}

func TestApproveSurfaceDeniedWhenNoApprovalIsPending(t *testing.T) {
	cfg := newAppConfig(t)
	st := startParentHandoffTask(t, cfg)
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh}, state.ParentReviewProducer{Role: string(state.ReviewerRole)})

	err := Execute(Command{Mode: ModeApproveSurface, AcceptedScope: "current-diff"}, cfg, nil, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("approval未保留状態でapprove-surfaceが受理されました")
	}
	if !strings.Contains(err.Error(), "quality-surface approval is not pending") {
		t.Fatalf("approve-surface拒否理由 = %v", err)
	}
}
