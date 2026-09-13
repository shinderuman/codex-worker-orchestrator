package state

import (
	"os"
	"testing"
)

func TestGuardRepairResumeProofSurvivesTaskStatsWriteLoss(t *testing.T) {
	st := newLifecycleTestStore(t)
	taskID, checkpoint, record := prepareGuardResumeTransitionTest(t, st)
	attemptID := "11111111-1111-4111-8111-111111111111"
	var err error
	record, err = st.PrepareGuardRepairResume(record, checkpoint, attemptID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != GuardRepairResuming || record.OriginalResumeObserved {
		t.Fatalf("prepared guard repair resume = %#v", record)
	}

	statsPath := st.Path(currentStatsFile)
	if err := os.Remove(statsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(statsPath, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := st.ObserveGuardRepairResume(checkpoint, attemptID); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != TaskStatusActive {
		t.Fatalf("status = %s want active", st.TaskStatus())
	}
	observed, err := st.VerifyGuardRepairResume(taskID, attemptID, checkpoint)
	if err != nil {
		t.Fatalf("transaction-owned resume evidence was not accepted: %v", err)
	}
	if !observed.OriginalResumeObserved || observed.Status != GuardRepairResuming {
		t.Fatalf("resume observation was not persisted in transaction: %#v", observed)
	}
	if info, err := os.Stat(statsPath); err != nil || !info.IsDir() {
		t.Fatalf("TaskStats write-loss fixture was repaired unexpectedly: info=%v err=%v", info, err)
	}
}

func TestVerifyGuardRepairResumeRejectsUnrelatedAttemptAndCheckpoint(t *testing.T) {
	st := newLifecycleTestStore(t)
	taskID, checkpoint, record := prepareGuardResumeTransitionTest(t, st)
	attemptID := "22222222-2222-4222-8222-222222222222"
	if _, err := st.PrepareGuardRepairResume(record, checkpoint, attemptID); err != nil {
		t.Fatal(err)
	}
	if err := st.ObserveGuardRepairResume(checkpoint, attemptID); err != nil {
		t.Fatal(err)
	}

	if _, err := st.VerifyGuardRepairResume(taskID, "33333333-3333-4333-8333-333333333333", checkpoint); err == nil {
		t.Fatal("unrelated resume attempt satisfied transaction proof")
	}
	changed := checkpoint
	changed.Phase = "worker-other"
	if _, err := st.VerifyGuardRepairResume(taskID, attemptID, changed); err == nil {
		t.Fatal("different resume checkpoint satisfied transaction proof")
	}
}

func TestObserveGuardRepairResumeRejectsUnpreparedAttemptWithoutActivating(t *testing.T) {
	st := newLifecycleTestStore(t)
	_, checkpoint, _ := prepareGuardResumeTransitionTest(t, st)
	if err := st.ObserveGuardRepairResume(checkpoint, "44444444-4444-4444-8444-444444444444"); err == nil {
		t.Fatal("unprepared guard repair resume attempt was accepted")
	}
	if st.TaskStatus() != TaskStatusGuardRecoverable {
		t.Fatalf("rejected attempt changed task status: %s", st.TaskStatus())
	}
}

func prepareGuardResumeTransitionTest(t *testing.T, st *StateStore) (string, ResumeCheckpoint, GuardRepairRecord) {
	t.Helper()
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := ResumeCheckpoint{
		Stage:        ResumeStageWorker,
		Phase:        "worker-new",
		Role:         WorkerRole,
		Model:        "opus",
		Prompt:       "prompt",
		Request:      "request",
		StopKind:     ResumeStopGuardRecoverable,
		GuardFailure: "guard failure",
		StopGitSnapshot: &GitSnapshot{
			Head:        "head",
			IndexDigest: "index",
		},
	}
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusGuardRecoverable); err != nil {
		t.Fatal(err)
	}
	record := GuardRepairRecord{
		TaskID:         taskID,
		Phase:          checkpoint.Phase,
		Fingerprint:    "fingerprint",
		Strategy:       "bounded-guard-source-repair-v1",
		Status:         GuardRepairReady,
		Failure:        checkpoint.GuardFailure,
		RelevantDigest: "digest-before",
		RepairedDigest: "digest-repaired",
	}
	if err := st.SaveGuardRepairRecord(record); err != nil {
		t.Fatal(err)
	}
	return taskID, checkpoint, record
}
