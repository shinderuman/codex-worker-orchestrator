package app

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestGuardRecoveryStatusIsVisibleAndResumable(t *testing.T) {
	st, err := state.NewStateStore(config.AppConfig{
		StateBase: t.TempDir(),
		RepoHash:  "guard-recovery-status",
		RepoRoot:  t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	checkpoint := state.ResumeCheckpoint{
		Stage:            state.ResumeStageWorker,
		Phase:            "worker-new",
		Role:             state.WorkerRole,
		Model:            "worker-model",
		ReportOnly:       false,
		GuardRecoverable: true,
		GuardFailure:     "git authority guard failed: blocked-command",
	}
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusGuardRecoverable); err != nil {
		t.Fatal(err)
	}

	output := statusOutput{}
	if !fillStatusCheckpoint(st, &output) {
		t.Fatal("guard recovery checkpoint must set resume_available")
	}
	if !checkpointResumeAvailable(st) {
		t.Fatal("stop endpoint must expose guard recovery as resumable")
	}
	if got := stopFinishedResult(st.TaskStatus()); got != "terminal" {
		t.Fatalf("stop endpoint result = %q want terminal", got)
	}
	status := taskStatusPtr(st.TaskStatus())
	if status == nil || *status != string(state.TaskStatusGuardRecoverable) {
		t.Fatalf("task status = %v", status)
	}
}
