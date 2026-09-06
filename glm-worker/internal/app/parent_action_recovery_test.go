package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const preCallInstructionGuardFailureText = "repository instruction surface guard failed: before-call-mismatch: AGENTS.md/AGENTS.local.md"

func TestRecoverParentActionCommandRestoresDecisionLeftover(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	seedParentActionSessions(t, st)
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}

	command, err := ParseCommand([]string{"--recover-parent-action"})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != modeRecoverParentAction {
		t.Fatalf("mode = %d want %d", command.Mode, modeRecoverParentAction)
	}

	seedDecisionLeftover(t, st)
	recordParentActionMaterial(t, st, "worker-decision", "error", preCallInstructionGuardFailureText)
	recordsBefore := telemetryRecordCount(t, st)

	var out bytes.Buffer
	if err := Execute(command, cfg, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var output parentActionRecoveryOutput
	if err := json.Unmarshal(out.Bytes(), &output); err != nil {
		t.Fatalf("recovery出力がmachine JSONではありません: %v: %q", err, out.String())
	}
	if output.Status != "recovered" || output.TaskStatus != string(state.TaskStatusWaitingDecision) || output.TaskID != taskID {
		t.Fatalf("recovery output = %#v", output)
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("recovered state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
	plan, planErr := st.ParentActionPlan()
	if planErr != nil || plan.RequiredAction != state.ParentActionDecision {
		t.Fatalf("recovered plan = %#v err=%v", plan, planErr)
	}
	assertParentActionLeftoverRetained(t, st, "last-decision", "decision-body", recordsBefore)

	if err := Execute(command, cfg, nil, io.Discard, io.Discard); err == nil {
		t.Fatal("recovery must be rejected once the waiting state is restored")
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("second recovery changed the state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
}

func TestRecoverParentActionCommandRestoresFixLeftover(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	seedParentActionSessions(t, st)

	command, err := ParseCommand([]string{"--recover-parent-action"})
	if err != nil {
		t.Fatal(err)
	}

	if err := st.Write("last-review", "review-body"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginParentFix(state.ParentOriginCodexReview, state.ParentCauseParentOrchestration); err != nil {
		t.Fatal(err)
	}
	recordParentActionMaterial(t, st, "worker-explicit-fix", "error", preCallInstructionGuardFailureText)
	recordsBefore := telemetryRecordCount(t, st)

	var out bytes.Buffer
	if err := Execute(command, cfg, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var output parentActionRecoveryOutput
	if err := json.Unmarshal(out.Bytes(), &output); err != nil {
		t.Fatalf("recovery出力がmachine JSONではありません: %v: %q", err, out.String())
	}
	if output.Status != "recovered" || output.TaskStatus != string(state.TaskStatusWaitingSolReview) {
		t.Fatalf("recovery output = %#v", output)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview || st.Exists("pending-decision") {
		t.Fatalf("recovered state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
	plan, planErr := st.ParentActionPlan()
	if planErr != nil || !plan.Allows(state.ParentActionFix) {
		t.Fatalf("recovered plan = %#v err=%v", plan, planErr)
	}
	assertParentActionLeftoverRetained(t, st, "last-review", "review-body", recordsBefore)
}

func TestRecoverParentActionCommandRejectsForeignConditions(t *testing.T) {
	tests := []struct {
		name string
		seed func(t *testing.T, st *state.StateStore)
	}{
		{
			name: "repository lock is held by a running worker",
			seed: func(t *testing.T, st *state.StateStore) {
				t.Helper()
				lock, err := AcquireRepoLock(st.LockPath())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = lock.Close() })
			},
		},
		{
			name: "failed call material is missing",
			seed: func(t *testing.T, st *state.StateStore) {
				t.Helper()
				taskID, err := st.TaskID()
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(st.ModelCallLogPath(taskID)); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "last material is not a pre-call guard error",
			seed: func(t *testing.T, st *state.StateStore) {
				t.Helper()
				recordParentActionMaterial(t, st, "worker-decision", "error", "transient provider failure: timeout")
			},
		},
		{
			name: "last material phase is not a parent action begin",
			seed: func(t *testing.T, st *state.StateStore) {
				t.Helper()
				recordParentActionMaterial(t, st, "worker-new", "error", preCallInstructionGuardFailureText)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := newAppConfig(t)
			st, err := state.NewStateStore(cfg)
			if err != nil {
				t.Fatal(err)
			}
			seedParentActionSessions(t, st)
			seedDecisionLeftover(t, st)
			recordParentActionMaterial(t, st, "worker-decision", "error", preCallInstructionGuardFailureText)

			command, err := ParseCommand([]string{"--recover-parent-action"})
			if err != nil {
				t.Fatal(err)
			}
			test.seed(t, st)

			if err := Execute(command, cfg, nil, io.Discard, io.Discard); err == nil {
				t.Fatal("foreign condition must be rejected")
			}
			if st.TaskStatus() != state.TaskStatusActive || !st.Exists("pending-decision") {
				t.Fatalf("rejected recovery changed the state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
			}
		})
	}
}

func seedParentActionSessions(t *testing.T, st *state.StateStore) {
	t.Helper()
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("worker.id", "worker-session"); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("worker.ready"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("reviewer.id", "reviewer-session"); err != nil {
		t.Fatal(err)
	}
}

func seedDecisionLeftover(t *testing.T, st *state.StateStore) {
	t.Helper()
	if err := st.Write("last-decision", "decision-body"); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginParentDecision(); err != nil {
		t.Fatal(err)
	}
}

func recordParentActionMaterial(t *testing.T, st *state.StateStore, phase string, outcome string, errText string) {
	t.Helper()
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID:     taskID,
		CallType:   state.CallTypeTask,
		Phase:      phase,
		Role:       state.WorkerRole,
		ModelAlias: "opus",
		Outcome:    outcome,
		Error:      errText,
	})
}

func telemetryRecordCount(t *testing.T, st *state.StateStore) int {
	t.Helper()
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	logs, err := st.ReadModelCallLogs(taskID)
	if err != nil {
		t.Fatal(err)
	}
	return len(logs)
}

func assertParentActionLeftoverRetained(t *testing.T, st *state.StateStore, payloadFile string, payload string, recordsBefore int) {
	t.Helper()
	if got := st.ReadOr(payloadFile, ""); got != payload {
		t.Fatalf("%s = %q want %q", payloadFile, got, payload)
	}
	if got := st.ReadOr("worker.id", ""); got != "worker-session" {
		t.Fatalf("worker.id = %q", got)
	}
	if !st.Exists("worker.ready") {
		t.Fatal("worker.ready was dropped")
	}
	if got := st.ReadOr("reviewer.id", ""); got != "reviewer-session" {
		t.Fatalf("reviewer.id = %q", got)
	}
	if recordsAfter := telemetryRecordCount(t, st); recordsAfter != recordsBefore {
		t.Fatalf("recovery must not add model call telemetry: records = %d want %d", recordsAfter, recordsBefore)
	}
}
