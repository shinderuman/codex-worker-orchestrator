package parentactioncmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestCompleteIgnoresMarkerlessForeignProjectProtocol(t *testing.T) {
	cases := []struct {
		name string
		plan string
	}{
		{name: "valid coincidental plan", plan: completeInitialPlan() + "\nforeign metadata\n"},
		{name: "malformed coincidental plan", plan: "foreign plan\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newCompleteFixture(t)
			if err := os.Remove(fixture.st.Path(repositoryharness.ActivationStateKey)); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(fixture.st.Path("active-task")); err != nil {
				t.Fatal(err)
			}
			writePushBindingFile(t, fixture.repo, "IMPLEMENTATION_PLAN.local.md", tc.plan)
			runFinalizationGit(t, fixture.repo, "add", "IMPLEMENTATION_PLAN.local.md")
			runFinalizationGit(t, fixture.repo, "commit", "-q", "-m", "foreign coincidental plan")
			runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")

			output := runCompleteCommand(t, fixture)
			if output.Status != completeStatusComplete || !output.Completed || output.ParentRequest != nil {
				t.Fatalf("foreign completion = %#v", output)
			}
			if output.RemoteSync == nil || output.RemoteSync.State != completeRemoteStateVerified || !output.RemoteSync.PostconditionMet {
				t.Fatalf("foreign remote sync = %#v", output.RemoteSync)
			}
		})
	}
}

func TestCompleteFailsClosedWhenTaskPinLacksActivationPin(t *testing.T) {
	fixture := newCompleteFixture(t)
	if err := os.Remove(fixture.st.Path(repositoryharness.ActivationStateKey)); err != nil {
		t.Fatal(err)
	}
	fixture.commitParentMetadataSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusAwaiting || output.Completed || output.ParentRequest != nil {
		t.Fatalf("activation mismatch completion = %#v", output)
	}
	if output.Failure == nil || output.Failure.Stage != "publication" || output.Failure.Reason != publicationFailureGateMissing ||
		!strings.Contains(output.Failure.Detail, "activation pin") {
		t.Fatalf("activation mismatch failure = %#v", output.Failure)
	}
}

func TestRepositoryAwareResumeDelegatesWithoutCanonicalActivation(t *testing.T) {
	assertRepositoryAwareResumeCutover(t, false)
}

func TestRepositoryAwareResumeDelegatesWithCanonicalActivation(t *testing.T) {
	assertRepositoryAwareResumeCutover(t, true)
}

func assertRepositoryAwareResumeCutover(t *testing.T, activate bool) {
	t.Helper()
	cfg := newCanonicalCutoverConfig(t, activate)
	binDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "canonical-resume-worker-args")
	worker := filepath.Join(binDir, "glm-worker")
	if err := os.WriteFile(worker, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$LEGACY_RESUME_MARKER\"\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LEGACY_RESUME_MARKER", marker)

	err := executeRepositoryAwareResume(cfg, io.Discard, io.Discard, nil)
	if err == nil || !strings.Contains(err.Error(), "99") {
		t.Fatalf("repository-aware resume error = %v", err)
	}
	args, err := os.ReadFile(marker)
	if err != nil || string(args) != "--resume\n" {
		t.Fatalf("canonical resume args = %q: %v", args, err)
	}
	if worktrees := canonicalCutoverWorktrees(t, cfg.RepoRoot); len(worktrees) != 0 {
		t.Fatalf("legacy resume minted repair worktree(s): %v", worktrees)
	}
}

func pinCompleteRepositoryHarnessActive(t *testing.T, st *state.StateStore) {
	t.Helper()
	if err := st.Write(repositoryharness.ActivationStateKey, repositoryharness.ActivationActiveValue); err != nil {
		t.Fatal(err)
	}
}
