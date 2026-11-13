package app

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

func TestNormalTaskAdmissionRejectsMalformedOrUntrackedHarnessMarker(t *testing.T) {
	for _, tc := range []struct {
		name   string
		marker string
		reason string
	}{
		{name: "content-mismatch", marker: "mismatch", reason: repositoryharness.ReasonContentMismatch},
		{name: "untracked", marker: "untracked", reason: repositoryharness.ReasonUntracked},
		{name: "nonregular", marker: "directory", reason: repositoryharness.ReasonNotRegularFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			runControllerActivationGit(t, root, "init", "-q")
			runControllerActivationGit(t, root, "config", "user.name", "Admission Test")
			runControllerActivationGit(t, root, "config", "user.email", "admission@example.invalid")
			writeAppTestFile(t, root, "README.md", "foreign project\n")
			if tc.marker == "mismatch" {
				writeAppTestFile(t, root, repositoryharness.MarkerPath, "wrong protocol\n")
			}
			runControllerActivationGit(t, root, "add", ".")
			runControllerActivationGit(t, root, "commit", "-qm", "base")
			switch tc.marker {
			case "untracked":
				writeAppTestFile(t, root, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
			case "directory":
				if err := os.Mkdir(filepath.Join(root, repositoryharness.MarkerPath), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			decision, err := repositoryharness.Evaluate(root)
			if err != nil || decision.Active || decision.Reason != tc.reason {
				t.Fatalf("incorrect harness fixture: decision=%+v err=%v", decision, err)
			}
			cfg := taskAdmissionTestConfig(t, root)
			if exists, err := controller.Exists(cfg); err != nil || exists {
				t.Fatalf("unexpected initial controller: exists=%v err=%v", exists, err)
			}
			_, err = activateWorkflowConfig(Command{Mode: ModeNewTask}, cfg)
			if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("inactive harness admission must fail explicitly without reading the Plan: %v", err)
			}
			if exists, err := controller.Exists(cfg); err != nil || exists {
				t.Fatalf("rejected admission created controller: exists=%v err=%v", exists, err)
			}
			var output bytes.Buffer
			err = runEntry([]string{"--authority", "controller-activate"}, func() (config.AppConfig, error) { return cfg, nil }, nil, bytes.NewReader(nil), &output, io.Discard)
			if err == nil || !strings.Contains(err.Error(), "requires active repository harness") || output.Len() != 0 {
				t.Fatalf("explicit activation must still reject inactive harness: %v output=%q", err, output.String())
			}
		})
	}
}

func TestCommittedPlanDoesNotActivateRepositoryHarnessWithoutMarker(t *testing.T) {
	root := t.TempDir()
	runControllerActivationGit(t, root, "init", "-q")
	runControllerActivationGit(t, root, "config", "user.name", "Admission Test")
	runControllerActivationGit(t, root, "config", "user.email", "admission@example.invalid")
	writeAppTestFile(t, root, "IMPLEMENTATION_PLAN.local.md", "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n")
	writeAppTestFile(t, root, "IMPLEMENTATION_TASKS/root.md", "# root\n\n## Contract\n\nfixture\n\n## Dependencies\n\nnone\n")
	runControllerActivationGit(t, root, "add", ".")
	runControllerActivationGit(t, root, "commit", "-qm", "base")
	decision, err := repositoryharness.Evaluate(root)
	if err != nil || decision.Active || decision.Reason != repositoryharness.ReasonAbsent {
		t.Fatalf("committed Plan must not imply harness opt-in: decision=%+v err=%v", decision, err)
	}
	cfg := taskAdmissionTestConfig(t, root)
	var output bytes.Buffer
	err = runEntry([]string{"--authority", "controller-activate"}, func() (config.AppConfig, error) { return cfg, nil }, nil, bytes.NewReader(nil), &output, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "requires active repository harness") || output.Len() != 0 {
		t.Fatalf("explicit project harness activation must remain opt-in: err=%v output=%q", err, output.String())
	}
}

func TestNormalTaskAdmissionDoesNotTreatRemovedTrackedMarkerAsOptOut(t *testing.T) {
	for _, staged := range []bool{false, true} {
		name := "worktree-only"
		if staged {
			name = "staged-deletion"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			runControllerActivationGit(t, root, "init", "-q")
			runControllerActivationGit(t, root, "config", "user.name", "Admission Test")
			runControllerActivationGit(t, root, "config", "user.email", "admission@example.invalid")
			writeAppTestFile(t, root, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
			writeAppTestFile(t, root, "IMPLEMENTATION_PLAN.local.md", "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n")
			writeAppTestFile(t, root, "IMPLEMENTATION_TASKS/root.md", "# root\n\n## Contract\n\nfixture\n\n## Dependencies\n\nnone\n")
			runControllerActivationGit(t, root, "add", ".")
			runControllerActivationGit(t, root, "commit", "-qm", "base")
			if staged {
				runControllerActivationGit(t, root, "rm", "--cached", repositoryharness.MarkerPath)
			}
			if err := os.Remove(filepath.Join(root, repositoryharness.MarkerPath)); err != nil {
				t.Fatal(err)
			}
			cfg := taskAdmissionTestConfig(t, root)
			if _, err := activateWorkflowConfig(Command{Mode: ModeNewTask}, cfg); err == nil || !strings.Contains(err.Error(), repositoryharness.ReasonAbsent) {
				t.Fatalf("tracked marker deletion must remain fail-closed: %v", err)
			}
			if exists, err := controller.Exists(cfg); err != nil || exists {
				t.Fatalf("marker deletion created controller: exists=%v err=%v", exists, err)
			}
		})
	}
}

func TestNormalTaskAdmissionNonGitHasExplicitBoundary(t *testing.T) {
	root := t.TempDir()
	writeAppTestFile(t, root, "README.md", "not a Git repository\n")
	cfg := taskAdmissionTestConfig(t, root)
	_, err := activateWorkflowConfig(Command{Mode: ModeNewTask}, cfg)
	if err == nil || !strings.Contains(err.Error(), "requires a Git repository") || !strings.Contains(err.Error(), "non-Git task authority") {
		t.Fatalf("non-Git admission must report unsupported authority without reading a Plan: %v", err)
	}
}

func TestNormalTaskAdmissionKeepsCommittedHarnessAuthority(t *testing.T) {
	root := t.TempDir()
	runControllerActivationGit(t, root, "init", "-q")
	runControllerActivationGit(t, root, "config", "user.name", "Admission Test")
	runControllerActivationGit(t, root, "config", "user.email", "admission@example.invalid")
	writeAppTestFile(t, root, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
	writeAppTestFile(t, root, "IMPLEMENTATION_PLAN.local.md", "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n")
	writeAppTestFile(t, root, "IMPLEMENTATION_TASKS/root.md", "# root\n\n## Contract\n\nexecution task\n\n## Dependencies\n\nnone\n")
	runControllerActivationGit(t, root, "add", ".")
	runControllerActivationGit(t, root, "commit", "-qm", "base")
	cfg := taskAdmissionTestConfig(t, root)
	if _, err := activateWorkflowConfig(Command{Mode: ModeNewTask}, cfg); err != nil {
		t.Fatalf("active harness normal task admission failed: %v", err)
	}
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if before.LiveLeaseID == "" || before.LiveAttemptID == "" || before.ExecutionTaskRef == nil || before.ExecutionTaskRef.TaskPath != "IMPLEMENTATION_TASKS/root.md" {
		t.Fatalf("normal admission did not bind the committed Task and lease: %+v", before)
	}
	writeAppTestFile(t, root, repositoryharness.MarkerPath, "changed marker\n")
	if _, err := activateWorkflowConfig(Command{Mode: ModeNewTask}, cfg); err == nil || !strings.Contains(err.Error(), repositoryharness.ReasonContentMismatch) {
		t.Fatalf("previously active harness must not become generic after marker alteration: %v", err)
	}
	after, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if after.ControllerGeneration != before.ControllerGeneration || after.LiveLeaseID != before.LiveLeaseID || after.LiveAttemptID != before.LiveAttemptID {
		t.Fatalf("rejected admission changed live controller authority: before=%+v after=%+v", before, after)
	}
}

func TestNormalTaskCLIRejectsNonGitBeforeRunner(t *testing.T) {
	root := t.TempDir()
	writeAppTestFile(t, root, "README.md", "not a Git repository\n")
	cfg := taskAdmissionTestConfig(t, root)
	called := false
	factory := func(_ config.AppConfig, _ *state.StateStore, _ *runner.StopController) workflow.ModelRunner {
		called = true
		return nil
	}
	var stdout, stderr bytes.Buffer
	err := runEntry([]string{"--task", "request"}, func() (config.AppConfig, error) { return cfg, nil }, factory, bytes.NewReader(nil), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "non-Git task authority") || called {
		t.Fatalf("non-Git CLI admission result: err=%v runnerFactoryCalled=%v", err, called)
	}
	if stdout.Len() != 0 {
		t.Fatalf("rejected CLI emitted success output: %q", stdout.String())
	}
}

func TestNormalTaskCLIDispatchPreservesActiveHarnessAuthority(t *testing.T) {
	cfg, rootTask, _, _ := newCanonicalWorkflowCorpus(t)
	scripted := runCanonicalWorkflow(t, cfg, ModeNewTask, "admission fixture", rootTask, "")
	store, err := controller.Open(scripted.cfg)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.LiveAttemptID == "" || head.LiveLeaseID == "" || head.ExecutionTaskRef == nil || head.ExecutionTaskRef.TaskPath != rootTask {
		t.Fatalf("model dispatch did not preserve active committed authority: %+v", head)
	}
}

func taskAdmissionTestConfig(t *testing.T, root string) config.AppConfig {
	t.Helper()
	hash := config.RepoHashFor(root)
	return config.AppConfig{RepoRoot: root, RepoHash: hash, RepoShort: hash[:12], StateBase: filepath.Join(t.TempDir(), "state", "sessions")}
}

func TestNewTaskAdmissionStartsUncommittedRegisteredTaskFromIdle(t *testing.T) {
	for _, registered := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-task", true: "registered"}[registered], func(t *testing.T) {
			root := t.TempDir()
			runControllerActivationGit(t, root, "init", "-q")
			runControllerActivationGit(t, root, "config", "user.name", "Admission Test")
			runControllerActivationGit(t, root, "config", "user.email", "admission@example.invalid")
			writeAppTestFile(t, root, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
			writeAppTestFile(t, root, "IMPLEMENTATION_PLAN.local.md", "## ACTIVE\n\n## NEXT\n\n## BLOCKED\n")
			runControllerActivationGit(t, root, "add", ".")
			runControllerActivationGit(t, root, "commit", "-qm", "idle base")
			base, err := controller.CaptureWorkspaceSnapshot(root)
			if err != nil {
				t.Fatal(err)
			}
			path := "IMPLEMENTATION_TASKS/registered.md"
			if registered {
				writeAppTestFile(t, root, "IMPLEMENTATION_PLAN.local.md", "## ACTIVE\n\n- `"+path+"`\n\n## NEXT\n\n## BLOCKED\n")
				writeAppTestFile(t, root, path, "# registered\n\n## Contract\n\nrequested work\n\n## External feasibility\n\nstatus: not-applicable\n\n## Dependencies\n\nnone\n")
			}
			cfg := taskAdmissionTestConfig(t, root)
			_, err = activateWorkflowConfig(Command{Mode: ModeNewTask, Payload: "oral request"}, cfg)
			store, openErr := controller.Open(cfg)
			if openErr != nil {
				t.Fatal(openErr)
			}
			head, headErr := store.LoadHead()
			if headErr != nil {
				t.Fatal(headErr)
			}
			if !registered {
				if err == nil || !controller.IsPristine(head) || head.LiveAttemptID != "" || head.LiveLeaseID != "" {
					t.Fatalf("empty Task created execution authority: %#v %v", head, err)
				}
				return
			}
			if err != nil || head.ExecutionTaskRef == nil || head.ExecutionTaskRef.TaskPath != path || head.LiveLeaseID == "" {
				t.Fatalf("registered Task was not started: %#v %v", head, err)
			}
			after, err := controller.CaptureWorkspaceSnapshot(root)
			if err != nil || after.Head != base.Head {
				t.Fatal("Task registration created a separate public commit")
			}
		})
	}
}
