package controller

import (
	"os"
	"testing"
)

func TestIntegratedLifecycleAllBundleKindsSurviveLaneDeletionGCAndPathReuse(t *testing.T) {
	t.Parallel()
	harness := newLifecycleHarness(t)
	root := harness.edit(t, harness.source, "root-owned.txt", "root work\n")
	episode := harness.planBlockingEpisode(t, root, harness.blocker, "bundle-gc-blocker")
	suspended, err := harness.store.SuspendExecution(root, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}

	blockerLane, err := harness.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: suspended.Head.ControllerGeneration,
		EpisodeID:          episode.EpisodeID,
		EpisodeRevision:    episode.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.source = *blockerLane.Admission
	blocker := harness.edit(t, harness.source, "blocker-result.txt", "blocker result\n")
	locallyRetired, candidate := harness.retireLifecycleTask(t, blocker, "blocker result\n")
	published := harness.publishRetiredLifecycleTask(t, locallyRetired, candidate)
	cleaned, err := harness.store.CleanupExecution(CleanupExecutionInput{
		ExpectedGeneration: published.Head.ControllerGeneration,
		WorkspaceID:        blocker.Workspace.ID,
		SealRef:            candidate.SealRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(blocker.Workspace.Root); !os.IsNotExist(err) {
		t.Fatalf("historical blocker lane survived deletion: %v", err)
	}

	resumed, err := harness.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: cleaned.Head.ControllerGeneration,
		EpisodeID:          episode.EpisodeID,
		EpisodeRevision:    published.Head.ActiveEpisodeRevision,
		SuspensionID:       suspended.Suspension.SnapshotID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Admission == nil {
		t.Fatal("root resume did not materialize canonical root authority")
	}
	if resumed.Admission.Workspace.Root != blocker.Workspace.Root || resumed.Admission.Workspace.ID == blocker.Workspace.ID {
		t.Fatalf("same-path recreation did not mint a fresh workspace identity: old=%#v new=%#v", blocker.Workspace, resumed.Admission.Workspace)
	}

	taskRef, episodeRef := latestTaskAndEpisodeEvidenceRefs(t, harness.store, harness.blocker.TaskPath, episode.EpisodeID)
	attemptRefs := []EvidenceObjectRef{*suspended.SealRef, candidate.SealRef}

	attemptDigests := make(map[string]string, len(attemptRefs))
	for _, ref := range attemptRefs {
		bundle, err := harness.store.BuildAttemptEvidenceBundle(ref)
		if err != nil {
			t.Fatal(err)
		}
		attemptDigests[ref.Digest] = bundle.EvidenceGraphDigest
	}
	taskBundle, err := harness.store.BuildTaskEvidenceBundle(taskRef)
	if err != nil {
		t.Fatal(err)
	}
	episodeBundle, err := harness.store.BuildEpisodeEvidenceBundle(episodeRef)
	if err != nil {
		t.Fatal(err)
	}

	runControllerGit(t, harness.repo, "reflog", "expire", "--expire=now", "--all")
	runControllerGit(t, harness.repo, "gc", "--aggressive", "--prune=now")

	for _, ref := range attemptRefs {
		bundle, err := harness.store.BuildAttemptEvidenceBundle(ref)
		if err != nil {
			t.Fatalf("attempt Bundle %s was not reconstructible after deletion/GC/path reuse: %v", ref.LogicalIdentity, err)
		}
		if bundle.EvidenceGraphDigest != attemptDigests[ref.Digest] {
			t.Fatalf("attempt Bundle digest changed after deletion/GC/path reuse: %s", ref.LogicalIdentity)
		}
	}
	postTaskBundle, err := harness.store.BuildTaskEvidenceBundle(taskRef)
	if err != nil {
		t.Fatalf("Task Bundle was not reconstructible after deletion/GC/path reuse: %v", err)
	}
	if postTaskBundle.EvidenceGraphDigest != taskBundle.EvidenceGraphDigest {
		t.Fatal("Task Bundle digest changed after deletion/GC/path reuse")
	}
	postEpisodeBundle, err := harness.store.BuildEpisodeEvidenceBundle(episodeRef)
	if err != nil {
		t.Fatalf("Episode Bundle was not reconstructible after deletion/GC/path reuse: %v", err)
	}
	if postEpisodeBundle.EvidenceGraphDigest != episodeBundle.EvidenceGraphDigest {
		t.Fatal("Episode Bundle digest changed after deletion/GC/path reuse")
	}
}

func latestTaskAndEpisodeEvidenceRefs(t *testing.T, store *Store, taskPath, episodeID string) (EvidenceObjectRef, EvidenceObjectRef) {
	t.Helper()
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.EvidenceHeadRef == nil {
		t.Fatal("controller has no evidence head")
	}
	evidenceHead, err := store.LoadEvidenceHead(*head.EvidenceHeadRef)
	if err != nil {
		t.Fatal(err)
	}

	var taskRef EvidenceObjectRef
	for _, subject := range evidenceHead.TaskHeads {
		revision, err := store.LoadTaskIndexRevision(subject.RevisionRef)
		if err != nil {
			t.Fatal(err)
		}
		if revision.TaskRef.TaskPath == taskPath {
			taskRef = subject.RevisionRef
			break
		}
	}
	if taskRef.Digest == "" {
		t.Fatalf("task evidence head for %s not found", taskPath)
	}

	var episodeRef EvidenceObjectRef
	for _, subject := range evidenceHead.EpisodeHeads {
		if subject.SubjectID == episodeID {
			episodeRef = subject.RevisionRef
			break
		}
	}
	if episodeRef.Digest == "" {
		t.Fatalf("episode evidence head %s not found", episodeID)
	}
	return taskRef, episodeRef
}
