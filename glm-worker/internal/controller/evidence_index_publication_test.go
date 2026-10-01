package controller

import (
	"testing"
	"time"
)

func TestEvidencePublicationRejectsTaskIndexBoundToDifferentTransition(t *testing.T) {
	fixture := newExecutionSwitchFixture(t)
	_, record := prepareExecutionSwitchTransition(t, fixture, 1)
	actual := executionSwitchActual(fixture, fixture.source)
	revision := testTaskIndexRevision(
		fixture.source.Attempt.SemanticTaskRef,
		record.CommittedGeneration,
		"different-transition",
		time.Unix(8000, 0).UTC(),
	)
	if _, _, err := fixture.store.CommitAuthorityTransitionWithEvidence(
		record,
		actual,
		EvidencePublicationInput{TaskRevisions: []TaskIndexRevision{revision}},
		nil,
	); err == nil {
		t.Fatal("task index revision bound to a different transition was published")
	}
}

func TestEvidencePublicationRejectsEpisodeTaskHeadOutsidePublishedTaskAuthority(t *testing.T) {
	fixture := newExecutionSwitchFixture(t)
	_, record := prepareExecutionSwitchTransition(t, fixture, 1)
	actual := executionSwitchActual(fixture, fixture.source)
	task := fixture.source.Attempt.SemanticTaskRef
	unpublishedTask := testTaskIndexRevision(task, record.CommittedGeneration, record.TransitionID, time.Unix(8100, 0).UTC())
	unpublishedTaskRef, _, err := fixture.store.StoreTaskIndexRevision(unpublishedTask)
	if err != nil {
		t.Fatal(err)
	}
	episode := testEpisodeIndexRevisionForPublication(
		"episode-unpublished-task",
		task,
		record,
		unpublishedTaskRef,
		time.Unix(8101, 0).UTC(),
	)
	if _, _, err := fixture.store.CommitAuthorityTransitionWithEvidence(
		record,
		actual,
		EvidencePublicationInput{EpisodeRevisions: []EpisodeIndexRevision{episode}},
		nil,
	); err == nil {
		t.Fatal("episode task index head outside published task authority was accepted")
	}
}

func TestEvidencePublicationAcceptsEpisodeTaskHeadPublishedInSameTransition(t *testing.T) {
	fixture := newExecutionSwitchFixture(t)
	_, record := prepareExecutionSwitchTransition(t, fixture, 1)
	actual := executionSwitchActual(fixture, fixture.source)
	task := fixture.source.Attempt.SemanticTaskRef
	taskRevision := testTaskIndexRevision(task, record.CommittedGeneration, record.TransitionID, time.Unix(8200, 0).UTC())
	taskRef, _, err := fixture.store.StoreTaskIndexRevision(taskRevision)
	if err != nil {
		t.Fatal(err)
	}
	episode := testEpisodeIndexRevisionForPublication(
		"episode-same-transition",
		task,
		record,
		taskRef,
		time.Unix(8201, 0).UTC(),
	)
	episodeRef, _, err := fixture.store.StoreEpisodeIndexRevision(episode)
	if err != nil {
		t.Fatal(err)
	}
	_, publication, err := fixture.store.CommitAuthorityTransitionWithEvidence(
		record,
		actual,
		EvidencePublicationInput{
			TaskRevisionRefs:    []EvidenceObjectRef{taskRef},
			EpisodeRevisionRefs: []EvidenceObjectRef{episodeRef},
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(publication.EvidenceHead.TaskHeads) != 1 || len(publication.EvidenceHead.EpisodeHeads) != 1 {
		t.Fatalf("publication did not retain task/episode authority: %#v", publication.EvidenceHead)
	}
}

func TestEpisodeTaskHeadPublicationAcceptsHistoricalPublishedTaskRevision(t *testing.T) {
	store := newEvidenceTestStore(t)
	task := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/A.md", ContractDigest: "task-a"}
	first := testTaskIndexRevision(task, 1, "transition-1", time.Unix(8300, 0).UTC())
	firstRef, _, err := store.StoreTaskIndexRevision(first)
	if err != nil {
		t.Fatal(err)
	}
	second := testTaskIndexRevision(task, 2, "transition-2", time.Unix(8301, 0).UTC())
	second.PreviousRevision = &firstRef
	secondRef, _, err := store.StoreTaskIndexRevision(second)
	if err != nil {
		t.Fatal(err)
	}
	episode := EpisodeIndexRevision{
		SchemaVersion:             evidenceSchemaVersion,
		EpisodeID:                 "episode-history",
		RootTaskRef:               task,
		EpisodeRevision:           1,
		DependencyGraphSnapshotID: "dependency-history",
		TaskIndexHeads: []EvidenceSubjectHead{{
			SubjectID:   taskEvidenceSubjectID(task),
			RevisionRef: firstRef,
		}},
		State:                "open",
		ControllerGeneration: 2,
		TransitionID:         "transition-2",
		CreatedAt:            time.Unix(8302, 0).UTC(),
	}
	episodeRef, _, err := store.StoreEpisodeIndexRevision(episode)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.validateEpisodeTaskHeadPublication(
		[]EvidenceSubjectHead{{SubjectID: taskEvidenceSubjectID(task), RevisionRef: secondRef}},
		[]EvidenceObjectRef{episodeRef},
	); err != nil {
		t.Fatalf("historical published TaskIndex head was rejected: %v", err)
	}
}

func testEpisodeIndexRevisionForPublication(
	episodeID string,
	task SemanticTaskRef,
	record TransitionRecord,
	taskRef EvidenceObjectRef,
	createdAt time.Time,
) EpisodeIndexRevision {
	return EpisodeIndexRevision{
		SchemaVersion:             evidenceSchemaVersion,
		EpisodeID:                 episodeID,
		RootTaskRef:               task,
		EpisodeRevision:           1,
		DependencyGraphSnapshotID: "dependency-" + episodeID,
		AdmittedClosureTaskRefs:   []SemanticTaskRef{task},
		TaskIndexHeads: []EvidenceSubjectHead{{
			SubjectID:   taskEvidenceSubjectID(task),
			RevisionRef: taskRef,
		}},
		State:                "open",
		ControllerGeneration: record.CommittedGeneration,
		TransitionID:         record.TransitionID,
		CreatedAt:            createdAt,
	}
}
