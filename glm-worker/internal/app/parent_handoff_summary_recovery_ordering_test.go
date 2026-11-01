package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentcontinuation"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

func TestSummaryRecoveryHandoffDoesNotBypassPendingControllerTransition(t *testing.T) {
	cfg, _, pending, _ := interruptedCompletionWorkflowFixture(t)
	bound, _ := retiredWorkflowRuntime(t, cfg)
	st := startParentHandoffTask(t, bound)
	const sessionID = "pending-controller-summary-recovery"
	const phase = "worker-new-result-correct"
	if err := st.Write("worker.id", sessionID); err != nil {
		t.Fatal(err)
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.CaptureGitSnapshot(bound.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	dirty, err := state.CaptureStopDirtyFiles(bound.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	packetData, err := json.Marshal(packet.Result{
		Status: packet.StatusImplemented, Risk: packet.RiskLow,
		Summary:             strings.Repeat("x", packet.MaxFieldBytes+1),
		RequirementCoverage: "covered", Tests: "pass", Unverified: "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(packetData)
	correction, err := json.Marshal(map[string]any{
		"version": 2, "task_id": taskID, "role": "worker", "session_id": sessionID,
		"snapshot": state.SnapshotDigest{
			Head: snapshot.Head, IndexDigest: snapshot.IndexDigest,
			WorktreeDigest:                snapshot.WorktreeDigest,
			WorktreeDigestExcludingParent: snapshot.WorktreeDigestExcludingParent,
		},
		"attempts": 1, "resubmissions": 1,
		"terminal": map[string]any{"reason": "repeated_violation"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write(state.ResultCorrectionStateFile, string(correction)); err != nil {
		t.Fatal(err)
	}
	st.RecordModelCallLog(state.ModelCallLog{
		CallID: "pending-summary-source", CallType: state.CallTypeTask,
		TaskID: taskID, SessionID: sessionID, Role: state.WorkerRole,
		Phase: phase, Outcome: "invalid_packet",
		ResumeSource: "result-format-recovery", ReadOnly: true,
		Response: string(packetData), ResponseSHA256: hex.EncodeToString(sum[:]),
	})
	if err := st.EnterStop(state.ResumeCheckpoint{
		Stage: state.ResumeStageWorker, Phase: phase,
		Role: state.WorkerRole, Model: "worker",
		ReadOnly: true, StopKind: state.ResumeStopResultFormat, ResultCorrection: true,
		StopGitSnapshot: &snapshot, StopDirtyFiles: dirty,
	}); err != nil {
		t.Fatal(err)
	}
	wf := workflow.NewWorkflow(bound, st, nil, io.Discard)
	if err := wf.SummaryRecoveryAvailable(); err != nil {
		t.Fatalf("summary recovery fixture is unavailable: %v", err)
	}
	legacy := buildParentHandoffFromContinuation(st, parentcontinuation.BuildWithRepositoryPolicy(bound, st))
	if !legacy.Consistent || legacy.RequiredAction == nil || *legacy.RequiredAction != string(state.ParentActionResubmitReport) {
		t.Fatalf("fixture does not reproduce early return: %#v; inconsistency=%s", legacy, stringPointerValue(legacy.Inconsistency))
	}
	handoff := buildParentHandoffWithConfig(bound, st)
	if handoff.Controller == nil || handoff.Controller.Head.PendingTransitionID != pending.TransitionID {
		t.Fatalf("pending canonical transition was omitted: %#v", handoff.Controller)
	}
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerExecutionSurface {
		t.Fatalf("summary recovery concealed controller recovery: %#v", handoff)
	}
	var command controllerExecutionCommand
	if err := json.Unmarshal([]byte(handoff.RequiredActionParameters["controller_command_json"]), &command); err != nil {
		t.Fatal(err)
	}
	if command.Action != "recover" || command.TransitionID != pending.TransitionID {
		t.Fatalf("canonical recovery command was replaced by resubmit-report: %#v", command)
	}
}
