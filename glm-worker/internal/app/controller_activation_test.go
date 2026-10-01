package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
)

func TestRunEntryControllerActivationRequiresExplicitMachineAction(t *testing.T) {
	root := t.TempDir()
	runControllerActivationGit(t, root, "init", "-q")
	runControllerActivationGit(t, root, "config", "user.email", "controller-activation@example.invalid")
	runControllerActivationGit(t, root, "config", "user.name", "Controller Activation Test")
	writeAppTestFile(t, root, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
	writeAppTestFile(t, root, "IMPLEMENTATION_PLAN.local.md", "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n")
	writeAppTestFile(t, root, "IMPLEMENTATION_TASKS/root.md", "# root\n\n## Contract\n\ncontroller activation task\n\n## Dependencies\n\nnone\n")
	runControllerActivationGit(t, root, "add", ".")
	runControllerActivationGit(t, root, "commit", "-q", "-m", "base")

	stateBase := filepath.Join(t.TempDir(), "state", "sessions")
	hash := config.RepoHashFor(root)
	cfg := config.AppConfig{RepoRoot: root, RepoHash: hash, RepoShort: hash[:12], StateBase: stateBase}
	exists, err := controller.Exists(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("controller store exists before explicit activation")
	}

	var stdout bytes.Buffer
	err = runEntry(
		[]string{"--authority", "controller-activate"},
		func() (config.AppConfig, error) { return cfg, nil },
		nil,
		bytes.NewReader(nil),
		&stdout,
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	var output controllerActivationOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.ControllerGeneration == 0 || output.AttemptID == "" || output.LeaseID == "" || output.ProjectSnapshotID == "" {
		t.Fatalf("controller activation output is incomplete: %#v", output)
	}
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if controller.IsPristine(head) || head.LiveAttemptID != output.AttemptID || head.LiveLeaseID != output.LeaseID {
		t.Fatalf("explicit activation did not install live authority: %#v", head)
	}
}

func runControllerActivationGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
