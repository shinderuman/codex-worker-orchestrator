package app

import (
	"strings"
	"testing"

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

