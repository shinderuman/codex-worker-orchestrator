package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecutionLaneSuspendsPreservedWorkAndMaterializesOnlyScheduledBlocker(t *testing.T) {
	fixture := newFindingAcceptanceFixture(t)
	writeSuspensionTestFile(t, fixture.source.Workspace.Root, "source.txt", "valuable unfinished Task\n")
	after, err := CaptureWorkspaceSnapshot(fixture.source.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	fixture.source, err = fixture.store.RecordAdmittedMutation(fixture.source, "edit source", "success", after)
	if err != nil {
		t.Fatal(err)
	}
	episode := planSuspensionTestEpisode(t, fixture)
	suspended, err := fixture.store.SuspendExecution(fixture.source, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if suspended.Head.LiveLeaseID != "" || suspended.Head.LiveAttemptID != "" || suspended.Suspension == nil || suspended.SealRef == nil {
		t.Fatalf("suspension did not preserve quiescent authority: %#v", suspended)
	}
	if _, err := fixture.store.AdmitMutation(fixture.source.MutationAuthority(), fixture.source.Workspace, fixture.source.Snapshot); err == nil {
		t.Fatal("revoked root lease was admitted")
	}
	seal, err := fixture.store.LoadAttemptSeal(*suspended.SealRef)
	if err != nil {
		t.Fatal(err)
	}
	if seal.CurrentWorktreeTree != suspended.Suspension.Current.WorktreeTree || seal.OperationalSnapshotID != suspended.Suspension.SnapshotID {
		t.Fatal("seal did not bind operational suspension")
	}
	if _, err := fixture.store.ProveCleanupDurability(*suspended.SealRef); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.GarbageCollectSuspension(suspended.Head.ControllerGeneration, suspended.Suspension.SnapshotID); err == nil {
		t.Fatal("root suspension GC accepted before episode closure")
	}
	input := MaterializeExecutionInput{ExpectedGeneration: suspended.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: episode.Revision}
	materialized, err := fixture.store.MaterializeExecution(input)
	if err != nil {
		t.Fatal(err)
	}
	if materialized.Admission == nil || !materialized.Admission.Attempt.SemanticTaskRef.Equal(fixture.child) || !materialized.Head.RootTaskRef.Equal(fixture.source.Attempt.RootTaskRef) {
		t.Fatalf("scheduled blocker authority mismatch: %#v", materialized)
	}
	if materialized.Admission.Workspace.Root == fixture.source.Workspace.Root {
		t.Fatal("blocker materialized in root workspace")
	}
	if _, err := fixture.store.MaterializeExecution(input); err == nil {
		t.Fatal("second lane/lease admitted")
	}
	data, err := os.ReadFile(filepath.Join(fixture.source.Workspace.Root, "source.txt"))
	if err != nil || string(data) != "valuable unfinished Task\n" {
		t.Fatal("root's unfinished work was modified")
	}
	if _, err := fixture.store.RecoverExecutionOperation(materialized.TransitionID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.BuildAttemptEvidenceBundle(*suspended.SealRef); err != nil {
		t.Fatal(err)
	}
}

func TestSuspensionUnsupportedStateKeepsOriginalLease(t *testing.T) {
	fixture := newFindingAcceptanceFixture(t)
	runControllerGit(t, fixture.source.Workspace.Root, "update-index", "--assume-unchanged", "source.txt")
	after, err := CaptureWorkspaceSnapshot(fixture.source.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	fixture.source, err = fixture.store.RecordAdmittedMutation(fixture.source, "unsupported index change", "success", after)
	if err != nil {
		t.Fatal(err)
	}
	episode := planSuspensionTestEpisode(t, fixture)
	if _, err := fixture.store.SuspendExecution(fixture.source, episode.EpisodeID, episode.Revision); err == nil {
		t.Fatal("unsupported suspension accepted")
	}
	head, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.LiveLeaseID != fixture.source.Lease.LeaseID || head.PendingTransitionID != "" {
		t.Fatal("failed preflight revoked or reserved execution authority")
	}
}

func planSuspensionTestEpisode(t *testing.T, fixture findingAcceptanceFixture) BlockerEpisodeRevision {
	t.Helper()
	finding := observeAcceptanceFinding(t, fixture, "lossless-suspension-child", "reviewer")
	result, err := fixture.store.ResolveFindingWithProjectAuthority(finding.FindingID, FindingDecision{Kind: FindingDecisionIndependentBlocking, TargetTaskRef: &fixture.child, BlockingBoundary: "root requires blocker integration"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Episode == nil {
		t.Fatal("blocker episode was not planned")
	}
	return *result.Episode
}
