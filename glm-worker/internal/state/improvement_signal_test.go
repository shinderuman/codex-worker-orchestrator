package state

import (
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestPendingImprovementSignalRequiresDispositionOncePerTask(t *testing.T) {
	st := newImprovementSignalTestStore(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	st.RecordModelCallLog(ModelCallLog{
		TaskID:             taskID,
		CallType:           CallTypeTask,
		CallID:             "11111111-1111-4111-8111-111111111111",
		StartedAt:          now,
		CompletedAt:        now,
		Outcome:            "invalid_packet",
		PacketRejectReason: "schema-invalid",
	})

	signal, err := st.PendingImprovementSignal()
	if err != nil {
		t.Fatal(err)
	}
	if signal == nil || signal.Kind != ImprovementSignalInvalidPacket || signal.Count != 1 || signal.Reason != "schema-invalid" {
		t.Fatalf("pending signal = %#v", signal)
	}

	record, created, err := st.RecordImprovementSignalDisposition(signal.Kind, string(ImprovementSignalDispositionReject), "")
	if err != nil || !created {
		t.Fatalf("record disposition: created=%t err=%v", created, err)
	}
	if record.Disposition != ImprovementSignalDispositionReject || record.SignalCount != 1 || record.SourceCallID != signal.SourceCallID {
		t.Fatalf("record = %#v", record)
	}
	if pending, err := st.PendingImprovementSignal(); err != nil || pending != nil {
		t.Fatalf("disposed signal remained pending: %#v err=%v", pending, err)
	}
	if repeated, created, err := st.RecordImprovementSignalDisposition(signal.Kind, string(ImprovementSignalDispositionReject), ""); err != nil || created || repeated.Disposition != ImprovementSignalDispositionReject {
		t.Fatalf("idempotent disposition failed: record=%#v created=%t err=%v", repeated, created, err)
	}
}

func TestImprovementSignalDispositionTaskRequirementsAreBounded(t *testing.T) {
	st := newImprovementSignalTestStore(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	st.RecordModelCallLog(ModelCallLog{TaskID: taskID, CallType: CallTypeTask, CallID: "22222222-2222-4222-8222-222222222222", StartedAt: now, CompletedAt: now, Outcome: "invalid_packet", PacketRejectReason: "schema-invalid"})

	if _, _, err := st.RecordImprovementSignalDisposition(ImprovementSignalInvalidPacket, string(ImprovementSignalDispositionAdopt), ""); err == nil {
		t.Fatal("adopt without target task was accepted")
	}
	if _, _, err := st.RecordImprovementSignalDisposition(ImprovementSignalInvalidPacket, string(ImprovementSignalDispositionReject), "IMPLEMENTATION_TASKS/extra.md"); err == nil {
		t.Fatal("reject with target task was accepted")
	}
	if _, _, err := st.RecordImprovementSignalDisposition(ImprovementSignalInvalidPacket, "invented", ""); err == nil {
		t.Fatal("unknown disposition was accepted")
	}
}

func TestPendingImprovementSignalDoesNotMakeUnreadableTelemetryAuthoritative(t *testing.T) {
	st := newImprovementSignalTestStore(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("telemetry/"+taskID+".jsonl", "{not-json\n"); err != nil {
		t.Fatal(err)
	}
	if signal, err := st.PendingImprovementSignal(); err != nil || signal != nil {
		t.Fatalf("unreadable observational telemetry became lifecycle authority: signal=%#v err=%v", signal, err)
	}
}

func newImprovementSignalTestStore(t *testing.T) *StateStore {
	t.Helper()
	st, err := NewStateStore(config.AppConfig{
		RepoRoot:  t.TempDir(),
		RepoHash:  strings.Repeat("b", 64),
		StateBase: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return st
}
