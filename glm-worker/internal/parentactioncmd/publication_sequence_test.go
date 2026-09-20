package parentactioncmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/app"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskdiff"
)

type publicationPushGuardFixture struct {
	cfg       config.AppConfig
	st        *state.StateStore
	repo      string
	remote    string
	candidate state.PublicationCandidate
}

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
	pushCommand := sequence.NextAction.Commands[0]
	if len(pushCommand) < 4 || pushCommand[0] != "git" || pushCommand[3] != "push" {
		t.Fatalf("push command = %#v", pushCommand)
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

	changed, available, err := taskdiff.ChangedPaths(fixture.repo, fixture.st)
	if err != nil || !available {
		t.Fatalf("changed paths available=%v err=%v", available, err)
	}
	var rawRuntimePaths []string
	for _, path := range changed {
		if taskdiff.RuntimeInstallPath(path) {
			rawRuntimePaths = append(rawRuntimePaths, path)
		}
	}
	if len(rawRuntimePaths) != 2 || reflect.DeepEqual(rawRuntimePaths, taskdiff.RuntimeChangedPaths(changed)) {
		t.Fatalf("fixture runtime paths do not exercise append-order divergence: %#v", rawRuntimePaths)
	}

	candidate, failure := preparePublicationCandidate(fixture.cfg, fixture.st, "mixed runtime paths publication")
	if failure != nil {
		t.Fatalf("prepare failed: %#v", failure)
	}

	sequence := app.ProjectPublicationSequence(fixture.repo, fixture.st)
	if sequence.Stage != "install-candidate" || sequence.NextAction == nil || sequence.NextAction.Stage != "install-candidate" {
		t.Fatalf("pre-evidence sequence = %#v", sequence)
	}

	requirement, err := runtimeInstallRequirementForTask(fixture.repo, fixture.st)
	if err != nil || !requirement.Required {
		t.Fatalf("runtime requirement = %#v err=%v", requirement, err)
	}
	taskID, err := fixture.st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.st.SaveRuntimeInstallEvidence(state.RuntimeInstallEvidence{
		Version:           1,
		TaskID:            taskID,
		Head:              candidate.CommitOID,
		SourceDigest:      requirement.SourceDigest,
		InstalledRevision: candidate.CommitOID,
		SmokeResult:       state.ValidationResultPass,
	}); err != nil {
		t.Fatal(err)
	}

	sequence = app.ProjectPublicationSequence(fixture.repo, fixture.st)
	if sequence.Stage != "promote" || sequence.NextAction == nil || sequence.NextAction.Stage != "promote" {
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

	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
	completion := runCompleteCommand(t, fixture)
	if completion.Status != completeStatusComplete || !completion.Completed {
		t.Fatalf("post-recovery completion = %#v", completion)
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

func TestPublicationSequenceBlocksUnclassifiedCommittedState(t *testing.T) {
	fixture := newCompleteFixture(t)
	if err := state.CaptureGitBaseline(fixture.cfg, fixture.st); err != nil {
		t.Fatal(err)
	}

	sequence := app.ProjectPublicationSequence(fixture.repo, fixture.st)
	if sequence.Stage != "blocked" || sequence.Failure == nil || sequence.Failure.Reason != "publication_source_empty" {
		t.Fatalf("clean baseline sequence = %#v", sequence)
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

func TestPublicationPromotionBlockedBySourceMutationLeavesRefAtBase(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, "README.md"), []byte("ready candidate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	publicationGit(t, cfg.RepoRoot, "add", "README.md")
	if _, failure := preparePublicationCandidate(cfg, st, "mutation publication"); failure != nil {
		t.Fatalf("prepare failed: %#v", failure)
	}
	baseHead := publicationGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD")

	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, "README.md"), []byte("mutated after prepare\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := promotePublicationCandidate(cfg, st)
	if output.Status != publicationPromotionStatusBlocked || output.Failure == nil || output.Failure.Reason != publicationFailureCandidateStale {
		t.Fatalf("mutated promotion = %#v", output)
	}
	if got := publicationGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD"); got != baseHead {
		t.Fatalf("blocked promotion advanced branch HEAD: %s != %s", got, baseHead)
	}
}

func TestPublicationGuardSetupPreflightFailsClosed(t *testing.T) {
	cfg, st, _ := preparePublicationRuntimeCandidate(t)
	hooksPath := publicationGitOutput(t, cfg.RepoRoot, "config", "--get", "core.hooksPath")
	referenceTransaction := filepath.Join(hooksPath, "reference-transaction")

	if err := os.Chmod(referenceTransaction, 0o644); err != nil {
		t.Fatal(err)
	}
	output := projectPublicationReadiness(cfg, st)
	if output.Status != publicationReadinessBlocked || output.Failure == nil || output.Failure.Reason != publicationFailureGuardSetupInvalid {
		t.Fatalf("non-executable hook readiness = %#v", output)
	}
	gate := publicationGateNamed(t, output.Gates, publicationGuardSetupGateName)
	if gate.Status != publicationGateMissing {
		t.Fatalf("guard gate = %#v", gate)
	}
	if failure := verifyPublicationCompletionGate(cfg, st); failure == nil || failure.Reason != publicationFailureGuardSetupInvalid {
		t.Fatalf("non-executable hook completion gate = %#v", failure)
	}

	if err := os.Chmod(referenceTransaction, 0o755); err != nil {
		t.Fatal(err)
	}
	publicationGit(t, cfg.RepoRoot, "config", "--unset", "core.hooksPath")
	output = projectPublicationReadiness(cfg, st)
	if output.Status != publicationReadinessBlocked || output.Failure == nil || output.Failure.Reason != publicationFailureGuardSetupInvalid {
		t.Fatalf("unset hooksPath readiness = %#v", output)
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
	var flattened []string
	for _, command := range sequence.NextAction.Commands {
		flattened = append(flattened, command...)
	}
	joined := strings.Join(flattened, " ")
	if !strings.Contains(joined, "chmod +x") || !strings.Contains(joined, ".githooks/reference-transaction") ||
		!strings.Contains(joined, ".githooks/pre-push") || !strings.Contains(joined, "git -C") {
		t.Fatalf("repair commands = %#v", sequence.NextAction.Commands)
	}
}

func TestCompleteHandoverOwnerVerificationFailsClosedWithoutAuthority(t *testing.T) {
	fixture := newCompleteFixture(t)
	fixture.commitParentMetadataSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
	taskID, err := fixture.st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fixture.st.TaskAuthorityPathPath(taskID)); err != nil {
		t.Fatal(err)
	}

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusAwaiting || output.Completed {
		t.Fatalf("owner-less completion = %#v", output)
	}
	if output.Failure == nil || output.Failure.Reason != "completion_transition_invalid" || !strings.Contains(output.Failure.Detail, "task authority") {
		t.Fatalf("owner-less failure = %#v", output.Failure)
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
	remoteMainBefore := publicationGitOutput(t, fixture.remote, "rev-parse", "refs/heads/main")
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
	if got := publicationGitOutput(t, fixture.remote, "rev-parse", "refs/heads/main"); got != remoteMainBefore {
		t.Fatalf("local branch name reached the remote: main %s != %s", got, remoteMainBefore)
	}

	sequence = app.ProjectPublicationSequence(fixture.repo, fixture.st)
	if sequence.Stage != "complete" || sequence.NextAction == nil || sequence.NextAction.Stage != "complete" {
		t.Fatalf("post-push sequence = %#v", sequence)
	}
}

func newPublicationPushGuardFixture(t *testing.T) publicationPushGuardFixture {
	t.Helper()
	sourceRoot := publicationWorktreeSourceRoot(t)
	pushGuardBin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(pushGuardBin, 0o700); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("GOCACHE") == "" {
		t.Setenv("GOCACHE", filepath.Join(t.TempDir(), "gocache"))
	}
	build := exec.Command("go", "build", "-o", filepath.Join(pushGuardBin, "glm-parent-action"), "./cmd/glm-parent-action")
	build.Dir = filepath.Join(sourceRoot, "glm-worker")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build glm-parent-action: %v: %s", err, output)
	}

	t.Setenv("CODEX_THREAD_ID", codexIdentityTestThreadID)
	t.Setenv("CODEX_SESSION_ID", codexIdentityTestThreadID)
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	if err := os.MkdirAll(remote, 0o700); err != nil {
		t.Fatal(err)
	}
	runFinalizationGit(t, repo, "init", "-q", "-b", "main")
	runFinalizationGit(t, repo, "config", "user.name", "push guard test")
	runFinalizationGit(t, repo, "config", "user.email", "push-guard@example.invalid")
	runFinalizationGit(t, remote, "init", "-q", "--bare", "-b", "main")
	if err := os.MkdirAll(filepath.Join(repo, "IMPLEMENTATION_TASKS"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePushBindingFile(t, repo, "IMPLEMENTATION_PLAN.local.md", completeInitialPlan())
	writePushBindingFile(t, repo, "IMPLEMENTATION_TASKS/active.md", "# active\n\n## Dependencies\n\n## External feasibility\n\nstatus: not-applicable\n")
	writeRepositoryHarnessMarker(t, repo)
	runFinalizationGit(t, repo, "add", "-A")
	runFinalizationGit(t, repo, "commit", "-q", "-m", "initial")
	runFinalizationGit(t, repo, "remote", "add", "origin", remote)
	runFinalizationGit(t, repo, "push", "-q", "-u", "origin", "main")

	hooks := t.TempDir()
	for _, name := range []string{"reference-transaction", "pre-push"} {
		data, err := os.ReadFile(filepath.Join(sourceRoot, ".githooks", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(hooks, name), data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runFinalizationGit(t, repo, "config", "core.hooksPath", hooks)

	stateHome := t.TempDir()
	cfg := config.AppConfig{
		RepoRoot:  repo,
		RepoHash:  config.RepoHashFor(repo),
		StateBase: filepath.Join(stateHome, "sessions"),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	pinCompleteRepositoryHarnessActive(t, st)
	if err := st.SetParentCodexIdentity(codexIdentityTestThreadID, codexIdentityTestThreadID, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("active-task", "IMPLEMENTATION_TASKS/active.md"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/active.md", []byte("# active\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AcceptParentReview(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GLM_WORKER_HOME", stateHome)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("PATH", pushGuardBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "IMPLEMENTATION_TASKS", "active.md")); err != nil {
		t.Fatal(err)
	}
	writePushBindingFile(t, repo, "IMPLEMENTATION_PLAN.local.md", completePromotedPlan())
	writePushBindingFile(t, repo, "impl.txt", "managed pre-push guard\n")
	runFinalizationGit(t, repo, "add", "-A")
	candidate, failure := preparePublicationCandidate(cfg, st, "managed pre-push guard publication")
	if failure != nil {
		t.Fatalf("prepare failed: %#v", failure)
	}
	promotion := promotePublicationCandidate(cfg, st)
	if promotion.Status != publicationPromotionStatusPromoted || promotion.Failure != nil {
		t.Fatalf("promotion through managed reference-transaction guard = %#v", promotion)
	}
	return publicationPushGuardFixture{cfg: cfg, st: st, repo: repo, remote: remote, candidate: candidate}
}

func TestObjectSourcedCandidatePushRejectedByManagedPrePushGuard(t *testing.T) {
	fixture := newPublicationPushGuardFixture(t)

	oidPush := exec.Command("git", "-C", fixture.repo, "push", "origin", fixture.candidate.CommitOID+":refs/heads/main")
	output, err := oidPush.CombinedOutput()
	if err == nil {
		t.Fatalf("object-sourced push passed the managed pre-push guard: %s", output)
	}
	if !strings.Contains(string(output), "publication_push_local_object_not_candidate") {
		t.Fatalf("object-sourced push rejection = %s", output)
	}
	if got := publicationGitOutput(t, fixture.remote, "rev-parse", "refs/heads/main"); got == fixture.candidate.CommitOID {
		t.Fatalf("rejected object-sourced push updated the remote ref to %s", got)
	}
}

func TestPublicationPushActionPassesManagedPrePushGuard(t *testing.T) {
	fixture := newPublicationPushGuardFixture(t)

	sequence := app.ProjectPublicationSequence(fixture.repo, fixture.st)
	if sequence.Stage != "push" || sequence.NextAction == nil || sequence.NextAction.Stage != "push" {
		t.Fatalf("push sequence = %#v", sequence)
	}
	pushArgv := sequence.NextAction.Commands[0]
	if len(pushArgv) != 6 || pushArgv[5] != "refs/heads/main:refs/heads/main" {
		t.Fatalf("canonical push argv = %#v", pushArgv)
	}
	push := exec.Command(pushArgv[0], pushArgv[1:]...)
	if output, err := push.CombinedOutput(); err != nil {
		t.Fatalf("canonical push failed the managed pre-push guard: %v: %s", err, output)
	}
	if got := publicationGitOutput(t, fixture.remote, "rev-parse", "refs/heads/main"); got != fixture.candidate.CommitOID {
		t.Fatalf("remote main = %s want candidate %s", got, fixture.candidate.CommitOID)
	}

	sequence = app.ProjectPublicationSequence(fixture.repo, fixture.st)
	if sequence.Stage != "complete" || sequence.NextAction == nil || sequence.NextAction.Stage != "complete" {
		t.Fatalf("post-push sequence = %#v", sequence)
	}
}

func commitCompleteFixtureHarnessMarker(t *testing.T, fixture *completeFixture) {
	t.Helper()
	writeRepositoryHarnessMarker(t, fixture.repo)
	runFinalizationGit(t, fixture.repo, "add", "-A")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-m", "harness marker")
}
