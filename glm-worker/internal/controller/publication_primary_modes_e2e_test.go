package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootPromotionDirectoryToRelativeSymlinkPreservesTarget(t *testing.T) {
	t.Parallel()
	fixture, _, accepted, candidate := primaryPromotionFixtureWithTreeEdit(t, "external/child", "old child\n", func(root string) {
		if err := os.Remove(filepath.Join(root, "external", "child")); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(root, "external")); err != nil {
			t.Fatal(err)
		}
		writeSuspensionTestFile(t, root, "target.txt", "untouched target\n")
		if err := os.Symlink("target.txt", filepath.Join(root, "external")); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{
		ExpectedGeneration: accepted.Head.ControllerGeneration,
		CandidateID: candidate.CandidateID,
	}); err != nil {
		t.Fatal(err)
	}
	root := fixture.store.identity.PrimaryRoot
	target, err := os.Readlink(filepath.Join(root, "external"))
	if err != nil || target != "target.txt" {
		t.Fatalf("directory-to-symlink target = %q %v", target, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "target.txt"))
	if err != nil || string(data) != "untouched target\n" {
		t.Fatalf("directory-to-symlink modified target = %q %v", data, err)
	}
	if err := fixture.store.verifyPrimaryPublicationTree(candidate); err != nil {
		t.Fatalf("primary worktree/index differ from accepted candidate: %v", err)
	}
}

func TestRootPromotionRegularFileToExecutablePreservesMode(t *testing.T) {
	t.Parallel()
	fixture, _, accepted, candidate := primaryPromotionFixtureWithTreeEdit(t, "external", "old external file\n", func(root string) {
		writeSuspensionTestFile(t, root, "external", "new executable\n")
		if err := os.Chmod(filepath.Join(root, "external"), 0o755); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{
		ExpectedGeneration: accepted.Head.ControllerGeneration,
		CandidateID: candidate.CandidateID,
	}); err != nil {
		t.Fatal(err)
	}
	root := fixture.store.identity.PrimaryRoot
	info, err := os.Lstat(filepath.Join(root, "external"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("promoted file is not executable: %v", info.Mode())
	}
	data, err := os.ReadFile(filepath.Join(root, "external"))
	if err != nil || string(data) != "new executable\n" {
		t.Fatalf("promoted executable content = %q err=%v", data, err)
	}
	if err := fixture.store.verifyPrimaryPublicationTree(candidate); err != nil {
		t.Fatalf("primary worktree/index differ from accepted candidate: %v", err)
	}
}
