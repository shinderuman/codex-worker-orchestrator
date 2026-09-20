package parentactioncmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/app"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskdiff"
)

func TestPublicationSequenceDrivesCanonicalHandoffToCompletion(t *testing.T) {
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

	sequence := app.ProjectPublicationSequence(fixture.repo, fixture.st)
	if sequence.Stage != "prepare" || sequence.NextAction == nil || sequence.NextAction.Stage != "prepare" {
		t.Fatalf("dirty handoff sequence = %#v", sequence)
	}
	wantPrepare := [][]string{
		{"git", "-C", fixture.repo, "add", "-A"},
		{"glm-parent-action", "push-binding", "prepare", "--message", "<commit-message>"},
	}
	if !reflect.DeepEqual(sequence.NextAction.Commands, wantPrepare) || sequence.NextAction.Parameter != "commit-message" {
		t.Fatalf("prepare commands = %#v", sequence.NextAction.Commands)
	}

	runFinalizationGit(t, fixture.repo, "add", "-A")
	candidate, failure := preparePublicationCandidate(fixture.cfg, fixture.st, "integration publication")
	if failure != nil {
		t.Fatalf("prepare failed: %#v", failure)
	}

	sequence = app.ProjectPublicationSequence(fixture.repo, fixture.st)
	if sequence.Stage != "promote" || sequence.NextAction == nil || sequence.NextAction.Stage != "promote" {
		t.Fatalf("prepared sequence = %#v", sequence)
	}

	promotion := promotePublicationCandidate(fixture.cfg, fixture.st)
	if promotion.Status != publicationPromotionStatusPromoted || promotion.CandidateOID != candidate.CommitOID {
		t.Fatalf("promotion = %#v", promotion)
	}

	sequence = app.ProjectPublicationSequence(fixture.repo, fixture.st)
	if sequence.Stage != "push" || sequence.NextAction == nil || sequence.NextAction.Stage != "push" {
		t.Fatalf("promoted sequence = %#v", sequence)
	}
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")

	sequence = app.ProjectPublicationSequence(fixture.repo, fixture.st)
	if sequence.Stage != "complete" || sequence.NextAction == nil || sequence.NextAction.Stage != "complete" {
		t.Fatalf("pushed sequence = %#v", sequence)
	}

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusComplete || !output.Completed {
		t.Fatalf("completion = %#v", output)
	}
	sequence = app.ProjectPublicationSequence(fixture.repo, fixture.st)
	if sequence.Stage != "complete" || sequence.NextAction != nil {
		t.Fatalf("completed sequence = %#v", sequence)
	}
}

