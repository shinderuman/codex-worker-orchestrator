package controller

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAttemptBundleStableAcrossLanePathReuseAndUnrelatedEvidence(t *testing.T) {
	store := newEvidenceTestStore(t)
	source, _ := newControllerLinkedWorktree(t)
	sealRef, seal := storeBundleAttemptSeal(t, store, source, "attempt-b1", "IMPLEMENTATION_TASKS/B.md", "episode-1", 1)
	taskRef := storeBundleTaskRevision(t, store, seal.SemanticTaskRef, 1, nil, []EvidenceObjectRef{sealRef}, nil)
	episodeRef := storeBundleEpisodeRevision(t, store, seal, taskRef, 1, 1, nil, []EvidenceObjectRef{sealRef}, nil)
	head1, ledger1 := publishBundleAuthority(t, store, 1, "snapshot-1", nil, nil,
		[]EvidenceSubjectHead{{SubjectID: taskEvidenceSubjectID(seal.SemanticTaskRef), RevisionRef: taskRef}},
		[]EvidenceSubjectHead{{SubjectID: seal.EpisodeID, RevisionRef: episodeRef}},
	)
	before, err := store.BuildAttemptEvidenceBundle(sealRef)
	if err != nil {
		t.Fatal(err)
	}
	if before.Kind != evidenceBundleAttempt || before.EvidenceGraphDigest == "" {
		t.Fatalf("attempt Bundle identity is incomplete: %#v", before)
	}

	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, source, "init", "-q")
	runControllerGit(t, source, "config", "user.email", "replacement@example.invalid")
	runControllerGit(t, source, "config", "user.name", "Replacement")
	if err := os.WriteFile(filepath.Join(source, "replacement.txt"), []byte("replacement lane\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, source, "add", ".")
	runControllerGit(t, source, "commit", "-q", "-m", "replacement")
	newSealRef, newSeal := storeBundleAttemptSeal(t, store, source, "attempt-c1", "IMPLEMENTATION_TASKS/C.md", "", 2)
	newTaskRef := storeBundleTaskRevision(t, store, newSeal.SemanticTaskRef, 2, nil, []EvidenceObjectRef{newSealRef}, nil)
	publishBundleAuthority(t, store, 2, "snapshot-2", &head1, &ledger1,
		[]EvidenceSubjectHead{
			{SubjectID: taskEvidenceSubjectID(seal.SemanticTaskRef), RevisionRef: taskRef},
			{SubjectID: taskEvidenceSubjectID(newSeal.SemanticTaskRef), RevisionRef: newTaskRef},
		},
		[]EvidenceSubjectHead{{SubjectID: seal.EpisodeID, RevisionRef: episodeRef}},
	)

	after, err := store.BuildAttemptEvidenceBundle(sealRef)
	if err != nil {
		t.Fatal(err)
	}
	if after.EvidenceGraphDigest != before.EvidenceGraphDigest {
		t.Fatalf("unrelated same-path evidence changed old attempt Bundle: before=%s after=%s", before.EvidenceGraphDigest, after.EvidenceGraphDigest)
	}
	if bundleContainsRef(after, newSealRef) {
		t.Fatal("old attempt Bundle contains replacement-lane evidence")
	}
}

func TestAttemptBundleIncludesLaterFinalizationWithoutSealRewrite(t *testing.T) {
	store := newEvidenceTestStore(t)
	source, _ := newControllerLinkedWorktree(t)
	sealRef, seal := storeBundleAttemptSeal(t, store, source, "attempt-b1", "IMPLEMENTATION_TASKS/B.md", "", 1)
	taskRef1 := storeBundleTaskRevision(t, store, seal.SemanticTaskRef, 1, nil, []EvidenceObjectRef{sealRef}, nil)
	head1, ledger1 := publishBundleAuthority(t, store, 1, "snapshot-1", nil, nil,
		[]EvidenceSubjectHead{{SubjectID: taskEvidenceSubjectID(seal.SemanticTaskRef), RevisionRef: taskRef1}}, nil,
	)
	before, err := store.BuildAttemptEvidenceBundle(sealRef)
	if err != nil {
		t.Fatal(err)
	}
	finalRef, _, err := store.StoreAttemptFinalization(AttemptFinalizationRecord{
		SchemaVersion: evidenceSchemaVersion, AttemptSealRef: sealRef, ControllerGeneration: 2,
		TransitionID: bundleTransitionID(2), ProjectSnapshotID: "snapshot-2", Kind: "integrated", CreatedAt: time.Unix(5200, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	taskRef2 := storeBundleTaskRevision(t, store, seal.SemanticTaskRef, 2, &taskRef1, nil, []EvidenceObjectRef{finalRef})
	publishBundleAuthority(t, store, 2, "snapshot-2", &head1, &ledger1,
		[]EvidenceSubjectHead{{SubjectID: taskEvidenceSubjectID(seal.SemanticTaskRef), RevisionRef: taskRef2}}, nil,
	)
	after, err := store.BuildAttemptEvidenceBundle(sealRef)
	if err != nil {
		t.Fatal(err)
	}
	if before.EvidenceGraphDigest == after.EvidenceGraphDigest {
		t.Fatal("later attempt finalization did not advance attempt Bundle graph")
	}
	if !bundleContainsRef(after, finalRef) {
		t.Fatal("later attempt finalization is absent from attempt Bundle")
	}
	loaded, err := store.LoadAttemptSeal(sealRef)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AttemptSealID != seal.AttemptSealID {
		t.Fatal("later finalization rewrote immutable AttemptSeal")
	}
}

func TestTaskAndEpisodeBundlesProjectHistoricalEvidence(t *testing.T) {
	store := newEvidenceTestStore(t)
	source, _ := newControllerLinkedWorktree(t)
	sealRef, seal := storeBundleAttemptSeal(t, store, source, "attempt-b1", "IMPLEMENTATION_TASKS/B.md", "episode-1", 1)
	taskRef := storeBundleTaskRevision(t, store, seal.SemanticTaskRef, 1, nil, []EvidenceObjectRef{sealRef}, nil)
	episodeRef := storeBundleEpisodeRevision(t, store, seal, taskRef, 1, 1, nil, []EvidenceObjectRef{sealRef}, nil)
	publishBundleAuthority(t, store, 1, "snapshot-1", nil, nil,
		[]EvidenceSubjectHead{{SubjectID: taskEvidenceSubjectID(seal.SemanticTaskRef), RevisionRef: taskRef}},
		[]EvidenceSubjectHead{{SubjectID: seal.EpisodeID, RevisionRef: episodeRef}},
	)
	taskBundle, err := store.BuildTaskEvidenceBundle(taskRef)
	if err != nil {
		t.Fatal(err)
	}
	episodeBundle, err := store.BuildEpisodeEvidenceBundle(episodeRef)
	if err != nil {
		t.Fatal(err)
	}
	if taskBundle.Kind != evidenceBundleTask || !bundleContainsRef(taskBundle, sealRef) {
		t.Fatalf("Task Bundle is incomplete: %#v", taskBundle)
	}
	if episodeBundle.Kind != evidenceBundleEpisode || !bundleContainsRef(episodeBundle, sealRef) || !bundleContainsRef(episodeBundle, taskRef) {
		t.Fatalf("Episode Bundle is incomplete: %#v", episodeBundle)
	}
}

func TestTaskAndEpisodeBundlesIncludeSemanticEvidenceLinks(t *testing.T) {
	store := newEvidenceTestStore(t)
	source, _ := newControllerLinkedWorktree(t)
	sealRef, seal := storeBundleAttemptSeal(t, store, source, "attempt-b1", "IMPLEMENTATION_TASKS/B.md", "episode-1", 1)
	findingRef := storeBundleSemanticObject(t, store, "finding-record", "finding:f1")
	dependencyRef := storeBundleSemanticObject(t, store, "dependency-edge", "dependency:d1")
	publicationRef := storeBundleSemanticObject(t, store, "publication-lineage", "publication:p1")
	transitionRef := storeBundleSemanticObject(t, store, "transition-record", "transition:t1")
	integrationRef := storeBundleSemanticObject(t, store, "integration-record", "integration:i1")

	taskRevision := testTaskIndexRevision(seal.SemanticTaskRef, 1, bundleTransitionID(1), time.Unix(5301, 0).UTC())
	taskRevision.AttemptSeals = []EvidenceObjectRef{sealRef}
	taskRevision.FindingRecords = []EvidenceObjectRef{findingRef}
	taskRevision.DependencyEdgeRecords = []EvidenceObjectRef{dependencyRef}
	taskRevision.PublicationLineageRecords = []EvidenceObjectRef{publicationRef}
	taskRef, _, err := store.StoreTaskIndexRevision(taskRevision)
	if err != nil {
		t.Fatal(err)
	}
	episodeRef, _, err := store.StoreEpisodeIndexRevision(EpisodeIndexRevision{
		SchemaVersion:             evidenceSchemaVersion,
		EpisodeID:                 seal.EpisodeID,
		RootTaskRef:               seal.RootTaskRef,
		EpisodeRevision:           1,
		DependencyGraphSnapshotID: "dependency-graph-1",
		AdmittedClosureTaskRefs:   []SemanticTaskRef{seal.SemanticTaskRef},
		TaskIndexHeads: []EvidenceSubjectHead{{
			SubjectID:   taskEvidenceSubjectID(seal.SemanticTaskRef),
			RevisionRef: taskRef,
		}},
		AttemptSeals:         []EvidenceObjectRef{sealRef},
		FindingRecords:       []EvidenceObjectRef{findingRef},
		TransitionRecords:    []EvidenceObjectRef{transitionRef},
		IntegrationHistory:   []EvidenceObjectRef{integrationRef},
		State:                "open",
		ControllerGeneration: 1,
		TransitionID:         bundleTransitionID(1),
		CreatedAt:            time.Unix(5401, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	publishBundleAuthority(t, store, 1, "snapshot-1", nil, nil,
		[]EvidenceSubjectHead{{SubjectID: taskEvidenceSubjectID(seal.SemanticTaskRef), RevisionRef: taskRef}},
		[]EvidenceSubjectHead{{SubjectID: seal.EpisodeID, RevisionRef: episodeRef}},
	)
	taskBundle, err := store.BuildTaskEvidenceBundle(taskRef)
	if err != nil {
		t.Fatal(err)
	}
	episodeBundle, err := store.BuildEpisodeEvidenceBundle(episodeRef)
	if err != nil {
		t.Fatal(err)
	}
	for name, ref := range map[string]EvidenceObjectRef{
		"finding":     findingRef,
		"dependency":  dependencyRef,
		"publication": publicationRef,
	} {
		if !bundleContainsRef(taskBundle, ref) {
			t.Fatalf("Task Bundle omitted %s semantic evidence", name)
		}
	}
	for name, ref := range map[string]EvidenceObjectRef{
		"finding":     findingRef,
		"transition":  transitionRef,
		"integration": integrationRef,
	} {
		if !bundleContainsRef(episodeBundle, ref) {
			t.Fatalf("Episode Bundle omitted %s semantic evidence", name)
		}
	}
}

func TestBundleFailsLoudlyWhenRequiredHistoricalEvidenceIsMissing(t *testing.T) {
	store := newEvidenceTestStore(t)
	source, _ := newControllerLinkedWorktree(t)
	sealRef, seal := storeBundleAttemptSeal(t, store, source, "attempt-b1", "IMPLEMENTATION_TASKS/B.md", "", 1)
	taskRef := storeBundleTaskRevision(t, store, seal.SemanticTaskRef, 1, nil, []EvidenceObjectRef{sealRef}, nil)
	publishBundleAuthority(t, store, 1, "snapshot-1", nil, nil,
		[]EvidenceSubjectHead{{SubjectID: taskEvidenceSubjectID(seal.SemanticTaskRef), RevisionRef: taskRef}}, nil,
	)
	if err := os.Remove(store.evidenceObjectPath(seal.GitObjectArchive.Digest)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BuildAttemptEvidenceBundle(sealRef); err == nil {
		t.Fatal("Bundle silently accepted missing required historical evidence")
	} else {
		var integrity *EvidenceIntegrityError
		if !errors.As(err, &integrity) {
			t.Fatalf("missing required evidence did not return typed integrity failure: %v", err)
		}
	}
}

func storeBundleAttemptSeal(t *testing.T, store *Store, source, attemptID, taskPath, episodeID string, generation uint64) (EvidenceObjectRef, AttemptSeal) {
	t.Helper()
	commit := controllerGitOutput(t, source, "rev-parse", "HEAD")
	tree := controllerGitOutput(t, source, "rev-parse", "HEAD^{tree}")
	archive, roots, err := store.CaptureGitObjectArchive(source, attemptID+":git", []string{commit, tree})
	if err != nil {
		t.Fatal(err)
	}
	required, err := store.PutEvidenceObject("validation", "application/json", attemptID+":validation", true, []byte(`{"status":"pass"}`))
	if err != nil {
		t.Fatal(err)
	}
	episodeRevision := uint64(0)
	if episodeID != "" {
		episodeRevision = 1
	}
	record := AttemptSeal{
		SchemaVersion: evidenceSchemaVersion, RepositoryIdentity: store.Identity().LineageID,
		SemanticTaskRef: SemanticTaskRef{TaskPath: taskPath, ContractDigest: attemptID + "-contract"},
		RootTaskRef:     SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/ROOT.md", ContractDigest: "root-contract"},
		AttemptID:       attemptID, EpisodeID: episodeID, EpisodeRevision: episodeRevision, ControllerGeneration: generation,
		SealingTransitionID: bundleTransitionID(generation), RevokedLeaseID: "lease-" + attemptID,
		WorkspaceID: "workspace-" + attemptID, ExecutionPurpose: "blocker-execution", SourceProjectSnapshotID: fmt.Sprintf("snapshot-source-%d", generation),
		StartedAt: time.Unix(5000+int64(generation), 0).UTC(), SealedAt: time.Unix(5100+int64(generation), 0).UTC(),
		Disposition: "suspended-for-blocker", ExecutionBaseOID: commit,
		BaselineIndexTree: tree, BaselineWorktreeTree: tree,
		CurrentIndexTree: tree, CurrentWorktreeTree: tree,
		ParentAuthorityDigest: "parent-authority", GitObjectArchive: archive, GitObjectArchiveRoots: roots,
		EvidenceRefs: []EvidenceObjectRef{required}, Coverage: "complete", RequiredKinds: []string{"git-object-archive", "validation"},
	}
	ref, stored, err := store.StoreAttemptSeal(record)
	if err != nil {
		t.Fatal(err)
	}
	return ref, stored
}

func storeBundleTaskRevision(t *testing.T, store *Store, task SemanticTaskRef, generation uint64, previous *EvidenceObjectRef, seals, finalizations []EvidenceObjectRef) EvidenceObjectRef {
	t.Helper()
	revision := testTaskIndexRevision(task, generation, bundleTransitionID(generation), time.Unix(5300+int64(generation), 0).UTC())
	revision.PreviousRevision = previous
	revision.AttemptSeals = seals
	revision.Finalizations = finalizations
	ref, _, err := store.StoreTaskIndexRevision(revision)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func storeBundleEpisodeRevision(t *testing.T, store *Store, seal AttemptSeal, taskRef EvidenceObjectRef, episodeRevision, generation uint64, previous *EvidenceObjectRef, seals, finalizations []EvidenceObjectRef) EvidenceObjectRef {
	t.Helper()
	ref, _, err := store.StoreEpisodeIndexRevision(EpisodeIndexRevision{
		SchemaVersion:             evidenceSchemaVersion,
		EpisodeID:                 seal.EpisodeID,
		RootTaskRef:               seal.RootTaskRef,
		EpisodeRevision:           episodeRevision,
		PreviousRevision:          previous,
		DependencyGraphSnapshotID: fmt.Sprintf("dependency-graph-%d", generation),
		AdmittedClosureTaskRefs:   []SemanticTaskRef{seal.SemanticTaskRef},
		TaskIndexHeads: []EvidenceSubjectHead{{
			SubjectID:   taskEvidenceSubjectID(seal.SemanticTaskRef),
			RevisionRef: taskRef,
		}},
		AttemptSeals:         seals,
		Finalizations:        finalizations,
		State:                "open",
		ControllerGeneration: generation,
		TransitionID:         bundleTransitionID(generation),
		CreatedAt:            time.Unix(5400+int64(generation), 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func storeBundleSemanticObject(t *testing.T, store *Store, kind, logicalIdentity string) EvidenceObjectRef {
	t.Helper()
	ref, err := store.PutEvidenceObject(kind, "application/json", logicalIdentity, true, []byte(`{"id":"evidence"}`))
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func publishBundleAuthority(
	t *testing.T,
	store *Store,
	generation uint64,
	snapshot string,
	previousHead, previousLedger *EvidenceObjectRef,
	taskHeads, episodeHeads []EvidenceSubjectHead,
) (EvidenceObjectRef, EvidenceObjectRef) {
	t.Helper()
	previous := EvidenceHead{}
	if previousHead != nil {
		loaded, err := store.LoadEvidenceHead(*previousHead)
		if err != nil {
			t.Fatal(err)
		}
		previous = loaded
	}
	changedTaskHeads, err := changedEvidenceHeads(previous.TaskHeads, taskHeads)
	if err != nil {
		t.Fatal(err)
	}
	changedEpisodeHeads, err := changedEvidenceHeads(previous.EpisodeHeads, episodeHeads)
	if err != nil {
		t.Fatal(err)
	}
	headRef, _, err := store.StoreEvidenceHead(EvidenceHead{
		SchemaVersion: evidenceSchemaVersion, RepositoryIdentity: store.Identity().LineageID,
		PreviousHead: previousHead, TaskHeads: taskHeads, EpisodeHeads: episodeHeads,
		ControllerGeneration: generation, ProjectSnapshotID: snapshot, CreatedAt: time.Unix(5500+int64(generation), 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ledgerRef, _, err := store.StoreEvidenceLedgerRecord(EvidenceLedgerRecord{
		SchemaVersion: evidenceSchemaVersion, Sequence: generation, PreviousRecord: previousLedger,
		RepositoryIdentity: store.Identity().LineageID, ControllerGeneration: generation,
		TransitionID: bundleTransitionID(generation), ChangedTaskIndexHeads: changedTaskHeads,
		ChangedEpisodeIndexHeads: changedEpisodeHeads, ProjectSnapshotID: snapshot,
		EvidenceHeadRef: headRef, CreatedAt: time.Unix(5600+int64(generation), 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	next := current
	next.ControllerGeneration = generation
	next.ProjectSnapshotID = snapshot
	next.EvidenceHeadRef = evidenceRefPointer(headRef)
	next.EvidenceLedgerHeadRef = evidenceRefPointer(ledgerRef)
	next.EvidenceLedgerSequence = generation
	if err := store.writeHeadCAS(current.ControllerGeneration, next); err != nil {
		t.Fatal(err)
	}
	return headRef, ledgerRef
}

func bundleTransitionID(generation uint64) string {
	return fmt.Sprintf("transition-bundle-%d", generation)
}

func bundleContainsRef(bundle EvidenceBundleProjection, target EvidenceObjectRef) bool {
	for _, object := range bundle.Objects {
		if evidenceRefsEqual(object.Ref, target) {
			return true
		}
	}
	return false
}
