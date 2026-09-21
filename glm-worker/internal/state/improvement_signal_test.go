package state

import (
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestPendingImprovementSignalRequiresDispositionPerSignal(t *testing.T) {
	st := newImprovementSignalTestStore(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	firstCallID := "11111111-1111-4111-8111-111111111111"
	st.RecordModelCallLog(ModelCallLog{
		TaskID:             taskID,
		CallType:           CallTypeTask,
		CallID:             firstCallID,
		StartedAt:          now,
		CompletedAt:        now,
		Outcome:            "invalid_packet",
		PacketRejectReason: "schema-invalid",
	})

	first, err := st.PendingImprovementSignal()
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || first.Kind != ImprovementSignalInvalidPacket || first.Count != 1 || first.SourceCallID != firstCallID || first.Reason != "schema-invalid" {
		t.Fatalf("pending signal = %#v", first)
	}

	record, created, err := st.RecordImprovementSignalDisposition(*first, string(ImprovementSignalDispositionReject), "")
	if err != nil || !created {
		t.Fatalf("record disposition: created=%t err=%v", created, err)
	}
	if record.Disposition != ImprovementSignalDispositionReject || record.SignalCount != 1 || record.SourceCallID != first.SourceCallID {
		t.Fatalf("record = %#v", record)
	}
	if pending, err := st.PendingImprovementSignal(); err != nil || pending != nil {
		t.Fatalf("disposed signal remained pending: %#v err=%v", pending, err)
	}
	if repeated, created, err := st.RecordImprovementSignalDisposition(*first, string(ImprovementSignalDispositionReject), ""); err != nil || created || repeated.SourceCallID != firstCallID {
		t.Fatalf("idempotent disposition failed: record=%#v created=%t err=%v", repeated, created, err)
	}

	secondCallID := "11111111-1111-4111-8111-222222222222"
	st.RecordModelCallLog(ModelCallLog{
		TaskID:             taskID,
		CallType:           CallTypeTask,
		CallID:             secondCallID,
		StartedAt:          now.Add(time.Second),
		CompletedAt:        now.Add(time.Second),
		Outcome:            "invalid_packet",
		PacketRejectReason: "targets-none",
	})
	second, err := st.PendingImprovementSignal()
	if err != nil {
		t.Fatal(err)
	}
	if second == nil || second.SourceCallID != secondCallID || second.Reason != "targets-none" {
		t.Fatalf("second signal = %#v", second)
	}
	if _, created, err := st.RecordImprovementSignalDisposition(*second, string(ImprovementSignalDispositionReject), ""); err != nil || !created {
		t.Fatalf("second disposition: created=%t err=%v", created, err)
	}
	records, err := st.CurrentImprovementSignalDispositions()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("disposition records = %#v", records)
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
	signal, err := st.PendingImprovementSignal()
	if err != nil || signal == nil {
		t.Fatalf("pending signal = %#v err=%v", signal, err)
	}

	if _, _, err := st.RecordImprovementSignalDisposition(*signal, string(ImprovementSignalDispositionAdopt), ""); err == nil {
		t.Fatal("adopt without target task was accepted")
	}
	if _, _, err := st.RecordImprovementSignalDisposition(*signal, string(ImprovementSignalDispositionReject), "IMPLEMENTATION_TASKS/extra.md"); err == nil {
		t.Fatal("reject with target task was accepted")
	}
	if _, _, err := st.RecordImprovementSignalDisposition(*signal, "invented", ""); err == nil {
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
