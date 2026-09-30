package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestResetDispositionCommandParsing(t *testing.T) {
	cmd, err := ParseCommand([]string{"--reset", "--disposition", "abandon"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode != ModeReset || cmd.Payload != "abandon" {
		t.Fatalf("parsed reset command = %#v", cmd)
	}
	if _, err := ParseCommand([]string{"--reset", "--disposition", "complete"}); err == nil {
		t.Fatal("unknown reset disposition was accepted")
	}
	if _, err := ParseCommand([]string{"--reset", "abandon"}); err == nil {
		t.Fatal("unscoped reset disposition syntax was accepted")
	}
}

func TestExecuteResetRejectsAwaitingParentCompletionWithoutDisposition(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err = Execute(Command{Mode: ModeReset}, cfg, nil, &out, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "explicit disposition") {
		t.Fatalf("unfinished generic reset did not fail closed: %v", err)
	}
	if got := st.ReadOr("task.id", ""); got != taskID {
		t.Fatalf("rejected reset changed task.id: %q", got)
	}
	if got := st.TaskStatus(); got != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("rejected reset changed status: %q", got)
	}
}

func TestExecuteResetRequiresDispositionBeforePassAcceptance(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err = Execute(Command{Mode: ModeReset}, cfg, nil, &out, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "explicit disposition") {
		t.Fatalf("PASS-pending generic reset did not fail closed: %v", err)
	}
	if got := st.ReadOr("task.id", ""); got != taskID {
		t.Fatalf("rejected PASS-pending reset changed task.id: %q", got)
	}
	if got := st.TaskStatus(); got != state.TaskStatusComplete {
		t.Fatalf("rejected PASS-pending reset changed status: %q", got)
	}

	out.Reset()
	if err := Execute(Command{Mode: ModeReset, Payload: string(state.TaskDispositionAbandon)}, cfg, nil, &out, io.Discard); err != nil {
		t.Fatalf("explicit abandon was rejected for PASS-pending task: %v", err)
	}
	var got dispositionResetOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Disposition != state.TaskDispositionAbandon {
		t.Fatalf("PASS-pending reset disposition = %q", got.Disposition)
	}
}

func TestExecuteResetAbandonAllowsVerifiedNewTaskAdmission(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := Execute(Command{Mode: ModeReset, Payload: "abandon"}, cfg, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var got dispositionResetOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "reset" || got.Disposition != state.TaskDispositionAbandon {
		t.Fatalf("reset output = %#v", got)
	}
	if err := admitParentCommand(Command{Mode: ModeNewTask}, st); err != nil {
		t.Fatalf("new task rejected after explicit disposition: %v", err)
	}
}

func TestExecuteResetRetryRepairsDispositionLifecycle(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ResetWithDisposition(string(state.TaskDispositionAbandon)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(st.TaskLifecycleLogPath(taskID)); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := Execute(Command{Mode: ModeReset}, cfg, nil, &out, io.Discard); err != nil {
		t.Fatalf("reset retry did not repair disposition lifecycle: %v", err)
	}
	var got dispositionResetOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Disposition != state.TaskDispositionAbandon {
		t.Fatalf("reset retry lost disposition provenance: %#v", got)
	}
	if err := st.ValidateResetDispositionForNewTask(); err != nil {
		t.Fatalf("reset retry left new-task admission blocked: %v", err)
	}
}

func TestExecuteResetPreservesRecoverableResetPath(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	checkpoint := state.ResumeCheckpoint{Stage: state.ResumeStageWorker, Phase: "worker", Role: state.WorkerRole, Model: "opus"}
	checkpoint.SetStopKind(state.ResumeStopInterrupted)
	if err := st.EnterStop(checkpoint); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := Execute(Command{Mode: ModeReset}, cfg, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var got dispositionResetOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Disposition != state.TaskDispositionRecovery {
		t.Fatalf("recoverable reset output = %#v", got)
	}
	if err := admitParentCommand(Command{Mode: ModeNewTask}, st); err != nil {
		t.Fatalf("new task rejected after recovery reset: %v", err)
	}
}

func TestNewTaskAdmissionFailsClosedOnUnreadableResetDisposition(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ResetWithDisposition("abandon"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path("task-disposition.json"), []byte("{not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err = admitParentCommand(Command{Mode: ModeNewTask}, st)
	if err == nil || !strings.Contains(err.Error(), "cannot verify reset disposition") {
		t.Fatalf("new task admitted with unreadable disposition: %v", err)
	}
}
