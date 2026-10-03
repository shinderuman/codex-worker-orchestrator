package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIntegratedLifecycle606F11Replay(t *testing.T) {
	t.Parallel()
	harness := newLifecycleHarness(t)
	task606 := harness.edit(t, harness.source, "606-production.txt", "unfinished 606 production work\n")
	episode := harness.planBlockingEpisode(t, task606, harness.blocker, "606-f11")

	suspended606, err := harness.store.SuspendExecution(task606, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.store.AdmitMutation(task606.MutationAuthority(), task606.Workspace, task606.Snapshot); err == nil {
		t.Fatal("suspended 606 lease regained mutation authority")
	}

	f11Lane, err := harness.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: suspended606.Head.ControllerGeneration,
		EpisodeID:          episode.EpisodeID,
		EpisodeRevision:    episode.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if f11Lane.Admission == nil || !f11Lane.Admission.Attempt.SemanticTaskRef.Equal(harness.blocker) {
		t.Fatalf("F11 authority mismatch: %#v", f11Lane.Admission)
	}
	harness.source = *f11Lane.Admission
	harness.assertNoSecondMutatingAuthority(t, "F11 live")

	f11 := harness.edit(t, harness.source, "f11-result.txt", "F11 blocker result\n")
	publishedF11, candidateF11 := harness.publishLifecycleTask(t, f11, "F11 blocker result\n")
	if publishedF11.Head.IntegrationTip != candidateF11.CommitOID || publishedF11.Head.ObservedPrefix != candidateF11.CommitOID {
		t.Fatalf("F11 was not canonically published before 606 resume: head=%#v candidate=%s", publishedF11.Head, candidateF11.CommitOID)
	}
	retiredF11 := harness.retireLifecycleTask(t, publishedF11, candidateF11)
	cleanedF11, err := harness.store.CleanupExecution(CleanupExecutionInput{
		ExpectedGeneration: retiredF11.Head.ControllerGeneration,
		WorkspaceID:        f11.Workspace.ID,
		SealRef:            candidateF11.SealRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(f11.Workspace.Root); !os.IsNotExist(err) {
		t.Fatalf("F11 lane survived cleanup: %v", err)
	}

	resumed606, err := harness.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: cleanedF11.Head.ControllerGeneration,
		EpisodeID:          episode.EpisodeID,
		EpisodeRevision:    retiredF11.Head.ActiveEpisodeRevision,
		SuspensionID:       suspended606.Suspension.SnapshotID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumed606.Admission == nil || retiredF11.Head.RootTaskRef == nil || !resumed606.Admission.Attempt.SemanticTaskRef.Equal(*retiredF11.Head.RootTaskRef) {
		t.Fatalf("606 resume authority mismatch: %#v", resumed606.Admission)
	}
	if resumed606.Admission.Attempt.PredecessorAttemptID != task606.Attempt.AttemptID ||
		resumed606.Admission.Attempt.ResumedFromSealID != suspended606.SealRef.LogicalIdentity {
		t.Fatalf("606 resume lost exact preserved lineage: %#v", resumed606.Admission.Attempt)
	}
	if resumed606.Admission.Attempt.ExecutionBaseOID != cleanedF11.Head.IntegrationTip {
		t.Fatalf("606 resumed from %s instead of canonical post-F11 integration tip %s", resumed606.Admission.Attempt.ExecutionBaseOID, cleanedF11.Head.IntegrationTip)
	}
	if resumed606.Admission.Lease.LeaseID == task606.Lease.LeaseID {
		t.Fatal("606 resume reused the revoked pre-blocker lease")
	}
	if resumed606.Admission.Workspace.Root != f11.Workspace.Root || resumed606.Admission.Workspace.ID == f11.Workspace.ID {
		t.Fatalf("606 resume did not recycle the one lane with fresh identity: old=%#v new=%#v", f11.Workspace, resumed606.Admission.Workspace)
	}

	preserved, err := os.ReadFile(filepath.Join(resumed606.Admission.Workspace.Root, "606-production.txt"))
	if err != nil || string(preserved) != "unfinished 606 production work\n" {
		t.Fatalf("606 production work was not preserved across F11: %q %v", preserved, err)
	}
	integrated, err := os.ReadFile(filepath.Join(resumed606.Admission.Workspace.Root, "f11-result.txt"))
	if err != nil || string(integrated) != "F11 blocker result\n" {
		t.Fatalf("published F11 result is missing from rebound 606 base: %q %v", integrated, err)
	}
	if head, err := harness.store.LoadHead(); err != nil || head.ActiveEpisodeID != "" {
		t.Fatalf("606 resume left blocker episode authority open: head=%#v err=%v", head, err)
	}
	harness.source = *resumed606.Admission
	harness.assertInvariant(t, "606 resumed after F11")
}

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
	published, candidate := harness.publishLifecycleTask(t, blocker, "blocker result\n")
	retired := harness.retireLifecycleTask(t, published, candidate)
	cleaned, err := harness.store.CleanupExecution(CleanupExecutionInput{
		ExpectedGeneration: retired.Head.ControllerGeneration,
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
		EpisodeRevision:    retired.Head.ActiveEpisodeRevision,
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
