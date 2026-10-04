package controller

import (
	"os"
	"testing"
	"time"
)

func TestEvidenceGraphMemoizesValidatedRevisionChains(t *testing.T) {
	store := newEvidenceTestStore(t)
	task := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/memoized.md", ContractDigest: "memoized-task"}
	subject := taskEvidenceSubjectID(task)

	firstTask := testTaskIndexRevision(task, 1, "transition-1", time.Unix(9000, 0).UTC())
	firstTaskRef, _, err := store.StoreTaskIndexRevision(firstTask)
	if err != nil {
		t.Fatal(err)
	}
	secondTask := testTaskIndexRevision(task, 2, "transition-2", time.Unix(9001, 0).UTC())
	secondTask.PreviousRevision = &firstTaskRef
	secondTaskRef, _, err := store.StoreTaskIndexRevision(secondTask)
	if err != nil {
		t.Fatal(err)
	}

	firstEpisode := EpisodeIndexRevision{
		SchemaVersion:        evidenceSchemaVersion,
		EpisodeID:            "episode-memoized",
		EpisodeRevision:      1,
		ControllerGeneration: 1,
		TransitionID:         "transition-1",
		CreatedAt:            time.Unix(9002, 0).UTC(),
	}
	firstEpisodeRef, _, err := store.StoreEpisodeIndexRevision(firstEpisode)
	if err != nil {
		t.Fatal(err)
	}
	secondEpisode := firstEpisode
	secondEpisode.EpisodeRevision = 2
	secondEpisode.ControllerGeneration = 2
	secondEpisode.TransitionID = "transition-2"
	secondEpisode.CreatedAt = time.Unix(9003, 0).UTC()
	secondEpisode.PreviousRevision = &firstEpisodeRef
	secondEpisodeRef, _, err := store.StoreEpisodeIndexRevision(secondEpisode)
	if err != nil {
		t.Fatal(err)
	}

	walker := evidenceGraphWalker{
		store:                  store,
		refs:                   map[string]EvidenceObjectRef{},
		expanded:               map[string]bool{},
		validatedTaskChains:    map[string]uint64{},
		validatedEpisodeChains: map[string]uint64{},
	}
	if err := walker.walkTaskRevisionChain(subject, secondTaskRef, 2); err != nil {
		t.Fatal(err)
	}
	if len(walker.validatedTaskChains) != 2 {
		t.Fatalf("validated task-chain cache = %d want 2", len(walker.validatedTaskChains))
	}
	if err := walker.walkTaskRevisionChain(subject, firstTaskRef, 2); err != nil {
		t.Fatalf("memoized task suffix was rejected: %v", err)
	}
	if err := walker.walkTaskRevisionChain(subject, secondTaskRef, 1); err == nil {
		t.Fatal("memoized task chain bypassed stricter generation authority")
	}

	if err := walker.walkEpisodeRevisionChain(firstEpisode.EpisodeID, secondEpisodeRef, 2); err != nil {
		t.Fatal(err)
	}
	if len(walker.validatedEpisodeChains) != 2 {
		t.Fatalf("validated episode-chain cache = %d want 2", len(walker.validatedEpisodeChains))
	}
	if err := walker.walkEpisodeRevisionChain(firstEpisode.EpisodeID, firstEpisodeRef, 2); err != nil {
		t.Fatalf("memoized episode suffix was rejected: %v", err)
	}
	if err := walker.walkEpisodeRevisionChain(firstEpisode.EpisodeID, secondEpisodeRef, 1); err == nil {
		t.Fatal("memoized episode chain bypassed stricter generation authority")
	}

	if err := os.Remove(store.evidenceObjectPath(firstTaskRef.Digest)); err != nil {
		t.Fatal(err)
	}
	fresh := evidenceGraphWalker{store: store, refs: map[string]EvidenceObjectRef{}, expanded: map[string]bool{}}
	if err := fresh.walkTaskRevisionChain(subject, secondTaskRef, 2); err == nil {
		t.Fatal("fresh graph validation accepted a missing required revision object")
	}
}
