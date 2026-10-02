package controller

import (
	"testing"
	"time"
)

func TestAttemptSealCoverageStatesRemainEvidenceCoverage(t *testing.T) {
	store, repo := newEvidenceTestStoreWithRepo(t)
	cases := []struct {
		name       string
		coverage   string
		missing    []string
		unreadable []string
	}{
		{name: "complete", coverage: "complete"},
		{name: "open", coverage: "open"},
		{name: "incomplete", coverage: "incomplete", missing: []string{"telemetry"}},
		{name: "corrupt", coverage: "corrupt", unreadable: []string{"validation"}},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := validPortableAttemptSeal(t, store, repo)
			record.AttemptID = "attempt-coverage-" + tc.name
			record.ControllerGeneration = uint64(i + 1)
			record.SealingTransitionID = "transition-coverage-" + tc.name
			record.RevokedLeaseID = "lease-coverage-" + tc.name
			record.WorkspaceID = "workspace-coverage-" + tc.name
			record.SourceProjectSnapshotID = "snapshot-coverage-" + tc.name
			record.Coverage = tc.coverage
			record.Missing = tc.missing
			record.Unreadable = tc.unreadable
			_, stored, err := store.StoreAttemptSeal(record)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Coverage != tc.coverage {
				t.Fatalf("coverage state changed during sealing: got=%q want=%q", stored.Coverage, tc.coverage)
			}
			if stored.Disposition != "accepted" {
				t.Fatalf("evidence coverage unexpectedly changed attempt lifecycle disposition: %q", stored.Disposition)
			}
		})
	}
}

