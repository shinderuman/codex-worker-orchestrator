package parentactioncmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/app"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPublicationSequenceUsesActualRemoteWhenTrackingRefIsStale(t *testing.T) {
	fixture := newCompleteFixture(t)
	commitCompleteFixtureHarnessMarker(t, fixture)
	if err := state.CaptureGitBaseline(fixture.cfg, fixture.st); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(fixture.repo, "IMPLEMENTATION_TASKS", "active.md")); err != nil {
		t.Fatal(err)
	}
	writePushBindingFile(t, fixture.repo, "IMPLEMENTATION_PLAN.local.md", completePromotedPlan())
	writePushBindingFile(t, fixture.repo, "impl.txt", "published\n")
	runFinalizationGit(t, fixture.repo, "add", "-A")

	candidate, failure := preparePublicationCandidate(fixture.cfg, fixture.st, "stale tracking publication")
	if failure != nil {
		t.Fatalf("prepare failed: %#v", failure)
	}
	promotion := promotePublicationCandidate(fixture.cfg, fixture.st)
	if promotion.Status != publicationPromotionStatusPromoted || promotion.Failure != nil {
		t.Fatalf("promotion = %#v", promotion)
	}
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")

	sequence := app.ProjectPublicationSequence(fixture.repo, fixture.st)
	if sequence.Stage != "complete" || sequence.NextAction == nil {
		t.Fatalf("initial sequence = %#v", sequence)
	}

	externalRepo := filepath.Join(t.TempDir(), "external")
	mustRunRemoteProjectionCommand(t, "git", "clone", "-q", fixture.remote, externalRepo)
	mustRunRemoteProjectionCommand(t, "git", "-C", externalRepo, "config", "user.email", "external@example.invalid")
	mustRunRemoteProjectionCommand(t, "git", "-C", externalRepo, "config", "user.name", "external")
	writePushBindingFile(t, externalRepo, "external.txt", "external advance\n")
	mustRunRemoteProjectionCommand(t, "git", "-C", externalRepo, "add", "external.txt")
	mustRunRemoteProjectionCommand(t, "git", "-C", externalRepo, "commit", "-q", "-m", "external advance")
	mustRunRemoteProjectionCommand(t, "git", "-C", externalRepo, "push", "-q", "origin", "main")

	trackingOID := publicationGitOutput(t, fixture.repo, "rev-parse", "refs/remotes/origin/main")
	if trackingOID != candidate.CommitOID {
		t.Fatalf("tracking ref unexpectedly refreshed: %s != %s", trackingOID, candidate.CommitOID)
	}
	sequence = app.ProjectPublicationSequence(fixture.repo, fixture.st)
	if sequence.Stage != "blocked" || sequence.Failure == nil || sequence.Failure.Reason != "publication_remote_diverged" {
		t.Fatalf("stale tracking sequence = %#v", sequence)
	}
	if sequence.NextAction != nil {
		t.Fatalf("diverged remote returned action: %#v", sequence.NextAction)
	}

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusAwaiting || output.Failure == nil || output.Failure.Stage != "remote_sync" {
		t.Fatalf("complete output = %#v", output)
	}
	if output.NextAction != nil {
		t.Fatalf("remote mismatch looped to next action: %#v", output.NextAction)
	}
}

func mustRunRemoteProjectionCommand(t *testing.T, name string, args ...string) {
	t.Helper()
	command := exec.Command(name, args...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed: %v: %s", name, args, err, output)
	}
}
