package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParentSourceRevisionReentersWithoutMetadataCommit(t *testing.T) {
	t.Parallel()
	f, policy, head, old := acceptedAmendmentLane(t)
	repo := f.store.Identity().PrimaryRoot
	before := controllerGitOutput(t, repo, "rev-parse", "HEAD")
	task := filepath.Join(repo, old.TaskRef.TaskPath)
	data, err := os.ReadFile(task)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(task, append(data, []byte("\n## Amendments\n\nparent source revision\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSuspensionTestFile(t, repo, "parent-result.txt", "parent implementation\n")
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	input := PublicationInput{ExpectedGeneration: head.ControllerGeneration, CandidateID: old.CandidateID}
	stale := input
	stale.ExpectedGeneration--
	if _, err := f.store.RebindParentSourceCandidate(stale, snapshot.ID); err == nil {
		t.Fatal("stale generation accepted")
	}
	if _, err := f.store.RebindParentSourceCandidate(input, "stale-snapshot"); err == nil {
		t.Fatal("stale source snapshot accepted")
	}
	rebound, err := f.store.RebindParentSourceCandidate(input, snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	current, err := f.store.LoadAcceptedCandidate(*rebound.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if current.EvidenceValid || current.Previous == nil || !evidenceRefsEqual(*current.Previous, *head.AcceptedCandidateRef) || !current.TaskRef.Equal(old.TaskRef) || rebound.Head.RootTaskRef.Equal(old.TaskRef) {
		t.Fatal("source revision reused proof or lost predecessor authority")
	}
	if controllerGitOutput(t, repo, "rev-parse", "HEAD") != before || observeTestRemote(t, repo, policy) != before {
		t.Fatal("source revision published metadata")
	}
	entered, err := f.store.ReenterAcceptedCandidate(PublicationInput{ExpectedGeneration: rebound.Head.ControllerGeneration, CandidateID: current.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	live, err := f.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: entered.Head.ControllerGeneration, SuspensionID: entered.Suspension.SnapshotID})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"result.txt", "parent-result.txt", old.TaskRef.TaskPath} {
		got, err := os.ReadFile(filepath.Join(live.Admission.Workspace.Root, path))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(repo, path))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("materialized source differs: %s", path)
		}
	}
	authority, err := f.store.RuntimeMaterializationReentryAuthority(live.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if authority.PredecessorAttemptID != old.AttemptID || authority.PredecessorTaskContractDigest != old.TaskRef.ContractDigest || authority.TaskContractDigest != live.Admission.Attempt.SemanticTaskRef.ContractDigest {
		t.Fatal("runtime predecessor proof differs")
	}
	if _, err := f.store.LoadAcceptedCandidate(*head.AcceptedCandidateRef); err != nil {
		t.Fatal(err)
	}
}

func TestParentSourceRevisionRejectsDifferentActiveTaskBeforePrepare(t *testing.T) {
	t.Parallel()
	f, _, head, old := acceptedAmendmentLane(t)
	repo := f.store.Identity().PrimaryRoot
	path := filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	plan := strings.NewReplacer(old.TaskRef.TaskPath, f.child.TaskPath, f.child.TaskPath, old.TaskRef.TaskPath).Replace(string(data))
	if err := os.WriteFile(path, []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.RebindParentSourceCandidate(PublicationInput{ExpectedGeneration: head.ControllerGeneration, CandidateID: old.CandidateID}, snapshot.ID); err == nil {
		t.Fatal("different ACTIVE Task adopted")
	}
	after, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	assertFindingHeadUnchanged(t, head, after)
}

func TestParentSourceRevisionPreservesPreparedJournalOnSourceRace(t *testing.T) {
	t.Parallel()
	f, _, head, old := acceptedAmendmentLane(t)
	repo := f.store.Identity().PrimaryRoot
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	op, source, err := f.store.planParentSourceRebind(PublicationInput{ExpectedGeneration: head.ControllerGeneration, CandidateID: old.CandidateID}, snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.prepareExecutionOperation(&op, source); err != nil {
		t.Fatal(err)
	}
	writeSuspensionTestFile(t, repo, "raced-source.txt", "unapproved mutation\n")
	if _, err := f.store.RecoverExecutionOperation(op.Transition.TransitionID); err == nil {
		t.Fatal("source race accepted")
	}
	after, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if after.PendingTransitionID != op.Transition.TransitionID || after.ControllerGeneration != op.Transition.PreparedGeneration || !evidenceRefsEqual(*after.AcceptedCandidateRef, *head.AcceptedCandidateRef) {
		t.Fatal("source race lost the original candidate or recovery journal")
	}
}

func observeTestRemote(t *testing.T, repo string, policy PublicationPolicy) string {
	t.Helper()
	oid, err := observePublicationRemote(repo, policy)
	if err != nil {
		t.Fatal(err)
	}
	return oid
}
