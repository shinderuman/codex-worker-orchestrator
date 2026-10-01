package controller

import (
	"testing"
	"time"
)

func TestCleanupDurabilityProofRequiresTaskAndEpisodeReachability(t *testing.T) {
	store, sealRef, taskRef, episodeRef := newCleanupProofFixture(t, true)
	proof, err := store.ProveCleanupDurability(sealRef)
	if err != nil {
		t.Fatal(err)
	}
	if !evidenceRefsEqual(proof.AttemptSealRef, sealRef) || !evidenceRefsEqual(proof.TaskRevisionRef, taskRef) {
		t.Fatalf("cleanup proof did not bind exact task evidence: %#v", proof)
	}
	if proof.EpisodeRevisionRef == nil || !evidenceRefsEqual(*proof.EpisodeRevisionRef, episodeRef) {
		t.Fatalf("cleanup proof did not bind exact episode evidence: %#v", proof)
	}
	if proof.EvidenceGraphDigest == "" || proof.ControllerGeneration != 1 || proof.ProjectSnapshotID != "snapshot-cleanup" {
		t.Fatalf("cleanup proof did not bind current evidence authority: %#v", proof)
	}
}

func TestCleanupDurabilityProofRejectsEpisodeSealMissingEpisodeIndex(t *testing.T) {
	store, sealRef, _, _ := newCleanupProofFixture(t, false)
	if _, err := store.ProveCleanupDurability(sealRef); err == nil {
		t.Fatal("episode attempt seal without episode-index reachability was accepted")
	}
}

func TestCleanupDurabilityProofRejectsUnindexedSeal(t *testing.T) {
	store, repo := newEvidenceTestStoreWithRepo(t)
	sealRef, _ := storeCleanupProofSeal(t, store, repo, "")
	task := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/OTHER.md", ContractDigest: "other"}
	revision := testTaskIndexRevision(task, 1, "transition-cleanup", time.Unix(4200, 0).UTC())
	taskRef, _, err := store.StoreTaskIndexRevision(revision)
	if err != nil {
		t.Fatal(err)
	}
	publishCleanupProofAuthority(t, store, []EvidenceSubjectHead{{SubjectID: taskEvidenceSubjectID(task), RevisionRef: taskRef}}, nil)
	if _, err := store.ProveCleanupDurability(sealRef); err == nil {
		t.Fatal("attempt seal outside current task-index authority was accepted")
	}
}

