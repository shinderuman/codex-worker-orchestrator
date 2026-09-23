package parentactioncmd

import (
	"bytes"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const improvementDispositionTestCallID = "44444444-4444-4444-8444-444444444444"

func TestImprovementSignalRejectDispositionDoesNotRequireRepositoryTaskBinding(t *testing.T) {
	cfg, st := newImprovementDispositionTestState(t)
	if err := st.Remove("active-task"); err != nil {
		t.Fatal(err)
	}

	if err := runImprovementDispositionTest(cfg, improvementDispositionTestCallID, state.ImprovementSignalDispositionReject, ""); err != nil {
		t.Fatal(err)
	}
	registrations, err := st.CurrentPendingDefectRegistrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(registrations) != 0 {
		t.Fatalf("reject created defect registration: %#v", registrations)
	}
}

func TestImprovementSignalRejectsReplacedSourceCall(t *testing.T) {
	cfg, st := newImprovementDispositionTestState(t)
	taskID := st.ReadOr("task.id", "")
	now := time.Now().UTC()
	newCallID := "55555555-5555-4555-8555-555555555555"
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID:             taskID,
		CallType:           state.CallTypeTask,
		CallID:             newCallID,
		StartedAt:          now,
		CompletedAt:        now,
		Outcome:            "invalid_packet",
		PacketRejectReason: "targets-none",
	})

	if err := runImprovementDispositionTest(cfg, improvementDispositionTestCallID, state.ImprovementSignalDispositionReject, ""); err == nil {
		t.Fatal("stale source call disposition was accepted after signal replacement")
	}
	if err := runImprovementDispositionTest(cfg, newCallID, state.ImprovementSignalDispositionReject, ""); err != nil {
		t.Fatalf("current source call disposition failed: %v", err)
	}
}

func TestRecoveredInvalidPacketRejectsStaleDisposition(t *testing.T) {
	cfg, st := newImprovementDispositionTestState(t)
	taskID := st.ReadOr("task.id", "")
	now := time.Now().UTC()
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID:      taskID,
		CallType:    state.CallTypeTask,
		CallID:      "66666666-6666-4666-8666-666666666666",
		StartedAt:   now,
		CompletedAt: now,
		Outcome:     "success",
	})

	if err := runImprovementDispositionTest(cfg, improvementDispositionTestCallID, state.ImprovementSignalDispositionReject, ""); err == nil {
		t.Fatal("stale historical invalid packet accepted a new disposition")
	}
}

func TestImprovementSignalIdempotentReplayRepairsDispositionEvent(t *testing.T) {
	cfg, st := newImprovementDispositionTestState(t)
	signal, err := st.PendingImprovementSignal()
	if err != nil || signal == nil {
		t.Fatalf("pending signal = %#v err=%v", signal, err)
	}
	if _, created, err := st.RecordImprovementSignalDisposition(
		*signal,
		string(state.ImprovementSignalDispositionReject),
		"",
	); err != nil || !created {
		t.Fatalf("seed durable disposition: created=%t err=%v", created, err)
	}

	if err := runImprovementDispositionTest(cfg, signal.SourceCallID, state.ImprovementSignalDispositionReject, ""); err != nil {
		t.Fatal(err)
	}
	logs, err := st.ReadModelCallLogs(st.ReadOr("task.id", ""))
	if err != nil {
		t.Fatal(err)
	}
	last := logs[len(logs)-1]
	if last.CallType != state.CallTypeEvent || last.Phase != actionImprovementDisposition || last.Outcome != string(state.ImprovementSignalDispositionReject) {
		t.Fatalf("replay did not repair disposition event: %#v", last)
	}
}

func TestImprovementSignalEmptySourceKeepsRecordedReplayPath(t *testing.T) {
	cfg, st := newImprovementDispositionTestState(t)
	signal, err := st.PendingImprovementSignal()
	if err != nil || signal == nil {
		t.Fatalf("pending signal = %#v err=%v", signal, err)
	}
	if _, created, err := st.RecordImprovementSignalDisposition(
		*signal,
		string(state.ImprovementSignalDispositionReject),
		"",
	); err != nil || !created {
		t.Fatalf("seed durable disposition: created=%t err=%v", created, err)
	}

	now := time.Now().UTC().Add(time.Second)
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID:             st.ReadOr("task.id", ""),
		CallType:           state.CallTypeTask,
		StartedAt:          now,
		CompletedAt:        now,
		Outcome:            "invalid_packet",
		PacketRejectReason: "schema-invalid",
	})

	if err := runImprovementDispositionTest(cfg, signal.SourceCallID, state.ImprovementSignalDispositionReject, ""); err != nil {
		t.Fatalf("empty-source pending signal blocked idempotent replay: %v", err)
	}
}

func TestImprovementSignalAdoptConnectsExistingDefectRegistrationLifecycle(t *testing.T) {
	cfg, st := newImprovementDispositionTestState(t)
	target := "IMPLEMENTATION_TASKS/adopted-improvement.md"
	if err := runImprovementDispositionTest(cfg, improvementDispositionTestCallID, state.ImprovementSignalDispositionAdopt, target); err != nil {
		t.Fatal(err)
	}
	registrations, err := st.CurrentPendingDefectRegistrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(registrations) != 1 || registrations[0].TaskPath != target {
		t.Fatalf("registrations = %#v", registrations)
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != state.ParentActionBindDefectTask || plan.RequiredActionParameters["task"] != target {
		t.Fatalf("post-adopt plan = %#v", plan)
	}
}

func TestImprovementSignalNonAdoptDispositionsDoNotRegisterTasks(t *testing.T) {
	for _, disposition := range []state.ImprovementSignalDisposition{
		state.ImprovementSignalDispositionExistingOwner,
		state.ImprovementSignalDispositionDuplicate,
		state.ImprovementSignalDispositionAwaitingEvidence,
	} {
		t.Run(string(disposition), func(t *testing.T) {
			cfg, st := newImprovementDispositionTestState(t)
			target := ""
			if state.ImprovementDispositionNeedsTask(disposition) {
				target = "IMPLEMENTATION_TASKS/existing-owner.md"
			}
			if err := runImprovementDispositionTest(cfg, improvementDispositionTestCallID, disposition, target); err != nil {
				t.Fatal(err)
			}
			registrations, err := st.CurrentPendingDefectRegistrations()
			if err != nil {
				t.Fatal(err)
			}
			if len(registrations) != 0 {
				t.Fatalf("%s created registration: %#v", disposition, registrations)
			}
		})
	}
}

func runImprovementDispositionTest(cfg config.AppConfig, sourceCallID string, disposition state.ImprovementSignalDisposition, targetTask string) error {
	args := []string{
		actionImprovementDisposition,
		improvementSignalKindOption, state.ImprovementSignalInvalidPacket,
		improvementSignalCallIDOption, sourceCallID,
		improvementDispositionOption, string(disposition),
	}
	if targetTask != "" {
		args = append(args, improvementTaskOption, targetTask)
	}
	return executeImprovementDisposition(cfg, args, &bytes.Buffer{})
}

func newImprovementDispositionTestState(t *testing.T) (config.AppConfig, *state.StateStore) {
	t.Helper()
	cfg, st := newParentActionTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("active-task", "IMPLEMENTATION_TASKS/source.md"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID:             taskID,
		CallType:           state.CallTypeTask,
		CallID:             improvementDispositionTestCallID,
		StartedAt:          now,
		CompletedAt:        now,
		Outcome:            "invalid_packet",
		PacketRejectReason: "schema-invalid",
	})
	return cfg, st
}
