package state

import (
	"os"
	"testing"
)

func TestBeginResumeWithEvidenceSurvivesTaskStatsWriteLoss(t *testing.T) {
	st := newLifecycleTestStore(t)
	taskID, checkpoint := prepareGuardResumeTransitionTest(t, st)
	statsPath := st.Path(currentStatsFile)
	if err := os.Remove(statsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(statsPath, 0o700); err != nil {
		t.Fatal(err)
	}

	attemptID := "11111111-1111-4111-8111-111111111111"
	if err := st.BeginResumeWithEvidence(checkpoint, attemptID); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != TaskStatusActive {
		t.Fatalf("status = %s want active", st.TaskStatus())
	}
	if err := st.VerifyResumeTransitionEvidence(taskID, attemptID, checkpoint); err != nil {
		t.Fatalf("canonical resume transition evidence was not accepted: %v", err)
	}
	if info, err := os.Stat(statsPath); err != nil || !info.IsDir() {
		t.Fatalf("TaskStats write-loss fixture was repaired unexpectedly: info=%v err=%v", info, err)
	}
}

func TestVerifyResumeTransitionEvidenceRejectsUnrelatedAttemptAndCheckpoint(t *testing.T) {
	st := newLifecycleTestStore(t)
	taskID, checkpoint := prepareGuardResumeTransitionTest(t, st)
	attemptID := "22222222-2222-4222-8222-222222222222"
	if err := st.BeginResumeWithEvidence(checkpoint, attemptID); err != nil {
		t.Fatal(err)
	}

	if err := st.VerifyResumeTransitionEvidence(taskID, "33333333-3333-4333-8333-333333333333", checkpoint); err == nil {
		t.Fatal("unrelated resume attempt satisfied canonical transition proof")
	}
	changed := checkpoint
	changed.Phase = "worker-other"
	if err := st.VerifyResumeTransitionEvidence(taskID, attemptID, changed); err == nil {
		t.Fatal("different resume checkpoint satisfied canonical transition proof")
	}
}

func TestBeginResumeWithEvidenceRollsBackWhenEvidenceCannotPersist(t *testing.T) {
	st := newLifecycleTestStore(t)
	_, checkpoint := prepareGuardResumeTransitionTest(t, st)
	if err := os.Mkdir(st.Path(resumeTransitionFile), 0o700); err != nil {
		t.Fatal(err)
	}

	err := st.BeginResumeWithEvidence(checkpoint, "44444444-4444-4444-8444-444444444444")
	if err == nil {
		t.Fatal("resume transition succeeded without durable canonical evidence")
	}
	if st.TaskStatus() != TaskStatusGuardRecoverable {
		t.Fatalf("status = %s want guard-recoverable", st.TaskStatus())
	}
	saved, loadErr := st.LoadResumeCheckpoint()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if saved.StopKind != ResumeStopGuardRecoverable || saved.Phase != checkpoint.Phase {
		t.Fatalf("resume stop was not restored: %#v", saved)
	}
}

func prepareGuardResumeTransitionTest(t *testing.T, st *StateStore) (string, ResumeCheckpoint) {
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
	return taskID, checkpoint
}
