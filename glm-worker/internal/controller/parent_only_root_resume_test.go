package controller

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentOnlyRootResumeUsesTypedMaterializationAndClosesEpisode(t *testing.T) {
	t.Parallel()
	fixture, retiredChild := newParentOnlyTaskFixture(t, ExecutionAuthorityParentOnly)
	successorPath := "IMPLEMENTATION_TASKS/successor.md"
	if err := os.WriteFile(filepath.Join(fixture.repo, successorPath), []byte(parentOnlyTaskContent(ExecutionAuthorityParentOnly)), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, fixture.repo, "add", successorPath)
	runControllerGit(t, fixture.repo, "commit", "-q", "-m", "designate parent-only successor Root")
	runControllerGit(t, fixture.repo, "push", "origin", "HEAD:"+fixture.policy.RemoteRef)

	writeParentOnlyProduct(t, fixture, "evaluation-result.md", "parent evaluation product\n")
	admitParentOnlyExecutionFixture(t, fixture)
	admission := parentOnlyCompletionAdmission(t, fixture)
	accepted, err := fixture.store.AcceptParentCompletion(admission, CandidateAcceptanceInput{
		Message: "retire child", Policy: fixture.policy, RequiredValidations: declaredNoValidations(),
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{
		ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID,
	})
	if err != nil {
		t.Fatal(err)
	}
	retired, _ := prepareCompletionTestCandidate(t, fixture.store, promoted, candidate)
	published, err := fixture.store.RecoverExecutionOperation(retired.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if published.Head.MetadataLineageRef == nil || published.Head.ExecutionTaskRef != nil || published.Head.LiveLeaseID != "" {
		t.Fatalf("child publication did not reach a retired successor: %#v", published.Head)
	}
	root, err := ResolveCommittedTaskAuthority(fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	if root.Task.TaskPath != successorPath || root.ExecutionMode != ExecutionAuthorityParentOnly {
		t.Fatalf("successor Root lacks committed parent-only authority: %#v", root)
	}

	// Seed the resumed-Root episode as a post-child-publication fixture; the
	// operation under test must perform its own closure without state edits.
	episode := BlockerEpisodeRevision{
		SchemaVersion: controllerSchemaVersion,
		EpisodeID: "parent-only-root-episode",
		Revision: 1,
		ProjectSnapshotID: published.Head.ProjectSnapshotID,
		RootTaskRef: root.Task,
		ScopeRootTaskRef: retiredChild,
		SatisfiedTaskRefs: []SemanticTaskRef{retiredChild},
		AdmittedClosure: []SemanticTaskRef{retiredChild},
		AdmittedOrder: []SemanticTaskRef{retiredChild},
		State: EpisodeStateResumingRoot,
		CreatedAt: time.Now().UTC(),
	}
	episode.RevisionID = blockerEpisodeRevisionID(episode)
	if err := fixture.store.writeEpisodeRecord(BlockerEpisodeRecord{
		SchemaVersion: controllerSchemaVersion, EpisodeID: episode.EpisodeID,
		RepositoryIdentity: fixture.store.Identity().LineageID,
		RootTaskRef: root.Task, ScopeRootTaskRef: retiredChild,
		CreatedAt: episode.CreatedAt,
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.writeEpisodeRevision(episode); err != nil {
		t.Fatal(err)
	}
	head := published.Head
	head.ControllerGeneration++
	head.ActiveEpisodeID, head.ActiveEpisodeRevision = episode.EpisodeID, episode.Revision
	if err := fixture.store.writeHeadCAS(published.Head.ControllerGeneration, head); err != nil {
		t.Fatal(err)
	}
	projected, err := fixture.store.ProjectParentCompletion()
	if err != nil || projected.Operation != PublicationOperationParentOnlyMaterialize || projected.Execution == nil ||
		projected.Execution.EpisodeID != episode.EpisodeID || projected.Execution.EpisodeRevision != episode.Revision {
		t.Fatalf("resuming parent-only Root did not project typed materialization: %#v %v", projected, err)
	}
	if _, err := fixture.store.AdmitParentOnlyExecution(ParentOnlyMaterializeInput{
		ExpectedGeneration: head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: episode.Revision + 1,
	}); err == nil {
		t.Fatal("stale Root episode revision was admitted")
	}
	if after, err := fixture.store.LoadHead(); err != nil || after.ControllerGeneration != head.ControllerGeneration {
		t.Fatalf("rejected episode mutation changed controller generation: %#v %v", after, err)
	}
	materialized, err := fixture.store.AdmitParentOnlyExecution(ParentOnlyMaterializeInput{
		ExpectedGeneration: projected.Execution.ExpectedGeneration,
		EpisodeID: projected.Execution.EpisodeID, EpisodeRevision: projected.Execution.EpisodeRevision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if materialized.Admission == nil || materialized.Head.ActiveEpisodeID != "" || materialized.Head.ActiveEpisodeRevision != 0 ||
		materialized.Head.LiveAttemptID != materialized.Admission.Attempt.AttemptID ||
		materialized.Admission.Attempt.EpisodeID != episode.EpisodeID ||
		materialized.Admission.Attempt.EpisodeRevision != episode.Revision {
		t.Fatalf("parent-only Root resume did not bind one admitted attempt and close episode: %#v", materialized)
	}
	closed, err := fixture.store.LoadEpisodeRevision(episode.EpisodeID, episode.Revision+1)
	if err != nil || closed.State != EpisodeStateClosed {
		t.Fatalf("resuming-root episode was not closed in the materialization: %#v %v", closed, err)
	}
	replayed, err := fixture.store.RecoverExecutionOperation(materialized.TransitionID)
	if err != nil || replayed.Head.LiveAttemptID != materialized.Head.LiveAttemptID {
		t.Fatalf("materialization recovery did not retain the attempt: %#v %v", replayed, err)
	}
	smoke := state.AttachStateStore(WorkflowTaskConfig(fixture.store.config, fixture.store.Identity(), root.Task))
	if id, err := smoke.TaskID(); err != nil || id != materialized.Admission.Attempt.AttemptID {
		t.Fatalf("parent-only Root resume did not bind smoke identity: %q %v", id, err)
	}
	if _, err := smoke.LoadControllerRuntimeBinding(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("parent-only Root resume created GLM Runtime binding: %v", err)
	}
}

func TestParentOnlyRootResumeRejectsUnpublishedEpisode(t *testing.T) {
	t.Parallel()
	fixture, task := newParentOnlyTaskFixture(t, ExecutionAuthorityParentOnly)
	head, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	head.RootTaskRef = &task
	head.ActiveEpisodeID = "unpublished-episode"
	head.ActiveEpisodeRevision = 1
	_, err = fixture.store.parentOnlyRootResumeEpisode(head, ParentOnlyMaterializeInput{
		ExpectedGeneration: head.ControllerGeneration,
		EpisodeID: head.ActiveEpisodeID,
		EpisodeRevision: head.ActiveEpisodeRevision,
	}, task)
	if err == nil || !strings.Contains(err.Error(), "finalized child publication") {
		t.Fatalf("unpublished Root episode admitted parent-only resumption: %v", err)
	}
}
