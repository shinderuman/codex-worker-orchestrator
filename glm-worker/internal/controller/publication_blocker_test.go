package controller

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPublishedBlockerSurvivesParentRestoreConflict(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	rootRuntime := fixture.store.dispositionRuntime(fixture.source.Attempt.RootTaskRef)
	if err := os.MkdirAll(rootRuntime.Path("."), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := rootRuntime.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	fixture.source = publicationTestEdit(t, fixture, "source.txt", "root unfinished conflicting work\n")
	episode := planSuspensionTestEpisode(t, fixture)
	suspended, err := fixture.store.SuspendExecution(fixture.source, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	child, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: suspended.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: episode.Revision})
	if err != nil {
		t.Fatal(err)
	}
	fixture.source = *child.Admission
	source := publicationTestEdit(t, fixture, "source.txt", "published blocker result\n")
	seedFinalValidationNoneObligation(t, fixture.store, source)
	accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "publish blocker result", Policy: policy, Evidence: publicationTestEvidence(t, fixture.store, source, policy)})
	if err != nil {
		t.Fatal(err)
	}
	c, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: c.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	if input, err := fixture.store.PreviewRuntimeDisposition(promoted.Head); err != nil || input != nil {
		t.Fatalf("suspended root offered for disposition: %v", err)
	}
	if _, err := fixture.store.runtimeDispositionTarget(promoted.Head); err == nil {
		t.Fatal("suspended root admitted as disposal target")
	}
	published, err := fixture.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: c.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	if rootRuntime.TaskStatus() != state.TaskStatusActive || rootRuntime.Exists(state.RuntimeDispositionFile) {
		t.Fatal("blocker publication terminated suspended root")
	}
	progress, err := fixture.store.LoadEpisodeRevision(episode.EpisodeID, published.Head.ActiveEpisodeRevision)
	if err != nil {
		t.Fatal(err)
	}
	if !taskPathSatisfied(progress.SatisfiedTaskRefs, c.TaskRef.TaskPath) {
		t.Fatal("published blocker not fulfilled")
	}
	cleaned, err := fixture.store.CleanupExecution(CleanupExecutionInput{ExpectedGeneration: published.Head.ControllerGeneration, WorkspaceID: source.Workspace.ID, SealRef: c.SealRef})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: cleaned.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: progress.Revision, SuspensionID: suspended.Suspension.SnapshotID}); err == nil {
		t.Fatal("conflicting parent restore accepted")
	}
	head, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	remote, err := observePublicationRemote(fixture.store.identity.PrimaryRoot, policy)
	if err != nil {
		t.Fatal(err)
	}
	if head.IntegrationTip != published.Head.IntegrationTip || remote != published.Head.IntegrationTip || head.LiveLeaseID != "" {
		t.Fatal("parent failure rolled back published blocker")
	}
	if _, err := os.Lstat(source.Workspace.Root); !os.IsNotExist(err) {
		t.Fatal("sealed lane was not disposable")
	}
	if _, err := fixture.store.BuildAttemptEvidenceBundle(c.SealRef); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(fixture.store.dir, "suspensions", suspended.Suspension.SnapshotID+".json")); err != nil {
		t.Fatal("parent failure lost suspension manifest")
	}
}

func TestPublishedCompletionPreservesContainingRaceAndRejectsUnvalidatedSourceAdoption(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "root.txt", "root result\n")
	accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "root", Policy: policy, Evidence: publicationTestEvidence(t, fixture.store, source, policy)})
	if err != nil {
		t.Fatal(err)
	}
	c, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: c.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	retired, candidate := prepareCompletionTestCandidate(t, fixture.store, promoted, c)
	c = candidate
	var op ExecutionOperation
	if err := readJSON(fixture.store.executionOperationPath(retired.TransitionID), &op); err != nil {
		t.Fatal(err)
	}
	phase, err := fixture.store.loadTransitionState(retired.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.beginCandidateCompletionWrite(op, &phase); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, fixture.store.identity.PrimaryRoot, "push", "origin", c.CommitOID+":"+policy.RemoteRef)
	remote := advancePublicationTestRemote(t, fixture, policy, "descendant.txt", "external descendant\n")
	published, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if published.Head.ObservedRemoteTip != remote || published.Head.IntegrationTip != c.CommitOID {
		t.Fatal("containing race was not recorded exactly")
	}
	if published.Head.AcceptedCandidateRef != nil || published.Head.LiveLeaseID != "" {
		t.Fatal("publication retained old execution authority")
	}
	if _, err := fixture.store.AdoptExternalAdvancement(ExternalAdvancementInput{ExpectedGeneration: published.Head.ControllerGeneration, Policy: policy}); err == nil {
		t.Fatal("retired metadata adoption admitted unvalidated external source")
	}
	after, err := fixture.store.LoadHead()
	if err != nil || !reflect.DeepEqual(after, published.Head) {
		t.Fatal("rejected external source adoption changed completion authority")
	}
	if _, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: after.ControllerGeneration}); err == nil {
		t.Fatal("new execution ignored observed external advancement")
	}
	if got := observeTestRemote(t, fixture.store.identity.PrimaryRoot, policy); got != remote {
		t.Fatal("external descendant was overwritten")
	}
	if _, err := fixture.store.BuildAttemptEvidenceBundle(c.SealRef); err != nil {
		t.Fatal(err)
	}
}
