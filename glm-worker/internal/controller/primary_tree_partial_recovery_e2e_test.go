package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootPromotionRecoversDirectoryToFileAfterPartialDeletion(t *testing.T) {
	t.Parallel()
	fixture, policy, accepted, candidate := primaryPromotionFixtureWithTreeEdit(t,
		"external/child", "old child\n", func(root string) {
			if err := os.Remove(filepath.Join(root, "external", "child")); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(root, "external")); err != nil {
				t.Fatal(err)
			}
			writeSuspensionTestFile(t, root, "external", "accepted file\n")
		})
	candidate.State = candidatePromoted
	op, err := fixture.store.planCandidateRevision(accepted.Head, candidate, *accepted.CandidateRef, publicationPromote)
	if err != nil {
		t.Fatal(err)
	}
	op.Publication.LocalOld = candidate.BaseOID
	op.Publication.RemoteOID = observeTestRemote(t, fixture.store.identity.PrimaryRoot, policy)
	if err := fixture.store.planPrimaryPromotion(&op, candidate); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.prepareExecutionOperation(&op, accepted.Head); err != nil {
		t.Fatal(err)
	}
	root := fixture.store.identity.PrimaryRoot
	if err := os.Remove(filepath.Join(root, "external", "child")); err != nil {
		t.Fatal(err)
	}
	writeSuspensionTestFile(t, root, "external/foreign", "foreign content\n")
	if _, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID); err == nil {
		t.Fatal("partial promotion accepted an unowned child")
	}
	foreign, err := os.ReadFile(filepath.Join(root, "external", "foreign"))
	if err != nil || string(foreign) != "foreign content\n" {
		t.Fatalf("rejected recovery changed foreign content: %q, %v", foreign, err)
	}
	if controllerGitOutput(t, root, "rev-parse", policy.LocalRef) != candidate.BaseOID {
		t.Fatal("rejected recovery moved the publication ref")
	}
	if err := os.Remove(filepath.Join(root, "external", "foreign")); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID); err != nil {
		t.Fatalf("legitimate partial directory replacement failed recovery: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(root, "external"))
	if err != nil || string(content) != "accepted file\n" {
		t.Fatalf("recovered primary file: %q, %v", content, err)
	}
	if err := fixture.store.verifyPrimaryPublicationTree(candidate); err != nil {
		t.Fatalf("recovered worktree/index do not match accepted tree: %v", err)
	}
}
