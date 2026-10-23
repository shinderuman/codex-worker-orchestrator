package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPromotedReentryRestoresTaskAuthorityAndPreservesSource(t *testing.T) {
	t.Parallel()
	f, _, accepted, candidate := primaryPromotionFixture(t)
	promoted, err := f.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	root := f.store.identity.PrimaryRoot
	writeSuspensionTestFile(t, root, "lane-only.txt", "uncommitted correction\n")
	reentered, err := f.store.ReenterAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, candidate.TaskRef.TaskPath))
	if err != nil || digestBytes(data) != candidate.TaskRef.ContractDigest {
		t.Fatal("reentry did not restore the sealed Task contract")
	}
	data, err = os.ReadFile(filepath.Join(root, "lane-only.txt"))
	if err != nil || string(data) != "uncommitted correction\n" {
		t.Fatal("reentry lost the primary source correction")
	}
	if _, err := f.store.RecoverExecutionOperation(reentered.TransitionID); err != nil {
		t.Fatal(err)
	}
	if required, err := f.store.RequiredTaskAuthoritySync(reentered.Head); err != nil || required != nil {
		t.Fatalf("reentry manufactured a Task sync inconsistency: %v %v", required, err)
	}
}

func TestPromotedReentryRejectsUnattributedTaskMetadata(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"regular", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			f, _, accepted, candidate := primaryPromotionFixture(t)
			promoted, err := f.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
			if err != nil {
				t.Fatal(err)
			}
			root := f.store.identity.PrimaryRoot
			if mode == "regular" {
				writeSuspensionTestFile(t, root, candidate.TaskRef.TaskPath, "foreign Task\n")
			} else if err := os.Symlink("foreign", filepath.Join(root, candidate.TaskRef.TaskPath)); err != nil {
				t.Fatal(err)
			}
			if _, err := f.store.ReenterAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err == nil {
				t.Fatal("reentry overwrote unattributed Task metadata")
			}
			head, err := f.store.LoadHead()
			if err != nil || head.ControllerGeneration != promoted.Head.ControllerGeneration || head.PendingTransitionID != "" {
				t.Fatal("rejected reentry changed controller authority")
			}
			if controllerGitOutput(t, root, "rev-parse", "HEAD") != candidate.CommitOID {
				t.Fatal("rejected reentry rewrote the candidate ref")
			}
		})
	}
}
