package parentactioncmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestFinalizationUsesLiveExecutionConfigAndCorrespondingModuleDirectory(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if data, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s %v", args, data, err)
		}
	}
	git("init", "-q")
	git("config", "user.name", "finalization test")
	git("config", "user.email", "finalization@example.invalid")
	for path, data := range map[string]string{
		"IMPLEMENTATION_PLAN.local.md":  "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n\n## NEXT\n\n- `IMPLEMENTATION_TASKS/child.md`\n",
		"IMPLEMENTATION_TASKS/root.md":  "# root\n\n## Contract\n\nroot\n\n## Dependencies\n\nnone\n",
		"IMPLEMENTATION_TASKS/child.md": "# child\n\n## Contract\n\nchild\n\n## Dependencies\n\nnone\n",
		"glm-worker/go.mod":             "module example.invalid/test\n\ngo 1.24\n",
	} {
		full := filepath.Join(repo, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("add", ".")
	git("commit", "-qm", "fixture")
	cfg := config.AppConfig{RepoRoot: repo, StateBase: filepath.Join(t.TempDir(), "sessions")}
	a, err := controller.Activate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.LoadProjectSnapshot(a.Head.ProjectSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	var child controller.SemanticTaskRef
	for _, task := range project.Tasks {
		if task.TaskPath == "IMPLEMENTATION_TASKS/child.md" {
			child = task
		}
	}
	finding, err := store.ObserveFinding(a, controller.FindingObservationInput{Producer: "parent", ProofClass: controller.FindingProofUnverified, ProblemKey: "fixture child"})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := store.ResolveFindingWithProjectAuthority(finding.FindingID, controller.FindingDecision{Kind: controller.FindingDecisionIndependentBlocking, TargetTaskRef: &child, BlockingBoundary: "fixture root requires child"})
	if err != nil {
		t.Fatal(err)
	}
	episode := resolved.Episode
	suspended, err := store.SuspendExecution(a, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	materialized, err := store.MaterializeExecution(controller.MaterializeExecutionInput{ExpectedGeneration: suspended.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: episode.Revision})
	if err != nil {
		t.Fatal(err)
	}
	lane := materialized.Admission.Workspace.Root
	cfg.RepoRoot = lane
	cfg, err = controller.WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("repo-root", lane); err != nil {
		t.Fatal(err)
	}
	primary := cfg
	primary.RepoRoot = repo
	if _, managed, failure := managedFinalizationState(primary, repo); managed || failure == nil || failure.Reason != "state_repository_mismatch" {
		t.Fatal("fixture did not reproduce primary/live runtime binding mismatch")
	}
	bound, dir, err := finalizationExecutionContext(primary, filepath.Join(repo, "glm-worker"))
	if err != nil || bound.RepoRoot != lane || dir != filepath.Join(lane, "glm-worker") || bound.RepoHash != cfg.RepoHash {
		t.Fatalf("finalization execution context = %s %s %v", bound.RepoRoot, dir, err)
	}
	if _, managed, failure := managedFinalizationState(bound, bound.RepoRoot); !managed || failure != nil {
		t.Fatalf("live finalization runtime unavailable: %v", failure)
	}
	if _, _, err := finalizationExecutionContext(primary, t.TempDir()); err == nil {
		t.Fatal("foreign working directory mapped into execution lane")
	}
}
