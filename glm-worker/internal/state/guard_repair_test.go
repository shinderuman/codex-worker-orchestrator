package state

import (
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func newGuardRepairStateStore(t *testing.T) *StateStore {
	t.Helper()
	st, err := NewStateStore(config.AppConfig{StateBase: t.TempDir(), RepoHash: "guard-repair-test", RepoRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func guardRepairRecordForTest() GuardRepairRecord {
	return GuardRepairRecord{
		TaskID:         "task-1",
		Phase:          "worker-new",
		Fingerprint:    "fingerprint-1",
		Strategy:       "bounded-guard-source-repair-v1",
		Status:         GuardRepairRequested,
		Failure:        "capture failed",
		RelevantDigest: "digest-before",
	}
}

func TestRequestGuardRepairDoesNotRepeatSameFailureStrategyAndEvidence(t *testing.T) {
	st := newGuardRepairStateStore(t)
	record := guardRepairRecordForTest()
	if err := st.RequestGuardRepair(record); err != nil {
		t.Fatal(err)
	}
	first, err := st.LoadGuardRepairRecord()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if err := st.RequestGuardRepair(record); err != nil {
		t.Fatal(err)
	}
	second, err := st.LoadGuardRepairRecord()
	if err != nil {
		t.Fatal(err)
	}
	if !second.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatalf("same recovery request was persisted again: first=%s second=%s", first.UpdatedAt, second.UpdatedAt)
	}
}

func TestRequestGuardRepairAllowsNewRelevantEvidence(t *testing.T) {
	st := newGuardRepairStateStore(t)
	record := guardRepairRecordForTest()
	if err := st.RequestGuardRepair(record); err != nil {
		t.Fatal(err)
	}
	record.Fingerprint = "fingerprint-2"
	record.RelevantDigest = "digest-after"
	if err := st.RequestGuardRepair(record); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadGuardRepairRecord()
	if err != nil {
		t.Fatal(err)
	}
	if got.Fingerprint != "fingerprint-2" || got.RelevantDigest != "digest-after" || got.Status != GuardRepairRequested {
		t.Fatalf("new evidence did not create a fresh repair request: %#v", got)
	}
}

func TestRequestGuardRepairPreservesReadyRepairForRepairedSource(t *testing.T) {
	st := newGuardRepairStateStore(t)
	record := guardRepairRecordForTest()
	record.Status = GuardRepairReady
	record.RepairedDigest = "digest-repaired"
	if err := st.SaveGuardRepairRecord(record); err != nil {
		t.Fatal(err)
	}
	request := guardRepairRecordForTest()
	request.Fingerprint = "fingerprint-repaired"
	request.RelevantDigest = "digest-repaired"
	if err := st.RequestGuardRepair(request); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadGuardRepairRecord()
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != GuardRepairReady || got.RepairedDigest != "digest-repaired" || got.Fingerprint != record.Fingerprint {
		t.Fatalf("rebuilt resume overwrote ready repair state: %#v", got)
	}
}

func TestGuardRepairCompleteRequiresObservedOriginalResume(t *testing.T) {
	st := newGuardRepairStateStore(t)
	record := guardRepairRecordForTest()
	record.Status = GuardRepairReady
	record.RepairedDigest = "digest-repaired"
	if err := st.SaveGuardRepairRecord(record); err != nil {
		t.Fatal(err)
	}
	record.Status = GuardRepairComplete
	record.OriginalResumeObserved = true
	if err := st.SaveGuardRepairRecord(record); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadGuardRepairRecord()
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != GuardRepairComplete || !got.OriginalResumeObserved {
		t.Fatalf("original resume evidence was not persisted: %#v", got)
	}
}
