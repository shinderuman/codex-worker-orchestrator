package controller

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type gcAcceptanceBundles struct {
	attempt EvidenceBundleProjection
	task    EvidenceBundleProjection
	episode EvidenceBundleProjection
}

func TestHistoricalBundlesSurviveOperationalSnapshotGCAndSamePathReuse(t *testing.T) {
	repo, lane := newControllerLinkedWorktree(t)
	store := openGCAcceptanceEvidenceStore(t, repo)
	writeGCAcceptanceFile(t, filepath.Join(lane, "lane-only.txt"), "lane B1\n")
	runControllerGit(t, lane, "add", "lane-only.txt")
	runControllerGit(t, lane, "commit", "-q", "-m", "lane B1")
	oldCommit := controllerGitOutput(t, lane, "rev-parse", "HEAD")
	oldGitDir := controllerGitOutput(t, lane, "rev-parse", "--absolute-git-dir")
	operationalRef := "refs/glm-worker/suspensions/snapshot-b1"
	runControllerGit(t, repo, "update-ref", operationalRef, oldCommit)

	rootTask := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/ROOT.md", ContractDigest: "root-contract"}
	oldTask := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/B.md", ContractDigest: "attempt-b1-contract"}
	sealRef, seal := storeGCAcceptanceAttemptSeal(t, store, lane, oldTask, rootTask, "episode-1", "attempt-b1", 1, "snapshot-b1")
	taskRef := storeBundleTaskRevision(t, store, seal.SemanticTaskRef, 1, nil, []EvidenceObjectRef{sealRef}, nil)
	episodeRef := storeBundleEpisodeRevision(t, store, seal, taskRef, 1, 1, nil, []EvidenceObjectRef{sealRef}, nil)
	head1, ledger1 := publishBundleAuthority(t, store, 1, "snapshot-1", nil, nil,
		[]EvidenceSubjectHead{{SubjectID: taskEvidenceSubjectID(seal.SemanticTaskRef), RevisionRef: taskRef}},
		[]EvidenceSubjectHead{{SubjectID: seal.EpisodeID, RevisionRef: episodeRef}},
	)
	beforeCleanup := buildGCAcceptanceBundles(t, store, sealRef, taskRef, episodeRef)

	runControllerGit(t, repo, "worktree", "remove", "--force", lane)
	requireGCAcceptancePathRemoved(t, lane)
	requireGCAcceptancePathRemoved(t, oldGitDir)
	runControllerGit(t, repo, "update-ref", "-d", operationalRef)
	runControllerGit(t, repo, "reflog", "expire", "--expire=now", "--expire-unreachable=now", "--all")
	runControllerGit(t, repo, "gc", "--aggressive", "--prune=now")
	if gcAcceptanceGitObjectExists(repo, oldCommit) {
		t.Fatalf("old lane-only commit remained reachable after operational ref removal and aggressive GC: %s", oldCommit)
	}

	withoutLane := buildGCAcceptanceBundles(t, store, sealRef, taskRef, episodeRef)
	requireGCAcceptanceBundleDigestsEqual(t, beforeCleanup, withoutLane)

	runControllerGit(t, repo, "worktree", "add", "-q", "--detach", lane, "HEAD")
	writeGCAcceptanceFile(t, filepath.Join(lane, "replacement.txt"), "lane C1\n")
	runControllerGit(t, lane, "add", "replacement.txt")
	runControllerGit(t, lane, "commit", "-q", "-m", "lane C1")
	newCommit := controllerGitOutput(t, lane, "rev-parse", "HEAD")
	newTask := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/C.md", ContractDigest: "attempt-c1-contract"}
	newSealRef, newSeal := storeGCAcceptanceAttemptSeal(t, store, lane, newTask, rootTask, "", "attempt-c1", 2, "")
	requireGCAcceptanceFreshAttemptAuthority(t, seal, newSeal, oldCommit, newCommit)
	newTaskRef := storeBundleTaskRevision(t, store, newSeal.SemanticTaskRef, 2, nil, []EvidenceObjectRef{newSealRef}, nil)
	publishBundleAuthority(t, store, 2, "snapshot-2", &head1, &ledger1,
		[]EvidenceSubjectHead{
			{SubjectID: taskEvidenceSubjectID(seal.SemanticTaskRef), RevisionRef: taskRef},
			{SubjectID: taskEvidenceSubjectID(newSeal.SemanticTaskRef), RevisionRef: newTaskRef},
		},
		[]EvidenceSubjectHead{{SubjectID: seal.EpisodeID, RevisionRef: episodeRef}},
	)

	afterReuse := buildGCAcceptanceBundles(t, store, sealRef, taskRef, episodeRef)
	requireGCAcceptanceBundleDigestsEqual(t, withoutLane, afterReuse)
	requireGCAcceptanceBundlesExclude(t, afterReuse, newSealRef, newTaskRef)

	if len(seal.SessionAssociationRefs) == 0 {
		t.Fatal("acceptance seal has no required session evidence to fault-inject")
	}
	if err := os.Remove(store.evidenceObjectPath(seal.SessionAssociationRefs[0].Digest)); err != nil {
		t.Fatal(err)
	}
	requireGCAcceptanceBundlesFailIntegrity(t, store, sealRef, taskRef, episodeRef)
}