func newCleanupProofFixture(t *testing.T, publishEpisode bool) (*Store, EvidenceObjectRef, EvidenceObjectRef, EvidenceObjectRef) {
	t.Helper()
	store, repo := newEvidenceTestStoreWithRepo(t)
	sealRef, seal := storeCleanupProofSeal(t, store, repo, "episode-cleanup")
	taskRevision := testTaskIndexRevision(seal.SemanticTaskRef, 1, "transition-cleanup", time.Unix(4100, 0).UTC())
	taskRevision.AttemptSeals = []EvidenceObjectRef{sealRef}
	taskRevision.SemanticStatus = "blocked"
	taskRef, _, err := store.StoreTaskIndexRevision(taskRevision)
	if err != nil {
		t.Fatal(err)
	}
	var episodeRef EvidenceObjectRef
	var episodeHeads []EvidenceSubjectHead
	if publishEpisode {
		episodeRef, _, err = store.StoreEpisodeIndexRevision(EpisodeIndexRevision{
			SchemaVersion:             evidenceSchemaVersion,
			EpisodeID:                 seal.EpisodeID,
			RootTaskRef:               seal.RootTaskRef,
			EpisodeRevision:           seal.EpisodeRevision,
			DependencyGraphSnapshotID: "dependency-snapshot-cleanup",
			AdmittedClosureTaskRefs:   []SemanticTaskRef{seal.SemanticTaskRef},
			TaskIndexHeads: []EvidenceSubjectHead{{
				SubjectID:   taskEvidenceSubjectID(seal.SemanticTaskRef),
				RevisionRef: taskRef,
			}},
			AttemptSeals:         []EvidenceObjectRef{sealRef},
			State:                "open",
			ControllerGeneration: 1,
			TransitionID:         "transition-cleanup",
			CreatedAt:            time.Unix(4101, 0).UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
		episodeHeads = []EvidenceSubjectHead{{SubjectID: seal.EpisodeID, RevisionRef: episodeRef}}
	}
	publishCleanupProofAuthority(t, store, []EvidenceSubjectHead{{SubjectID: taskEvidenceSubjectID(seal.SemanticTaskRef), RevisionRef: taskRef}}, episodeHeads)
	return store, sealRef, taskRef, episodeRef
}

func storeCleanupProofSeal(t *testing.T, store *Store, repo, episodeID string) (EvidenceObjectRef, AttemptSeal) {
	t.Helper()
	commit := controllerGitOutput(t, repo, "rev-parse", "HEAD")
	tree := controllerGitOutput(t, repo, "rev-parse", "HEAD^{tree}")
	archive, roots, err := store.CaptureGitObjectArchive(repo, "cleanup-attempt:git", []string{commit, tree})
	if err != nil {
		t.Fatal(err)
	}
	episodeRevision := uint64(0)
	if episodeID != "" {
		episodeRevision = 3
	}
	record := AttemptSeal{
		SchemaVersion: evidenceSchemaVersion, RepositoryIdentity: store.Identity().LineageID,
		SemanticTaskRef: SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/A.md", ContractDigest: "task-a"}, RootTaskRef: SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/ROOT.md", ContractDigest: "root"},
		AttemptID: "cleanup-attempt", EpisodeID: episodeID, EpisodeRevision: episodeRevision, ControllerGeneration: 1,
		SealingTransitionID: "transition-cleanup", RevokedLeaseID: "lease-cleanup", WorkspaceID: "workspace-cleanup", ExecutionPurpose: "blocker-execution", SourceProjectSnapshotID: "snapshot-cleanup-source",
		StartedAt: time.Unix(4000, 0).UTC(), SealedAt: time.Unix(4001, 0).UTC(), Disposition: "suspended-for-blocker",
		ExecutionBaseOID: commit, BaselineIndexTree: tree, BaselineWorktreeTree: tree, CurrentIndexTree: tree, CurrentWorktreeTree: tree,
		ParentAuthorityDigest: "parent-authority", GitObjectArchive: archive, GitObjectArchiveRoots: roots, Coverage: "complete", RequiredKinds: []string{"git-object-archive"},
	}
	ref, stored, err := store.StoreAttemptSeal(record)
	if err != nil {
		t.Fatal(err)
	}
	return ref, stored
}

func publishCleanupProofAuthority(t *testing.T, store *Store, taskHeads, episodeHeads []EvidenceSubjectHead) {
	t.Helper()
	headRef, _, err := store.StoreEvidenceHead(EvidenceHead{
		SchemaVersion: evidenceSchemaVersion, RepositoryIdentity: store.Identity().LineageID,
		TaskHeads: taskHeads, EpisodeHeads: episodeHeads, ControllerGeneration: 1, ProjectSnapshotID: "snapshot-cleanup", CreatedAt: time.Unix(4300, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ledgerRef, _, err := store.StoreEvidenceLedgerRecord(EvidenceLedgerRecord{
		SchemaVersion: evidenceSchemaVersion, Sequence: 1, RepositoryIdentity: store.Identity().LineageID,
		ControllerGeneration: 1, TransitionID: "transition-cleanup", ProjectSnapshotID: "snapshot-cleanup", EvidenceHeadRef: headRef, CreatedAt: time.Unix(4301, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	next := current
	next.ControllerGeneration = 1
	next.ProjectSnapshotID = "snapshot-cleanup"
	next.EvidenceHeadRef = evidenceRefPointer(headRef)
	next.EvidenceLedgerHeadRef = evidenceRefPointer(ledgerRef)
	next.EvidenceLedgerSequence = 1
	if err := store.writeHeadCAS(current.ControllerGeneration, next); err != nil {
		t.Fatal(err)
	}
}
