package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublishedBlockerSurvivesParentRestoreConflict(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
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
	published, err := fixture.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: c.CandidateID})
	if err != nil {
		t.Fatal(err)
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
	if head.IntegrationTip != c.CommitOID || remote != c.CommitOID || head.LiveLeaseID != "" {
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

func TestPublishedCandidateDescendantRequiresAdoptionAndFreshEvidence(t *testing.T) {
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
	runControllerGit(t, fixture.store.identity.PrimaryRoot, "push", "origin", c.CommitOID+":"+policy.RemoteRef)
	remote := advancePublicationTestRemote(t, fixture, policy, "descendant.txt", "external descendant\n")
	published, err := fixture.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: c.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	if published.Head.ObservedRemoteTip != remote || published.Head.IntegrationTip != c.CommitOID {
		t.Fatal("containing race was not recorded exactly")
	}
	adopted, err := fixture.store.AdoptExternalAdvancement(ExternalAdvancementInput{ExpectedGeneration: published.Head.ControllerGeneration, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	current, err := fixture.store.LoadAcceptedCandidate(*adopted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if current.CandidateID != c.CandidateID || current.DescendantTip != remote || len(current.DescendantEvidence) != 0 {
		t.Fatal("immutable candidate was rewritten or stale descendant proof reused")
	}
	tree := controllerGitOutput(t, fixture.store.identity.PrimaryRoot, "rev-parse", remote+"^{tree}")
	validated, err := fixture.store.RevalidateAcceptedCandidate(PublicationInput{ExpectedGeneration: adopted.Head.ControllerGeneration, CandidateID: c.CandidateID}, storePublicationTestEvidence(t, fixture.store, canonicalDescendantEvidenceView(current, tree)))
	if err != nil {
		t.Fatal(err)
	}
	if validated.Head.ObservedRemoteTip != remote {
		t.Fatal("revalidation lost observed descendant")
	}
	if _, err := fixture.store.BuildAttemptEvidenceBundle(c.SealRef); err != nil {
		t.Fatal(err)
	}
}
