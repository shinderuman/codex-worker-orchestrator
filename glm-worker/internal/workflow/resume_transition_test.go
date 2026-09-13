package workflow

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestActivateResumeRecordsGuardRepairTransactionObservation(t *testing.T) {
	st, taskID, checkpoint, record := prepareGuardRepairResumeTransition(t)
	attemptID := "66666666-6666-4666-8666-666666666666"
	if _, err := st.PrepareGuardRepairResume(record, checkpoint, attemptID); err != nil {
		t.Fatal(err)
	}
	t.Setenv(state.GuardRepairParentActionEnv, state.GuardRepairRebuiltResume)
	t.Setenv(state.GuardRepairResumeAttemptEnv, attemptID)

	workflow := &Workflow{state: st}
	if err := workflow.activateResume(checkpoint); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusActive {
		t.Fatalf("status = %s want active", st.TaskStatus())
	}
	observed, err := st.VerifyGuardRepairResume(taskID, attemptID, checkpoint)
	if err != nil {
		t.Fatalf("rebuilt resume did not record transaction-owned lifecycle evidence: %v", err)
	}
	if !observed.OriginalResumeObserved {
		t.Fatal("rebuilt resume observation was not persisted")
	}
}

func TestActivateResumeRejectsGuardRepairAttemptOutsideRebuiltResume(t *testing.T) {
	st, _, checkpoint, _ := prepareGuardRepairResumeTransition(t)
	t.Setenv(state.GuardRepairParentActionEnv, state.GuardRepairParentActionResume)
	t.Setenv(state.GuardRepairResumeAttemptEnv, "77777777-7777-4777-8777-777777777777")

	workflow := &Workflow{state: st}
	if err := workflow.activateResume(checkpoint); err == nil {
		t.Fatal("guard repair attempt token was accepted outside rebuilt resume")
	}
	if st.TaskStatus() != state.TaskStatusGuardRecoverable {
		t.Fatalf("rejected attempt changed task status: %s", st.TaskStatus())
	}
}

func TestActivateResumeRejectsRebuiltResumeWithoutAttempt(t *testing.T) {
	st, _, checkpoint, _ := prepareGuardRepairResumeTransition(t)
	t.Setenv(state.GuardRepairParentActionEnv, state.GuardRepairRebuiltResume)
	t.Setenv(state.GuardRepairResumeAttemptEnv, "")

	workflow := &Workflow{state: st}
	if err := workflow.activateResume(checkpoint); err == nil {
		t.Fatal("rebuilt resume without attempt ID was accepted")
	}
	if st.TaskStatus() != state.TaskStatusGuardRecoverable {
		t.Fatalf("missing attempt changed task status: %s", st.TaskStatus())
	}
}

func prepareGuardRepairResumeTransition(t *testing.T) (*state.StateStore, string, state.ResumeCheckpoint, state.GuardRepairRecord) {
	t.Helper()
	cfg := config.AppConfig{RepoRoot: t.TempDir(), StateBase: t.TempDir(), RepoHash: "guard-repair-resume-transition"}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := state.ResumeCheckpoint{
		Stage:        state.ResumeStageWorker,
		Phase:        "worker-new",
		Role:         state.WorkerRole,
		Model:        "opus",
		Prompt:       "prompt",
		Request:      "request",
		StopKind:     state.ResumeStopGuardRecoverable,
		GuardFailure: "guard failure",
		StopGitSnapshot: &state.GitSnapshot{
			Head:        "head",
			IndexDigest: "index",
		},
	}
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusGuardRecoverable); err != nil {
		t.Fatal(err)
	}
	record := state.GuardRepairRecord{
		TaskID:         taskID,
		Phase:          checkpoint.Phase,
		Fingerprint:    "fingerprint",
		Strategy:       "bounded-guard-source-repair-v1",
		Status:         state.GuardRepairReady,
		Failure:        checkpoint.GuardFailure,
		RelevantDigest: "digest-before",
		RepairedDigest: "digest-repaired",
	}
	if err := st.SaveGuardRepairRecord(record); err != nil {
		t.Fatal(err)
	}
	return st, taskID, checkpoint, record
}