func TestPublicationSequencePromotesAfterInstallEvidenceForMixedRuntimePaths(t *testing.T) {
	fixture := newCompleteFixture(t)
	commitCompleteFixtureHarnessMarker(t, fixture)
	writePushBindingFile(t, fixture.repo, "install.sh", "#!/bin/sh\nexit 0\n")
	runFinalizationGit(t, fixture.repo, "add", "-A")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-m", "runtime base")
	if err := state.CaptureGitBaseline(fixture.cfg, fixture.st); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(fixture.repo, "IMPLEMENTATION_TASKS", "active.md")); err != nil {
		t.Fatal(err)
	}
	writePushBindingFile(t, fixture.repo, "IMPLEMENTATION_PLAN.local.md", completePromotedPlan())
	writePushBindingFile(t, fixture.repo, "install.sh", "#!/bin/sh\nexit 0\n# task runtime edit\n")
	if err := os.MkdirAll(filepath.Join(fixture.repo, "glm-worker", "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePushBindingFile(t, fixture.repo, "glm-worker/internal/task_created_runtime_fixture.go", "package internal\n\nvar taskCreatedRuntime = true\n")
	runFinalizationGit(t, fixture.repo, "add", "-A")

	candidate, failure := preparePublicationCandidate(fixture.cfg, fixture.st, "mixed runtime paths publication")
	if failure != nil {
		t.Fatalf("prepare failed: %#v", failure)
	}
	sequence := app.ProjectPublicationSequence(fixture.repo, fixture.st)
	if sequence.Stage != "install-candidate" || sequence.NextAction == nil {
		t.Fatalf("pre-evidence sequence = %#v", sequence)
	}

	changed, available, err := taskdiff.ChangedPaths(fixture.repo, fixture.st)
	if err != nil || !available {
		t.Fatalf("changed paths available=%v err=%v", available, err)
	}
	runtimePaths := taskdiff.RuntimeChangedPaths(changed)
	digest, err := taskdiff.RuntimeSourceDigest(fixture.repo, runtimePaths)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := fixture.st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.st.SaveRuntimeInstallEvidence(state.RuntimeInstallEvidence{
		Version:           1,
		TaskID:            taskID,
		Head:              candidate.CommitOID,
		SourceDigest:      digest,
		InstalledRevision: candidate.CommitOID,
		SmokeResult:       state.ValidationResultPass,
	}); err != nil {
		t.Fatal(err)
	}
	sequence = app.ProjectPublicationSequence(fixture.repo, fixture.st)
	if sequence.Stage != "promote" || sequence.NextAction == nil {
		t.Fatalf("post-evidence sequence = %#v", sequence)
	}
}

func TestCompleteWithDirtyTreeReturnsExactPrepareAction(t *testing.T) {
	fixture := newCompleteFixture(t)
	writePushBindingFile(t, fixture.repo, "impl.txt", "impl\n")

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusAwaiting || output.Failure == nil || output.Failure.Reason != "tree_not_clean" {
		t.Fatalf("dirty complete = %#v", output)
	}
	if output.NextAction == nil || output.NextAction.Stage != "prepare" {
		t.Fatalf("dirty complete next action = %#v", output.NextAction)
	}
	var flattened []string
	for _, command := range output.NextAction.Commands {
		flattened = append(flattened, command...)
		if command[0] == "git" && len(command) > 3 && command[3] == "commit" {
			t.Fatalf("next action directed a plain git commit: %#v", command)
		}
	}
	if !strings.Contains(strings.Join(flattened, " "), "push-binding prepare --message") {
		t.Fatalf("next action commands = %#v", output.NextAction.Commands)
	}
}

func TestPublicationRecoverAdoptsSingleCommittedStateWithoutCandidate(t *testing.T) {
	fixture := newCompleteFixture(t)
	commitCompleteFixtureHarnessMarker(t, fixture)
	if err := state.CaptureGitBaseline(fixture.cfg, fixture.st); err != nil {
		t.Fatal(err)
	}
	baseline := completeFixtureHead(t, fixture.repo)
	fixture.commitParentMetadataSync(t)
	head := completeFixtureHead(t, fixture.repo)

	output := recoverPublicationCandidate(fixture.cfg, fixture.st)
	if output.Status != publicationRecoverStatusRecovered || output.Candidate == nil || output.Failure != nil {
		t.Fatalf("recovery = %#v", output)
	}
	if output.Candidate.CommitOID != head || output.Candidate.BaseHead != baseline {
		t.Fatalf("adopted candidate = %#v want head %s base %s", output.Candidate, head, baseline)
	}
	if output.Safety == "" || output.HistoryImpact == "" || !strings.Contains(output.HistoryImpact, head) {
		t.Fatalf("recovery notes = %#v", output)
	}
	if output.NextAction == nil || output.NextAction.Stage != "push" {
		t.Fatalf("recovery next action = %#v", output.NextAction)
	}
	if got := fixture.st.TaskStatus(); got != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("recovery changed task status: %s", got)
	}
}

