package app

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPostPullSourceResynchronizationAdoptsAndReentersReview(t *testing.T) {
	f := awaitingAdmissionBehindPull(t)
	exit, output, diagnostics := runParentActionCLIInProcess(t, f.cfg, "resync-source")
	if exit != 0 {
		t.Fatalf("source resynchronization failed: %s %s", output, diagnostics)
	}
	if !strings.Contains(output, "\"source-resynchronized\"") || !strings.Contains(output, "\"adopt-mutable\"") || !strings.Contains(output, "\"processing_duration_ms\"") {
		t.Fatalf("source resynchronization missed typed adoption: %s", output)
	}
	head, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.IntegrationTip != f.tip || head.LiveLeaseID == "" || head.PendingTransitionID != "" {
		t.Fatalf("source resynchronization did not reenter a live reviewed execution: %#v", head)
	}
	if f.runtime.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("resynchronization must require a fresh Review: %s", f.runtime.TaskStatus())
	}
	if f.scripted.workerRounds != 1 {
		t.Fatalf("resynchronization unexpectedly invoked a model: %d worker rounds", f.scripted.workerRounds)
	}
	remote := strings.Fields(string(appTestGitOutput(t, f.store.Identity().PrimaryRoot, "ls-remote", f.policy.Remote, f.policy.RemoteRef)))
	if len(remote) != 2 || remote[0] != f.tip {
		t.Fatalf("source resynchronization modified the remote: %v", remote)
	}
	exit, output, diagnostics = runParentActionCLIInProcess(t, f.cfg, "resync-source")
	if exit != 0 || !strings.Contains(output, "\"no-mechanical-step\"") {
		t.Fatalf("repeat resynchronization must be a no-op: exit=%d %s %s", exit, output, diagnostics)
	}
}

func TestPostPullSourceResynchronizationRebindsCandidateAndInvalidatesEvidence(t *testing.T) {
	f := candidateBehindMetadataPull(t)
	exit, output, diagnostics := runParentActionCLIInProcess(t, f.cfg, "resync-source")
	if exit != 0 {
		t.Fatalf("source resynchronization failed: %s %s", output, diagnostics)
	}
	if !strings.Contains(output, "\"source-resynchronized\"") || !strings.Contains(output, "\"rebind\"") {
		t.Fatalf("source resynchronization missed typed rebind: %s", output)
	}
	head, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.IntegrationTip != f.tip || head.PendingTransitionID != "" {
		t.Fatalf("candidate source did not converge: %#v", head)
	}
	if f.runtime.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("rebound candidate must wait for fresh Review: %s", f.runtime.TaskStatus())
	}
	if head.AcceptedCandidateRef != nil {
		candidate, err := f.store.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
		if err != nil {
			t.Fatal(err)
		}
		if candidate.EvidenceValid {
			t.Fatalf("post-pull source resynchronization inherited old Review: %#v", candidate)
		}
	}
}

func TestPostPullSourceResynchronizationRefusesRemoteRace(t *testing.T) {
	f := awaitingAdmissionBehindPull(t)
	before, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	remote := advanceRemoteBeyondPulledPrimary(t, f.store.Identity().PrimaryRoot, f.policy, "further-remote.txt", "remote changed after pull\n")
	exit, output, diagnostics := runParentActionCLIInProcess(t, f.cfg, "resync-source")
	if exit == 0 || !strings.Contains(diagnostics, "does not match the configured remote tip") {
		t.Fatalf("remote race must be rejected: exit=%d %s %s", exit, output, diagnostics)
	}
	after, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if after.IntegrationTip != before.IntegrationTip || after.LiveLeaseID != before.LiveLeaseID || after.PendingTransitionID != "" {
		t.Fatalf("remote race changed Controller authority: %#v", after)
	}
	fields := strings.Fields(string(appTestGitOutput(t, f.store.Identity().PrimaryRoot, "ls-remote", f.policy.Remote, f.policy.RemoteRef)))
	if len(fields) != 2 || fields[0] != remote {
		t.Fatalf("refused resynchronization modified the remote: %v", fields)
	}
}

