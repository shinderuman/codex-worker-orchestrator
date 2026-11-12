package controller

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentOnlyRootResumeUsesTypedMaterializationAndClosesEpisode(t *testing.T) {
	t.Parallel()
	repo, store, policy, initial, episode := startParentOnlyBlockerEpisode(t)
	publishParentOnlyBlocker(t, store, initial, episode, policy)
	fixture := parentOnlyTaskFixture{repo: repo, store: store, policy: policy}
	root, err := ResolveCommittedTaskAuthority(repo)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	episode, err = store.LoadEpisodeRevision(head.ActiveEpisodeID, head.ActiveEpisodeRevision)
	if err != nil {
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
		EpisodeID:          projected.Execution.EpisodeID, EpisodeRevision: projected.Execution.EpisodeRevision,
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
	if _, err := fixture.store.QuiescentRuntimeAdmission(head); err == nil || !strings.Contains(err.Error(), "completed child metadata target") {
		t.Fatalf("unfinished blocker episode bypassed normal Runtime readiness: %v", err)
	}
	_, err = fixture.store.parentOnlyRootResumeEpisode(head, ParentOnlyMaterializeInput{
		ExpectedGeneration: head.ControllerGeneration,
		EpisodeID:          head.ActiveEpisodeID,
		EpisodeRevision:    head.ActiveEpisodeRevision,
	}, task)
	if err == nil || !strings.Contains(err.Error(), "finalized child publication") {
		t.Fatalf("unpublished Root episode admitted parent-only resumption: %v", err)
	}
}