func TestPublicationRecoverFailsClosedOutsideBoundedState(t *testing.T) {
	t.Run("candidate already present", func(t *testing.T) {
		fixture := newCompleteFixture(t)
		commitCompleteFixtureHarnessMarker(t, fixture)
		if err := state.CaptureGitBaseline(fixture.cfg, fixture.st); err != nil {
			t.Fatal(err)
		}
		fixture.commitParentMetadataSync(t)
		ensureCompleteFixturePublicationAuthority(t, fixture)
		output := recoverPublicationCandidate(fixture.cfg, fixture.st)
		if output.Status != publicationRecoverStatusBlocked || output.Failure == nil || output.Failure.Reason != publicationFailureRecoverCandidate {
			t.Fatalf("recovery = %#v", output)
		}
	})

	t.Run("dirty tree", func(t *testing.T) {
		fixture := newCompleteFixture(t)
		commitCompleteFixtureHarnessMarker(t, fixture)
		if err := state.CaptureGitBaseline(fixture.cfg, fixture.st); err != nil {
			t.Fatal(err)
		}
		fixture.commitParentMetadataSync(t)
		writePushBindingFile(t, fixture.repo, "late.txt", "late\n")
		output := recoverPublicationCandidate(fixture.cfg, fixture.st)
		if output.Status != publicationRecoverStatusBlocked || output.Failure == nil || output.Failure.Reason != publicationFailureRecoverTree {
			t.Fatalf("recovery = %#v", output)
		}
	})

	t.Run("multiple commits ahead", func(t *testing.T) {
		fixture := newCompleteFixture(t)
		commitCompleteFixtureHarnessMarker(t, fixture)
		if err := state.CaptureGitBaseline(fixture.cfg, fixture.st); err != nil {
			t.Fatal(err)
		}
		fixture.commitParentMetadataSync(t)
		writePushBindingFile(t, fixture.repo, "second.txt", "second\n")
		runFinalizationGit(t, fixture.repo, "add", "second.txt")
		runFinalizationGit(t, fixture.repo, "commit", "-q", "-m", "second rogue commit")
		output := recoverPublicationCandidate(fixture.cfg, fixture.st)
		if output.Status != publicationRecoverStatusBlocked || output.Failure == nil || output.Failure.Reason != publicationFailureRecoverHead {
			t.Fatalf("recovery = %#v", output)
		}
	})

	t.Run("already pushed", func(t *testing.T) {
		fixture := newCompleteFixture(t)
		commitCompleteFixtureHarnessMarker(t, fixture)
		if err := state.CaptureGitBaseline(fixture.cfg, fixture.st); err != nil {
			t.Fatal(err)
		}
		fixture.commitParentMetadataSync(t)
		runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
		output := recoverPublicationCandidate(fixture.cfg, fixture.st)
		if output.Status != publicationRecoverStatusBlocked || output.Failure == nil || output.Failure.Reason != publicationFailureRecoverRemote {
			t.Fatalf("recovery = %#v", output)
		}
	})
}

func TestPublicationSequenceDirectsRecoveryForCommittedStateWithoutCandidate(t *testing.T) {
	fixture := newCompleteFixture(t)
	if err := state.CaptureGitBaseline(fixture.cfg, fixture.st); err != nil {
		t.Fatal(err)
	}
	fixture.commitParentMetadataSync(t)

	sequence := app.ProjectPublicationSequence(fixture.repo, fixture.st)
	if sequence.Stage != "recover" || sequence.NextAction == nil || sequence.NextAction.Stage != "recover" {
		t.Fatalf("committed sequence = %#v", sequence)
	}
	if !reflect.DeepEqual(sequence.NextAction.Commands, [][]string{{"glm-parent-action", "push-binding", "recover"}}) {
		t.Fatalf("recover commands = %#v", sequence.NextAction.Commands)
	}
}

