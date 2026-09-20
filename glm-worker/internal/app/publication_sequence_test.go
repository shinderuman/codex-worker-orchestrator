package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentHandoffIncludesPublicationSequenceSection(t *testing.T) {
	repoRoot := t.TempDir()
	mustRunPublicationSequenceTestGit(t, repoRoot, "init", "-q", "-b", "main")
	mustRunPublicationSequenceTestGit(t, repoRoot, "config", "user.name", "publication test")
	mustRunPublicationSequenceTestGit(t, repoRoot, "config", "user.email", "publication@example.invalid")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRunPublicationSequenceTestGit(t, repoRoot, "add", "README.md")
	mustRunPublicationSequenceTestGit(t, repoRoot, "commit", "-q", "-m", "initial")

	cfg := config.AppConfig{
		RepoRoot:  repoRoot,
		RepoHash:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		StateBase: filepath.Join(t.TempDir(), "sessions"),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(repositoryharness.ActivationStateKey, repositoryharness.ActivationActiveValue); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}

	output := buildParentHandoff(st)
	if output.Version != parentHandoffVersion {
		t.Fatalf("handoff version = %d", output.Version)
	}
	if !output.Consistent {
		t.Fatalf("handoff unexpectedly inconsistent: %#v", output)
	}
	if output.Publication == nil {
		t.Fatalf("handoff lacks publication section: %#v", output)
	}
	if output.Publication.Stage != "blocked" || output.Publication.Failure == nil ||
		output.Publication.Failure.Reason != "publication_guard_setup_invalid" {
		t.Fatalf("publication section = %#v", output.Publication)
	}

	markHandoffInconsistent(&output, "probe")
	if output.Publication != nil {
		t.Fatalf("inconsistent handoff kept publication section: %#v", output.Publication)
	}
}

func mustRunPublicationSequenceTestGit(t *testing.T, repoRoot string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, output)
	}
}
