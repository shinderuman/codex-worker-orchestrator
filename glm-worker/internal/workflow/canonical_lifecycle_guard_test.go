package workflow

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionunit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestWorkflowEntriesRejectForeignWorkspaceBeforeStateMutation(t *testing.T) {
	for _, mode := range []string{"single", "milestones", "resume"} {
		t.Run(mode, func(t *testing.T) {
			repo := t.TempDir()
			cfg, st := newWorkflowGuardFixture(t, repo)
			original, err := controller.Activate(cfg)
			if err != nil {
				t.Fatal(err)
			}
			lane := filepath.Join(t.TempDir(), "unauthorized-lane")
			runWorkflowGuardGit(t, repo, "worktree", "add", "--detach", lane, "HEAD")
			cfg.RepoRoot = lane
			r := &admissionTestRunner{}
			w := NewWorkflow(cfg, st, r, io.Discard)
			if mode == "resume" {
				if _, err := st.StartNewTask(); err != nil {
					t.Fatal(err)
				}
				if err := st.EnterStop(state.ResumeCheckpoint{Stage: state.ResumeStageWorker, Phase: "worker-new", Role: state.WorkerRole, Model: "test", Prompt: "preserved", Request: "root", StopKind: state.ResumeStopInterrupted}); err != nil {
					t.Fatal(err)
				}
			}
			beforeTask := st.ReadOr("task.id", "")
			beforeStatus := st.TaskStatus()
			switch mode {
			case "single":
				err = w.ExecuteNewTask("unauthorized task")
			case "milestones":
				err = w.ExecuteNewTaskWithMilestones("unauthorized task", []executionunit.MilestoneDefinition{{ID: "first", Scope: "first unit", Acceptance: "first result"}, {ID: "second", Scope: "second unit", Acceptance: "second result"}})
			default:
				err = w.ExecuteResume()
			}
			if err == nil {
				t.Fatal("foreign workspace acquired workflow authority")
			}
			if r.calls != 0 || st.ReadOr("task.id", "") != beforeTask || st.TaskStatus() != beforeStatus {
				t.Fatal("rejected workflow consumed state or invoked a model")
			}
			store, err := controller.Open(cfg)
			if err != nil {
				t.Fatal(err)
			}
			head, err := store.LoadHead()
			if err != nil || head.LiveAttemptID != original.Attempt.AttemptID || head.LiveLeaseID != original.Lease.LeaseID {
				t.Fatalf("foreign workspace minted another attempt or lease: %#v %v", head, err)
			}
		})
	}
}

func TestWorkflowParkRemainsRetired(t *testing.T) {
	cfg, st := newWorkflowGuardFixture(t, t.TempDir())
	w := NewWorkflow(cfg, st, nil, io.Discard)
	if err := w.ExecutePark(io.Discard); err == nil || !strings.Contains(err.Error(), "canonical controller cutover") {
		t.Fatalf("park error = %v", err)
	}
}

func newWorkflowGuardFixture(t *testing.T, repo string) (config.AppConfig, *state.StateStore) {
	t.Helper()
	runWorkflowGuardGit(t, repo, "init", "-q")
	runWorkflowGuardGit(t, repo, "config", "user.email", "workflow-guard@example.invalid")
	runWorkflowGuardGit(t, repo, "config", "user.name", "Workflow Guard Test")
	if err := os.MkdirAll(filepath.Join(repo, "IMPLEMENTATION_TASKS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte("## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_TASKS", "root.md"), []byte("# root\n\n## Contract\n\nroot\n\n## Dependencies\n\nnone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runWorkflowGuardGit(t, repo, "add", ".")
	runWorkflowGuardGit(t, repo, "commit", "-q", "-m", "base")
	hash := config.RepoHashFor(repo)
	cfg := config.AppConfig{RepoRoot: repo, RepoHash: hash, RepoShort: hash[:12], StateBase: filepath.Join(t.TempDir(), "sessions"), WorktreeBase: filepath.Join(t.TempDir(), "worktrees")}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, st
}

func runWorkflowGuardGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