func TestPostPullRetiredMetadataUsesNonPushingSourceOwner(t *testing.T) {
	cfg, _, store, policy, _ := completedRetiredWorkflowFixture(t)
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	tip := pullExternalSourceChange(t, cfg, policy, map[string]string{
		"IMPLEMENTATION_HISTORY.md": "pulled parent metadata only\n",
	}, nil)
	exit, output, diagnostics := runParentActionCLI(t, cfg, "resync-source")
	if exit != 0 || !strings.Contains(output, "\"source-resynchronized\"") || !strings.Contains(output, "\"adopt\"") {
		t.Fatalf("retired metadata pull not synchronized: exit=%d %s %s", exit, output, diagnostics)
	}
	after, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if after.IntegrationTip != tip || after.RootTaskRef == nil || !after.RootTaskRef.Equal(*before.RootTaskRef) {
		t.Fatalf("retired metadata pull changed the active Task: %#v", after)
	}
	assertPostPullRemoteOID(t, store.Identity().PrimaryRoot, policy, tip)
	exit, output, diagnostics = runParentActionCLI(t, cfg, "resync-source")
	if exit != 0 || !strings.Contains(output, "\"no-mechanical-step\"") {
		t.Fatalf("repeated retired sync must be a no-op: exit=%d %s %s", exit, output, diagnostics)
	}
	assertPostPullRemoteOID(t, store.Identity().PrimaryRoot, policy, tip)
}

func TestPostPullRefusesUnpublishedLocalMetadataWithoutPush(t *testing.T) {
	cfg, _, store, policy, _ := completedRetiredWorkflowFixture(t)
	primary := store.Identity().PrimaryRoot
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	remote := strings.Fields(string(appTestGitOutput(t, primary, "ls-remote", policy.Remote, policy.RemoteRef)))[0]
	writeAppTestFile(t, primary, "IMPLEMENTATION_HISTORY.md", "local metadata not yet published\n")
	runControllerActivationGit(t, primary, "add", "IMPLEMENTATION_HISTORY.md")
	runControllerActivationGit(t, primary, "commit", "-qm", "unpushed parent metadata")
	exit, output, diagnostics := runParentActionCLI(t, cfg, "resync-source")
	if exit == 0 || !strings.Contains(diagnostics, "remote commit differs") {
		t.Fatalf("unpushed local metadata must not be adopted or published: exit=%d %s %s", exit, output, diagnostics)
	}
	assertPostPullRemoteOID(t, primary, policy, remote)
	after, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if after.ControllerGeneration != before.ControllerGeneration || after.IntegrationTip != before.IntegrationTip {
		t.Fatalf("refused unpushed metadata changed Controller authority: %#v", after)
	}
}

func TestPostPullRetiredMetadataRejectsConcurrentRemoteChange(t *testing.T) {
	cfg, _, store, policy, _ := completedRetiredWorkflowFixture(t)
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	pullExternalSourceChange(t, cfg, policy, map[string]string{"IMPLEMENTATION_HISTORY.md": "pulled metadata\n"}, nil)
	remote := advanceRemoteBeyondPulledPrimary(t, store.Identity().PrimaryRoot, policy, "new-remote-only.txt", "competing remote\n")
	exit, output, diagnostics := runParentActionCLI(t, cfg, "resync-source")
	if exit == 0 || !strings.Contains(diagnostics, "remote commit differs") {
		t.Fatalf("retired remote race must be refused: exit=%d %s %s", exit, output, diagnostics)
	}
	assertPostPullRemoteOID(t, store.Identity().PrimaryRoot, policy, remote)
	after, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if after.ControllerGeneration != before.ControllerGeneration || after.IntegrationTip != before.IntegrationTip {
		t.Fatalf("retired remote race changed Controller authority: %#v", after)
	}
}

func TestPostPullRefusesAnotherSelectedTaskAndMaterialization(t *testing.T) {
	cfg, _, store, policy, _ := completedRetiredWorkflowFixture(t)
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	successor := "IMPLEMENTATION_TASKS/alternate-successor.md"
	remote := pullExternalSourceChange(t, cfg, policy, nil, &[2]string{before.RootTaskRef.TaskPath, successor})
	exit, output, diagnostics := runParentActionCLI(t, cfg, "resync-source")
	if exit == 0 || !strings.Contains(diagnostics, "selected Task") {
		t.Fatalf("different Task must not be synchronized: exit=%d %s %s", exit, output, diagnostics)
	}
	assertPostPullRemoteOID(t, store.Identity().PrimaryRoot, policy, remote)
	after, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if after.ControllerGeneration != before.ControllerGeneration || after.IntegrationTip != before.IntegrationTip || after.ExecutionTaskRef != nil {
		t.Fatalf("cross-task resync changed state or materialized another task: %#v", after)
	}
}

func assertPostPullRemoteOID(t *testing.T, primary string, policy controller.PublicationPolicy, want string) {
	t.Helper()
	fields := strings.Fields(string(appTestGitOutput(t, primary, "ls-remote", policy.Remote, policy.RemoteRef)))
	if len(fields) != 2 || fields[0] != want {
		t.Fatalf("post-pull sync modified remote OID: got=%v want=%s", fields, want)
	}
}
