package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIntegratedLifecycleDuplicateFindingConverges(t *testing.T) {
	t.Parallel()
	harness := newLifecycleHarness(t)
	head, err := harness.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	first, err := harness.store.ObserveFinding(harness.source, FindingObservationInput{
		Producer: "reviewer-a", ProofClass: FindingProofUnverified, ProblemKey: "duplicate-defect",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := harness.store.ObserveFinding(harness.source, FindingObservationInput{
		Producer: "reviewer-b", ProofClass: FindingProofUnverified, ProblemKey: "duplicate-defect",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.FindingID == second.FindingID || second.CanonicalFindingID != first.FindingID {
		t.Fatalf("duplicate observations did not converge canonically: first=%#v second=%#v", first, second)
	}
	if second.Producer == first.Producer {
		t.Fatal("duplicate finding provenance was collapsed")
	}
	result, err := harness.store.ResolveFindingWithProjectAuthority(second.FindingID, FindingDecision{Kind: FindingDecisionDuplicate})
	if err != nil {
		t.Fatal(err)
	}
	if result.Intent != FindingIntentDuplicate || result.Disposition == nil || result.Disposition.CanonicalFindingID != first.FindingID {
		t.Fatalf("duplicate disposition = %#v", result)
	}
	if result.Episode != nil {
		t.Fatalf("duplicate disposition minted episode authority: %#v", result.Episode)
	}
	after, err := harness.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if after.ControllerGeneration != head.ControllerGeneration || after.ActiveEpisodeID != "" || after.LiveAttemptID != head.LiveAttemptID {
		t.Fatalf("duplicate disposition changed execution authority: before=%#v after=%#v", head, after)
	}
	harness.assertInvariant(t, "duplicate disposition")
}

func TestIntegratedLifecycleCycleFailsClosed(t *testing.T) {
	t.Parallel()
	harness := newLifecycleHarness(t)
	head, err := harness.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	finding, err := harness.store.ObserveFinding(harness.source, FindingObservationInput{
		Producer: "reviewer-a", ProofClass: FindingProofUnverified, ProblemKey: "cycle-defect",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.store.ResolveFindingWithProjectAuthority(finding.FindingID, FindingDecision{
		Kind:             FindingDecisionIndependentBlocking,
		TargetTaskRef:    &harness.root,
		BlockingBoundary: "cycle attempt",
	}); err == nil {
		t.Fatal("cyclic blocking finding was admitted")
	}
	outside := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/unknown.md", ContractDigest: "unknown"}
	if _, err := harness.store.ResolveFindingWithProjectAuthority(finding.FindingID, FindingDecision{
		Kind:             FindingDecisionIndependentBlocking,
		TargetTaskRef:    &outside,
		BlockingBoundary: "outside project",
	}); err == nil {
		t.Fatal("finding outside project authority was admitted")
	}
	after, err := harness.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if after.ControllerGeneration != head.ControllerGeneration || after.ActiveEpisodeID != "" || after.PendingTransitionID != "" {
		t.Fatalf("rejected cycle changed controller authority: before=%#v after=%#v", head, after)
	}
	harness.assertInvariant(t, "cycle rejection")
}

func TestIntegratedLifecycleContextProliferationReplay(t *testing.T) {
	t.Parallel()
	harness := newLifecycleHarness(t)
	rootSealed := harness.edit(t, harness.source, "root-owned.txt", "root work\n")
	episode := harness.planBlockingEpisode(t, rootSealed, harness.blocker, "root-blocker")
	suspended, err := harness.store.SuspendExecution(rootSealed, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	live, err := harness.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: suspended.Head.ControllerGeneration,
		EpisodeID:          episode.EpisodeID,
		EpisodeRevision:    episode.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.source = *live.Admission
	harness.assertInvariant(t, "derived execution live")

	harness.assertNoSecondMutatingAuthority(t, "derived execution live")

	if _, err := harness.store.AdmitMutation(rootSealed.MutationAuthority(), live.Admission.Workspace, live.Admission.Snapshot); err == nil {
		t.Fatal("sealed root lease admitted against the derived lane")
	}

	recovery := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/recovery-of-recovery.md", ContractDigest: "recovery"}
	finding, err := harness.store.ObserveFinding(harness.source, FindingObservationInput{
		Producer: "reviewer-a", ProofClass: FindingProofUnverified, ProblemKey: "proliferation-defect",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.store.ResolveFindingWithProjectAuthority(finding.FindingID, FindingDecision{
		Kind:             FindingDecisionIndependentBlocking,
		TargetTaskRef:    &recovery,
		BlockingBoundary: "proliferation attempt",
	}); err == nil {
		t.Fatal("recovery-of-recovery target was admitted as blocker authority")
	}

	deeper := harness.planBlockingEpisode(t, harness.source, harness.deeper, "blocker-blocker")
	if deeper.EpisodeID != episode.EpisodeID {
		t.Fatalf("serial replan minted a second episode identity: %#v", deeper)
	}
	harness.assertInvariant(t, "proliferation replay")
}

func TestIntegratedLifecycleRestoreConflictPreserved(t *testing.T) {
	t.Parallel()
	harness := newLifecycleHarness(t)
	rootSuspended := harness.edit(t, harness.source, "conflicted.txt", "root-owned conflict\n")
	episode := harness.planBlockingEpisode(t, rootSuspended, harness.blocker, "root-blocker")
	suspended, err := harness.store.SuspendExecution(rootSuspended, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	harness.assertInvariant(t, "root suspended for conflicting blocker")

	live, err := harness.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: suspended.Head.ControllerGeneration,
		EpisodeID:          episode.EpisodeID,
		EpisodeRevision:    episode.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.source = *live.Admission
	blocker := harness.edit(t, harness.source, "conflicted.txt", "blocker-owned conflict\n")
	published, candidate := harness.publishLifecycleTask(t, blocker, "conflicting blocker result\n")
	retired := harness.retireLifecycleTask(t, published, candidate)
	cleaned, err := harness.store.CleanupExecution(CleanupExecutionInput{
		ExpectedGeneration: retired.Head.ControllerGeneration,
		WorkspaceID:        blocker.Workspace.ID,
		SealRef:            candidate.SealRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.assertInvariant(t, "conflicting blocker cleaned")

	if _, err := harness.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: cleaned.Head.ControllerGeneration,
		EpisodeID:          episode.EpisodeID,
		EpisodeRevision:    retired.Head.ActiveEpisodeRevision,
		SuspensionID:       suspended.Suspension.SnapshotID,
	}); err == nil {
		t.Fatal("conflicting rebind was silently normalized")
	}
	head, err := harness.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.Status != ControllerStatusFailClosed || head.FailureID == "" {
		t.Fatalf("restore conflict did not fail the controller closed: %#v", head)
	}
	if head.LiveLeaseID != "" || head.LiveAttemptID != "" || head.PendingTransitionID != "" {
		t.Fatalf("conflicting rebind left partial authority: %#v", head)
	}
	if _, err := harness.store.LoadSuspension(harness.repo, suspended.Suspension.SnapshotID); err != nil {
		t.Fatalf("failed rebind lost the suspension: %v", err)
	}
	if _, err := harness.store.ProveCleanupDurability(*suspended.SealRef); err != nil {
		t.Fatalf("failed rebind lost cleanup durability: %v", err)
	}
	if _, err := harness.store.BuildAttemptEvidenceBundle(*suspended.SealRef); err != nil {
		t.Fatal(err)
	}
}

func TestIntegratedLifecyclePublicationRaceAdoptsSafely(t *testing.T) {
	t.Parallel()
	harness := newLifecycleHarness(t)
	rootSuspended := harness.edit(t, harness.source, "root-owned.txt", "root work\n")
	episode := harness.planBlockingEpisode(t, rootSuspended, harness.blocker, "root-blocker")
	suspended, err := harness.store.SuspendExecution(rootSuspended, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	live, err := harness.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: suspended.Head.ControllerGeneration,
		EpisodeID:          episode.EpisodeID,
		EpisodeRevision:    episode.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.source = *live.Admission
	blocker := harness.edit(t, harness.source, "blocker-result.txt", "blocker deliverable\n")

	evidence := publicationTestEvidence(t, harness.store, blocker, harness.policy)
	accepted, err := harness.store.AcceptExecutionCandidate(blocker, CandidateAcceptanceInput{Message: "raced result\n", Policy: harness.policy, Evidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := harness.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	external := advancePublicationTestRemote(t, findingAcceptanceFixture{store: harness.store, source: harness.source}, harness.policy, "external.txt", "external advancement\n")
	if tip := harness.remoteTip(t); tip != external {
		t.Fatalf("remote moved before any controller action: %s != %s", tip, external)
	}

	rebound, err := harness.store.RebindUnpublishedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	harness.assertInvariant(t, "external advancement adopted")
	current, err := harness.store.LoadAcceptedCandidate(*rebound.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if current.EvidenceValid || current.BaseOID != external || current.CandidateID == candidate.CandidateID {
		t.Fatalf("rebind kept stale evidence/base/identity: %#v", current)
	}
	if _, err := harness.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: rebound.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err == nil {
		t.Fatal("superseded candidate re-published after adoption")
	}

	checked, err := harness.store.RevalidateAcceptedCandidate(PublicationInput{ExpectedGeneration: rebound.Head.ControllerGeneration, CandidateID: current.CandidateID}, storePublicationTestEvidence(t, harness.store, current))
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := harness.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: checked.Head.ControllerGeneration, CandidateID: current.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	published, err := harness.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: current.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	if published.Head.IntegrationTip != current.CommitOID || published.Head.ObservedPrefix != current.CommitOID {
		t.Fatalf("post-adoption publication is not canonical: %#v", published.Head)
	}
	harness.assertInvariant(t, "post-race publication")
	if _, err := runGitBinary(harness.repo, nil, "merge-base", "--is-ancestor", external, current.CommitOID); err != nil {
		t.Fatalf("external advancement was dropped from the integrated base: %v", err)
	}
}

func TestIntegratedLifecycleEvidenceSurvivesGCAndPathReuse(t *testing.T) {
	t.Parallel()
	harness := newLifecycleHarness(t)
	rootSuspended := harness.edit(t, harness.source, "root-owned.txt", "root work\n")
	episode := harness.planBlockingEpisode(t, rootSuspended, harness.blocker, "root-blocker")
	suspended, err := harness.store.SuspendExecution(rootSuspended, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	live, err := harness.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: suspended.Head.ControllerGeneration,
		EpisodeID:          episode.EpisodeID,
		EpisodeRevision:    episode.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.source = *live.Admission
	blocker := harness.edit(t, harness.source, "blocker-result.txt", "blocker deliverable\n")
	published, candidate := harness.publishLifecycleTask(t, blocker, "blocker result\n")
	retired := harness.retireLifecycleTask(t, published, candidate)
	cleaned, err := harness.store.CleanupExecution(CleanupExecutionInput{
		ExpectedGeneration: retired.Head.ControllerGeneration,
		WorkspaceID:        blocker.Workspace.ID,
		SealRef:            candidate.SealRef,
	})
	if err != nil {
		t.Fatal(err)
	}

	historicalSeals := []EvidenceObjectRef{*suspended.SealRef, candidate.SealRef}
	before := map[string]string{}
	for _, seal := range historicalSeals {
		bundle, err := harness.store.BuildAttemptEvidenceBundle(seal)
		if err != nil {
			t.Fatal(err)
		}
		before[seal.Digest] = bundle.EvidenceGraphDigest
	}

	runControllerGit(t, harness.repo, "reflog", "expire", "--expire=now", "--all")
	runControllerGit(t, harness.repo, "gc", "--aggressive", "--prune=now")

	fresh, err := harness.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: cleaned.Head.ControllerGeneration,
		EpisodeID:          episode.EpisodeID,
		EpisodeRevision:    retired.Head.ActiveEpisodeRevision,
		SuspensionID:       suspended.Suspension.SnapshotID,
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.source = *fresh.Admission
	if fresh.Admission.Workspace.Root != blocker.Workspace.Root || fresh.Admission.Workspace.ID == blocker.Workspace.ID {
		t.Fatalf("same-path lane reuse did not mint a fresh identity: %#v", fresh.Admission.Workspace)
	}
	newEdit := harness.edit(t, harness.source, "post-gc-work.txt", "work after gc\n")
	newFinding, err := harness.store.ObserveFinding(newEdit, FindingObservationInput{
		Producer: "post-gc", ProofClass: FindingProofUnverified, ProblemKey: "post-gc-observation",
	})
	if err != nil {
		t.Fatal(err)
	}
	if newFinding.FindingID == "" {
		t.Fatal("post-reuse observation lost identity")
	}

	for _, seal := range historicalSeals {
		bundle, err := harness.store.BuildAttemptEvidenceBundle(seal)
		if err != nil {
			t.Fatalf("historical bundle lost integrity after GC/path reuse: %v", err)
		}
		if bundle.EvidenceGraphDigest != before[seal.Digest] {
			t.Fatalf("historical bundle digest changed after path reuse: %s", seal.Digest)
		}
	}

	path := filepath.Join(harness.store.dir, "evidence", "objects", historicalSeals[0].Digest[:2], historicalSeals[0].Digest)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(original, 'x'), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.WriteFile(path, original, 0o600) })
	if _, err := harness.store.BuildAttemptEvidenceBundle(historicalSeals[0]); err == nil {
		t.Fatal("corrupt required evidence produced a bundle")
	}
}

func TestIntegratedLifecycleOrdinarySuccessorRootExecution(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "root result\n")
	published, candidate := publishTerminalTestSource(t, fixture, policy, source)
	retired, err := fixture.store.RetireTerminalTask(TerminalTaskInput{
		ExpectedGeneration: published.Head.ControllerGeneration,
		ProjectSnapshotID:  published.Head.ProjectSnapshotID,
		CandidateID:        candidate.CandidateID,
		TaskRef:            candidate.TaskRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	if retired.Head.RootTaskRef == nil || retired.Head.RootTaskRef.Equal(source.Attempt.SemanticTaskRef) {
		t.Fatalf("terminal retirement did not establish successor root authority: %#v", retired.Head.RootTaskRef)
	}
	if retired.Head.ActiveEpisodeID != "" || retired.Head.LiveLeaseID != "" || retired.Head.LiveAttemptID != "" || retired.Head.PendingTransitionID != "" {
		t.Fatalf("retired root left live or episode authority: %#v", retired.Head)
	}

	workspace, err := ResolveWorkspaceIdentity(fixture.source.Workspace.Root, fixture.store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureWorkspaceSnapshot(fixture.source.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.BootstrapExecution(*retired.Head.RootTaskRef, workspace, snapshot); err == nil {
		t.Fatal("already-active controller re-bootstrapped mutation authority")
	}
	if _, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: retired.Head.ControllerGeneration - 1}); err == nil {
		t.Fatal("stale generation minted the successor execution")
	}

	successor, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: retired.Head.ControllerGeneration,
	})
	if err != nil {
		t.Fatal(err)
	}
	if successor.Admission == nil || !successor.Admission.Attempt.SemanticTaskRef.Equal(*retired.Head.RootTaskRef) {
		t.Fatalf("successor authority mismatch: %#v", successor.Admission)
	}
	if !successor.Admission.Attempt.RootTaskRef.Equal(*retired.Head.RootTaskRef) {
		t.Fatalf("successor execution lost focus root identity: %#v", successor.Admission.Attempt)
	}
	if successor.Admission.Workspace.Root == source.Workspace.Root {
		t.Fatal("successor execution materialized in the primary workspace")
	}
	if successor.Admission.Attempt.PredecessorAttemptID != "" || successor.Admission.Attempt.ResumedFromSealID != "" {
		t.Fatalf("fresh successor incorrectly bound to a suspension lineage: %#v", successor.Admission.Attempt)
	}

	if _, err := fixture.store.BootstrapExecution(*retired.Head.RootTaskRef, workspace, snapshot); err == nil {
		t.Fatal("bootstrap minted authority while the successor was live")
	}
	if _, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: successor.Head.ControllerGeneration}); err == nil {
		t.Fatal("second mutating lane minted while the successor was live")
	}
	if _, err := fixture.store.AdmitMutation(source.MutationAuthority(), successor.Admission.Workspace, successor.Admission.Snapshot); err == nil {
		t.Fatal("retired root lease admitted against the successor lane")
	}
	if _, err := fixture.store.BuildAttemptEvidenceBundle(candidate.SealRef); err != nil {
		t.Fatal(err)
	}
}
