package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func primaryPromotionFixture(t *testing.T) (findingAcceptanceFixture, PublicationPolicy, ExecutionOperationResult, AcceptedCandidate) {
	t.Helper()
	f, policy := newPublicationTestFixture(t)
	advancePublicationTestRemote(t, f, policy, "external.txt", "external baseline\n")
	suspended, err := f.store.AdoptMutableExternalAdvancement(f.source, policy)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := f.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: suspended.Head.ControllerGeneration, SuspensionID: suspended.Suspension.SnapshotID})
	if err != nil {
		t.Fatal(err)
	}
	f.source = *resumed.Admission
	source := publicationTestEdit(t, f, "lane-only.txt", "reviewed lane result\n")
	accepted, err := f.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "lane result", Policy: policy, Evidence: publicationTestEvidence(t, f.store, source, policy)})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := f.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	return f, policy, accepted, candidate
}

func TestRootPromotionMaterializesRetainedLaneSourceAndIndex(t *testing.T) {
	t.Parallel()
	f, _, accepted, candidate := primaryPromotionFixture(t)
	promoted, err := f.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(f.store.identity.PrimaryRoot, "lane-only.txt"))
	if err != nil || string(data) != "reviewed lane result\n" {
		t.Fatalf("accepted lane source was not materialized in primary: %v", err)
	}
	if err := f.store.verifyPrimaryPublicationTree(candidate); err != nil {
		t.Fatal(err)
	}
	var op ExecutionOperation
	if err := readJSON(f.store.executionOperationPath(promoted.TransitionID), &op); err != nil {
		t.Fatal(err)
	}
	if op.Publication.PrimaryArchive == nil {
		t.Fatal("primary input was not retained")
	}
	if _, err := f.store.VerifyGitObjectArchive(*op.Publication.PrimaryArchive); err != nil {
		t.Fatal(err)
	}
}

func TestRootPromotionRejectsUnattributedPrimaryChangesBeforeMutation(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"worktree", "index-only"} {
		t.Run(mode, func(t *testing.T) {
			f, _, accepted, candidate := primaryPromotionFixture(t)
			root := f.store.identity.PrimaryRoot
			path := filepath.Join(root, "unrelated.txt")
			writeSuspensionTestFile(t, root, "unrelated.txt", "must survive\n")
			if mode == "index-only" {
				runControllerGit(t, root, "add", "unrelated.txt")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			before := controllerGitOutput(t, root, "rev-parse", "HEAD")
			if _, err := f.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err == nil {
				t.Fatal("unattributed primary changes were overwritten")
			}
			head, err := f.store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			assertFindingHeadUnchanged(t, accepted.Head, head)
			if controllerGitOutput(t, root, "rev-parse", "HEAD") != before {
				t.Fatal("rejected primary promotion changed local history")
			}
			if mode == "index-only" {
				if controllerGitOutput(t, root, "show", ":unrelated.txt") != "must survive" {
					t.Fatal("rejected promotion lost staged data")
				}
			} else if data, err := os.ReadFile(path); err != nil || string(data) != "must survive\n" {
				t.Fatal("rejected promotion lost working data")
			}
		})
	}
}

func TestRootPromotionRecoversExactPartialSourceAndRejectsForeignMutation(t *testing.T) {
	t.Parallel()
	f, policy, accepted, candidate := primaryPromotionFixture(t)
	candidate.State = candidatePromoted
	op, err := f.store.planCandidateRevision(accepted.Head, candidate, *accepted.CandidateRef, publicationPromote)
	if err != nil {
		t.Fatal(err)
	}
	op.Publication.LocalOld = candidate.BaseOID
	op.Publication.RemoteOID = observeTestRemote(t, f.store.identity.PrimaryRoot, policy)
	if err := f.store.planPrimaryPromotion(&op, candidate); err != nil {
		t.Fatal(err)
	}
	if err := f.store.prepareExecutionOperation(&op, accepted.Head); err != nil {
		t.Fatal(err)
	}
	entries, err := sourceImportTreeEntries(f.store.identity.PrimaryRoot, candidate.TreeOID)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.path == "lane-only.txt" {
			if err := applyParentSourceEntry(f.store.identity.PrimaryRoot, workspaceTreeEntry{}, entry); err != nil {
				t.Fatal(err)
			}
		}
	}
	root := f.store.identity.PrimaryRoot
	writeSuspensionTestFile(t, root, "foreign.txt", "foreign data\n")
	if _, err := f.store.RecoverExecutionOperation(op.Transition.TransitionID); err == nil {
		t.Fatal("foreign source was ignored during promotion recovery")
	}
	if controllerGitOutput(t, root, "rev-parse", policy.LocalRef) != candidate.BaseOID {
		t.Fatal("rejected recovery changed the publication branch")
	}
	if err := os.Remove(filepath.Join(root, "foreign.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.RecoverExecutionOperation(op.Transition.TransitionID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.verifyPrimaryPublicationTree(candidate); err != nil {
		t.Fatal(err)
	}
}