func TestPublicationSequenceFailsClosedWhenUpstreamIsUnconfigured(t *testing.T) {
	fixture := newCompleteFixture(t)
	fixture.commitParentMetadataSync(t)
	runFinalizationGit(t, fixture.repo, "config", "--unset", "branch.main.remote")
	ensureCompleteFixturePublicationAuthority(t, fixture)

	sequence := app.ProjectPublicationSequence(fixture.repo, fixture.st)
	if sequence.Stage != "blocked" || sequence.Failure == nil || sequence.Failure.Reason != "publication_upstream_unconfigured" {
		t.Fatalf("unconfigured upstream sequence = %#v", sequence)
	}
	if sequence.NextAction != nil {
		t.Fatalf("unconfigured upstream returned a next action: %#v", sequence.NextAction)
	}
}

func TestPublicationSequenceOffersTrackedHookModeRepair(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.RepoRoot, ".githooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"reference-transaction", "pre-push"} {
		if err := os.WriteFile(filepath.Join(cfg.RepoRoot, ".githooks", name), []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	publicationGit(t, cfg.RepoRoot, "config", "core.hooksPath", ".githooks")

	sequence := app.ProjectPublicationSequence(cfg.RepoRoot, st)
	if sequence.Stage != "blocked" || sequence.Failure == nil || sequence.Failure.Reason != "publication_guard_setup_invalid" {
		t.Fatalf("tracked-mode sequence = %#v", sequence)
	}
	if sequence.NextAction == nil || sequence.NextAction.Stage != "repair-guard-setup" {
		t.Fatalf("tracked-mode repair = %#v", sequence.NextAction)
	}
}

func TestPublicationPushCommandTargetsConfiguredUpstreamRef(t *testing.T) {
	fixture := newCompleteFixture(t)
	commitCompleteFixtureHarnessMarker(t, fixture)
	if err := state.CaptureGitBaseline(fixture.cfg, fixture.st); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(fixture.repo, "IMPLEMENTATION_TASKS", "active.md")); err != nil {
		t.Fatal(err)
	}
	writePushBindingFile(t, fixture.repo, "IMPLEMENTATION_PLAN.local.md", completePromotedPlan())
	writePushBindingFile(t, fixture.repo, "impl.txt", "published upstream\n")
	runFinalizationGit(t, fixture.repo, "add", "-A")
	candidate, failure := preparePublicationCandidate(fixture.cfg, fixture.st, "upstream ref publication")
	if failure != nil {
		t.Fatalf("prepare failed: %#v", failure)
	}
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", candidate.BaseHead+":refs/heads/trunk")
	runFinalizationGit(t, fixture.repo, "config", "branch.main.merge", "refs/heads/trunk")
	promotion := promotePublicationCandidate(fixture.cfg, fixture.st)
	if promotion.Status != publicationPromotionStatusPromoted || promotion.Failure != nil {
		t.Fatalf("promotion = %#v", promotion)
	}

	sequence := app.ProjectPublicationSequence(fixture.repo, fixture.st)
	if sequence.Stage != "push" || sequence.NextAction == nil || sequence.NextAction.Stage != "push" {
		t.Fatalf("upstream push sequence = %#v", sequence)
	}
	want := []string{"git", "-C", fixture.repo, "push", "origin", "refs/heads/main:refs/heads/trunk"}
	if !reflect.DeepEqual(sequence.NextAction.Commands[0], want) {
		t.Fatalf("push command = %#v want %#v", sequence.NextAction.Commands[0], want)
	}
	output, err := exec.Command(want[0], want[1:]...).CombinedOutput()
	if err != nil {
		t.Fatalf("projected push failed: %v: %s", err, output)
	}
	if got := publicationGitOutput(t, fixture.remote, "rev-parse", "refs/heads/trunk"); got != candidate.CommitOID {
		t.Fatalf("upstream trunk ref = %s want candidate %s", got, candidate.CommitOID)
	}
}

func commitCompleteFixtureHarnessMarker(t *testing.T, fixture *completeFixture) {
	t.Helper()
	writeRepositoryHarnessMarker(t, fixture.repo)
	runFinalizationGit(t, fixture.repo, "add", "-A")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-m", "harness marker")
}