func TestEvidenceLifecycleSeparatesAttemptTaskAndEpisodeTerminality(t *testing.T) {
	store, repo := newEvidenceTestStoreWithRepo(t)
	task := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/B.md", ContractDigest: "task-b"}
	root := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/ROOT.md", ContractDigest: "root"}

	suspended := validPortableAttemptSeal(t, store, repo)
	suspended.SemanticTaskRef = task
	suspended.RootTaskRef = root
	suspended.AttemptID = "attempt-b1"
	suspended.ControllerGeneration = 1
	suspended.SealingTransitionID = "transition-suspend"
	suspended.RevokedLeaseID = "lease-b1"
	suspended.WorkspaceID = "workspace-b1"
	suspended.SourceProjectSnapshotID = "snapshot-b1"
	suspended.Disposition = "suspended-for-blocker"
	suspended.Coverage = "complete"
	suspendedRef, suspendedStored, err := store.StoreAttemptSeal(suspended)
	if err != nil {
		t.Fatal(err)
	}

	blocked := testTaskIndexRevision(task, 1, "transition-suspend", time.Unix(8000, 0).UTC())
	blocked.SemanticStatus = "blocked"
	blocked.AttemptSeals = []EvidenceObjectRef{suspendedRef}
	blockedRef, blockedStored, err := store.StoreTaskIndexRevision(blocked)
	if err != nil {
		t.Fatal(err)
	}
	if suspendedStored.Coverage != "complete" || suspendedStored.Disposition != "suspended-for-blocker" {
		t.Fatalf("suspended seal lifecycle/coverage changed: %#v", suspendedStored)
	}
	if blockedStored.SemanticStatus != "blocked" || blockedStored.TerminalRecord != nil {
		t.Fatalf("complete suspended AttemptSeal implied Task terminality: %#v", blockedStored)
	}

	accepted := validPortableAttemptSeal(t, store, repo)
	accepted.SemanticTaskRef = task
	accepted.RootTaskRef = root
	accepted.AttemptID = "attempt-b2"
	accepted.PredecessorAttemptID = suspendedStored.AttemptID
	accepted.ControllerGeneration = 2
	accepted.SealingTransitionID = "transition-accepted"
	accepted.RevokedLeaseID = "lease-b2"
	accepted.WorkspaceID = "workspace-b2"
	accepted.SourceProjectSnapshotID = "snapshot-b2"
	accepted.StartedAt = time.Unix(8010, 0).UTC()
	accepted.SealedAt = time.Unix(8011, 0).UTC()
	accepted.Disposition = "accepted"
	accepted.Coverage = "complete"
	acceptedRef, acceptedStored, err := store.StoreAttemptSeal(accepted)
	if err != nil {
		t.Fatal(err)
	}

	awaiting := testTaskIndexRevision(task, 2, "transition-accepted", time.Unix(8020, 0).UTC())
	awaiting.PreviousRevision = &blockedRef
	awaiting.SemanticStatus = "awaiting-publication"
	awaiting.AttemptSeals = []EvidenceObjectRef{suspendedRef, acceptedRef}
	awaitingRef, awaitingStored, err := store.StoreTaskIndexRevision(awaiting)
	if err != nil {
		t.Fatal(err)
	}
	if acceptedStored.Disposition != "accepted" || awaitingStored.SemanticStatus != "awaiting-publication" || len(awaitingStored.Finalizations) != 0 {
		t.Fatalf("accepted attempt was conflated with later Task finalization: seal=%#v task=%#v", acceptedStored, awaitingStored)
	}

	finalizationRef, _, err := store.StoreAttemptFinalization(AttemptFinalizationRecord{
		SchemaVersion:        evidenceSchemaVersion,
		AttemptSealRef:       acceptedRef,
		ControllerGeneration: 3,
		TransitionID:         "transition-integrated",
		ProjectSnapshotID:    "snapshot-integrated",
		Kind:                 "integrated",
		CreatedAt:            time.Unix(8030, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	terminalRecord := storeBundleSemanticObject(t, store, "task-terminal-record", "task-b:terminal")
	terminal := testTaskIndexRevision(task, 3, "transition-terminal", time.Unix(8040, 0).UTC())
	terminal.PreviousRevision = &awaitingRef
	terminal.SemanticStatus = "complete"
	terminal.AttemptSeals = []EvidenceObjectRef{suspendedRef, acceptedRef}
	terminal.Finalizations = []EvidenceObjectRef{finalizationRef}
	terminal.TerminalRecord = &terminalRecord
	terminalRef, terminalStored, err := store.StoreTaskIndexRevision(terminal)
	if err != nil {
		t.Fatal(err)
	}
	if terminalStored.SemanticStatus != "complete" || terminalStored.TerminalRecord == nil {
		t.Fatalf("terminal Task evidence is incomplete: %#v", terminalStored)
	}

	openEpisode, _, err := store.StoreEpisodeIndexRevision(EpisodeIndexRevision{
		SchemaVersion:             evidenceSchemaVersion,
		EpisodeID:                 "episode-b",
		RootTaskRef:               root,
		EpisodeRevision:           1,
		DependencyGraphSnapshotID: "dependency-graph-open",
		AdmittedClosureTaskRefs:   []SemanticTaskRef{task},
		TaskIndexHeads: []EvidenceSubjectHead{{
			SubjectID:   taskEvidenceSubjectID(task),
			RevisionRef: awaitingRef,
		}},
		AttemptSeals:         []EvidenceObjectRef{suspendedRef, acceptedRef},
		State:                "open",
		ControllerGeneration: 2,
		TransitionID:         "transition-accepted",
		CreatedAt:            time.Unix(8050, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	closeRecord := storeBundleSemanticObject(t, store, "episode-close-record", "episode-b:close")
	closedRef, closedStored, err := store.StoreEpisodeIndexRevision(EpisodeIndexRevision{
		SchemaVersion:             evidenceSchemaVersion,
		EpisodeID:                 "episode-b",
		RootTaskRef:               root,
		EpisodeRevision:           2,
		PreviousRevision:          &openEpisode,
		DependencyGraphSnapshotID: "dependency-graph-closed",
		AdmittedClosureTaskRefs:   []SemanticTaskRef{task},
		TaskIndexHeads: []EvidenceSubjectHead{{
			SubjectID:   taskEvidenceSubjectID(task),
			RevisionRef: terminalRef,
		}},
		AttemptSeals:         []EvidenceObjectRef{suspendedRef, acceptedRef},
		Finalizations:        []EvidenceObjectRef{finalizationRef},
		State:                "closed",
		CloseRecord:          &closeRecord,
		ControllerGeneration: 3,
		TransitionID:         "transition-terminal",
		CreatedAt:            time.Unix(8060, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if closedStored.State != "closed" || closedStored.CloseRecord == nil {
		t.Fatalf("closed Episode evidence is incomplete: %#v", closedStored)
	}
	if closedRef.Digest == "" || suspendedRef.Digest == acceptedRef.Digest {
		t.Fatal("lifecycle evidence identities were not kept distinct")
	}

	reloadedSuspended, err := store.LoadAttemptSeal(suspendedRef)
	if err != nil {
		t.Fatal(err)
	}
	if reloadedSuspended.AttemptSealID != suspendedStored.AttemptSealID || reloadedSuspended.Disposition != "suspended-for-blocker" {
		t.Fatal("later Task/Episode lifecycle evidence rewrote the immutable suspended AttemptSeal")
	}
}
