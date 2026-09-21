package parentactioncmd

import (
	"bytes"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestImprovementSignalBlocksOtherParentActionsUntilDisposition(t *testing.T) {
	cfg, st := newImprovementDispositionTestState(t)
	if err := requireImprovementSignalDisposition(cfg, actionAccept); err == nil {
		t.Fatal("parent action was admitted before improvement disposition")
	}
	if err := requireImprovementSignalDisposition(cfg, actionImprovementDisposition); err != nil {
		t.Fatalf("disposition action was blocked: %v", err)
	}

	var out bytes.Buffer
	if err := executeImprovementDisposition(cfg, []string{
		actionImprovementDisposition,
		improvementSignalKindOption, state.ImprovementSignalInvalidPacket,
		improvementDispositionOption, string(state.ImprovementSignalDispositionReject),
	}, &out); err != nil {
		t.Fatal(err)
	}
	if err := requireImprovementSignalDisposition(cfg, actionAccept); err != nil {
		t.Fatalf("parent action remained blocked after disposition: %v", err)
	}
	registrations, err := st.CurrentPendingDefectRegistrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(registrations) != 0 {
		t.Fatalf("reject created defect registration: %#v", registrations)
	}
}

func TestRecoveredInvalidPacketDoesNotBecomeStaleActionGate(t *testing.T) {
	cfg, st := newImprovementDispositionTestState(t)
	taskID := st.ReadOr("task.id", "")
	now := time.Now().UTC()
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID:      taskID,
		CallType:    state.CallTypeTask,
		CallID:      "55555555-5555-4555-8555-555555555555",
		StartedAt:   now,
		CompletedAt: now,
		Outcome:     "success",
	})

	if err := requireImprovementSignalDisposition(cfg, actionAccept); err != nil {
		t.Fatalf("recovered invalid packet still blocked parent action: %v", err)
	}
	if err := executeImprovementDisposition(cfg, []string{
		actionImprovementDisposition,
		improvementSignalKindOption, state.ImprovementSignalInvalidPacket,
		improvementDispositionOption, string(state.ImprovementSignalDispositionReject),
	}, &bytes.Buffer{}); err == nil {
		t.Fatal("stale historical invalid packet accepted a new disposition")
	}
}

func TestImprovementSignalAdoptConnectsExistingDefectRegistrationLifecycle(t *testing.T) {
	cfg, st := newImprovementDispositionTestState(t)
	target := "IMPLEMENTATION_TASKS/adopted-improvement.md"
	var out bytes.Buffer
	if err := executeImprovementDisposition(cfg, []string{
		actionImprovementDisposition,
		improvementSignalKindOption, state.ImprovementSignalInvalidPacket,
		improvementDispositionOption, string(state.ImprovementSignalDispositionAdopt),
		improvementTaskOption, target,
	}, &out); err != nil {
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
			args := []string{
				actionImprovementDisposition,
				improvementSignalKindOption, state.ImprovementSignalInvalidPacket,
				improvementDispositionOption, string(disposition),
			}
			if state.ImprovementDispositionNeedsTask(disposition) {
				args = append(args, improvementTaskOption, "IMPLEMENTATION_TASKS/existing-owner.md")
			}
			if err := executeImprovementDisposition(cfg, args, &bytes.Buffer{}); err != nil {
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
		CallID:             "44444444-4444-4444-8444-444444444444",
		StartedAt:          now,
		CompletedAt:        now,
		Outcome:            "invalid_packet",
		PacketRejectReason: "schema-invalid",
	})
	return cfg, st
}
