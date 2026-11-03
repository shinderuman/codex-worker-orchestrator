package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRetainedMaterializedReviewRequiresCurrentTaskAndSource(t *testing.T) {
	t.Parallel()
	cfg, runtime := newWorkflowGuardFixture(t, t.TempDir())
	admission, err := controller.Activate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	binding := state.ControllerRuntimeBinding{AttemptID: admission.Attempt.AttemptID, TaskPath: admission.Attempt.SemanticTaskRef.TaskPath, TaskContractDigest: admission.Attempt.SemanticTaskRef.ContractDigest}
	if err := runtime.SaveControllerRuntimeBinding(binding); err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.CaptureGitSnapshot(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.SaveWorkerEndSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := verifyRetainedMaterializedReview(cfg, admission.Attempt); err != nil {
		t.Fatal(err)
	}
	foreign := binding
	foreign.TaskPath = "IMPLEMENTATION_TASKS/foreign.md"
	if err := runtime.SaveControllerRuntimeBinding(foreign); err != nil {
		t.Fatal(err)
	}
	if err := verifyRetainedMaterializedReview(cfg, admission.Attempt); err == nil {
		t.Fatal("foreign Task binding reused retained review")
	}
	if err := runtime.SaveControllerRuntimeBinding(binding); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, "changed.txt"), []byte("changed source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyRetainedMaterializedReview(cfg, admission.Attempt); err == nil {
		t.Fatal("stale source reused retained review")
	}
}