func openGCAcceptanceEvidenceStore(t *testing.T, repo string) *Store {
	t.Helper()
	store, err := Open(controllerTestConfig(repo, filepath.Join(t.TempDir(), "state", "sessions")))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func storeGCAcceptanceAttemptSeal(
	t *testing.T,
	store *Store,
	source string,
	task SemanticTaskRef,
	rootTask SemanticTaskRef,
	episodeID string,
	attemptID string,
	generation uint64,
	operationalSnapshotID string,
) (EvidenceObjectRef, AttemptSeal) {
	t.Helper()
	record := validPortableAttemptSeal(t, store, source)
	record.SemanticTaskRef = task
	record.RootTaskRef = rootTask
	record.AttemptID = attemptID
	record.EpisodeID = episodeID
	record.EpisodeRevision = 0
	if episodeID != "" {
		record.EpisodeRevision = generation
	}
	record.ControllerGeneration = generation
	record.SealingTransitionID = bundleTransitionID(generation)
	record.RevokedLeaseID = "lease-" + attemptID
	record.WorkspaceID = "workspace-" + attemptID
	record.ExecutionPurpose = "blocker-work"
	record.SourceProjectSnapshotID = "snapshot-source-" + attemptID
	record.OperationalSnapshotID = operationalSnapshotID
	record.Disposition = "suspended-for-blocker"
	record.EvidenceRefs = []EvidenceObjectRef{storeBundleSemanticObject(t, store, "telemetry", attemptID+":telemetry")}
	record.FindingRecordRefs = []EvidenceObjectRef{storeBundleSemanticObject(t, store, "finding-record", attemptID+":finding")}
	record.ReviewValidationRefs = []EvidenceObjectRef{storeBundleSemanticObject(t, store, "validation", attemptID+":validation")}
	record.SessionAssociationRefs = []EvidenceObjectRef{storeBundleSemanticObject(t, store, "session-association", attemptID+":session")}
	ref, stored, err := store.StoreAttemptSeal(record)
	if err != nil {
		t.Fatal(err)
	}
	return ref, stored
}

func buildGCAcceptanceBundles(
	t *testing.T,
	store *Store,
	sealRef EvidenceObjectRef,
	taskRef EvidenceObjectRef,
	episodeRef EvidenceObjectRef,
) gcAcceptanceBundles {
	t.Helper()
	attempt, err := store.BuildAttemptEvidenceBundle(sealRef)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.BuildTaskEvidenceBundle(taskRef)
	if err != nil {
		t.Fatal(err)
	}
	episode, err := store.BuildEpisodeEvidenceBundle(episodeRef)
	if err != nil {
		t.Fatal(err)
	}
	return gcAcceptanceBundles{attempt: attempt, task: task, episode: episode}
}

func requireGCAcceptanceBundleDigestsEqual(t *testing.T, want, got gcAcceptanceBundles) {
	t.Helper()
	if want.attempt.EvidenceGraphDigest != got.attempt.EvidenceGraphDigest {
		t.Fatalf("historical Attempt Bundle digest changed: want=%s got=%s", want.attempt.EvidenceGraphDigest, got.attempt.EvidenceGraphDigest)
	}
	if want.task.EvidenceGraphDigest != got.task.EvidenceGraphDigest {
		t.Fatalf("historical Task Bundle digest changed: want=%s got=%s", want.task.EvidenceGraphDigest, got.task.EvidenceGraphDigest)
	}
	if want.episode.EvidenceGraphDigest != got.episode.EvidenceGraphDigest {
		t.Fatalf("historical Episode Bundle digest changed: want=%s got=%s", want.episode.EvidenceGraphDigest, got.episode.EvidenceGraphDigest)
	}
}

func requireGCAcceptanceBundlesExclude(t *testing.T, bundles gcAcceptanceBundles, refs ...EvidenceObjectRef) {
	t.Helper()
	for _, ref := range refs {
		if bundleContainsRef(bundles.attempt, ref) || bundleContainsRef(bundles.task, ref) || bundleContainsRef(bundles.episode, ref) {
			t.Fatalf("historical Bundle absorbed replacement-lane evidence: %#v", ref)
		}
	}
}

func requireGCAcceptanceFreshAttemptAuthority(t *testing.T, old, fresh AttemptSeal, oldCommit, newCommit string) {
	t.Helper()
	if old.AttemptID == fresh.AttemptID || old.WorkspaceID == fresh.WorkspaceID || old.RevokedLeaseID == fresh.RevokedLeaseID || old.ControllerGeneration == fresh.ControllerGeneration {
		t.Fatalf("same-path replacement reused old attempt authority: old=%#v fresh=%#v", old, fresh)
	}
	if oldCommit == newCommit || fresh.ExecutionBaseOID != newCommit {
		t.Fatalf("same-path replacement did not bind fresh Git authority: old=%s new=%s seal=%s", oldCommit, newCommit, fresh.ExecutionBaseOID)
	}
}

func requireGCAcceptanceBundlesFailIntegrity(
	t *testing.T,
	store *Store,
	sealRef EvidenceObjectRef,
	taskRef EvidenceObjectRef,
	episodeRef EvidenceObjectRef,
) {
	t.Helper()
	builders := map[string]func() (EvidenceBundleProjection, error){
		"attempt": func() (EvidenceBundleProjection, error) { return store.BuildAttemptEvidenceBundle(sealRef) },
		"task":    func() (EvidenceBundleProjection, error) { return store.BuildTaskEvidenceBundle(taskRef) },
		"episode": func() (EvidenceBundleProjection, error) { return store.BuildEpisodeEvidenceBundle(episodeRef) },
	}
	for name, build := range builders {
		if _, err := build(); err == nil {
			t.Fatalf("%s Bundle silently accepted missing required historical evidence", name)
		} else {
			var integrity *EvidenceIntegrityError
			if !errors.As(err, &integrity) {
				t.Fatalf("%s Bundle returned non-integrity failure for missing historical evidence: %v", name, err)
			}
		}
	}
}

func requireGCAcceptancePathRemoved(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected removed path %s, stat error=%v", path, err)
	}
}

func writeGCAcceptanceFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gcAcceptanceGitObjectExists(repo, object string) bool {
	command := exec.Command("git", "-C", repo, "cat-file", "-e", object+"^{object}")
	return command.Run() == nil
}
