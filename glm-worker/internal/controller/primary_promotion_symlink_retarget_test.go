package controller

import (
	"os"
	"path/filepath"
	"testing"
)

// A symlink retarget must apply the archived link blob without dereferencing
// either the old or the new target, including during an idempotent retry.
func TestSourceTreeWriterRetargetsSymlinkWithoutFollowingTargets(t *testing.T) {
	t.Parallel()
	repo := newControllerRepo(t)
	writeSuspensionTestFile(t, repo, "old-target.txt", "old target untouched\n")
	writeSuspensionTestFile(t, repo, "new-target.txt", "new target untouched\n")
	link := filepath.Join(repo, "link")
	if err := os.Symlink("old-target.txt", link); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "add", "link", "old-target.txt", "new-target.txt")
	runControllerGit(t, repo, "commit", "-qm", "before")
	beforeTree, err := gitTrimmed(repo, "rev-parse", "HEAD^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("new-target.txt", link); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "add", "link")
	runControllerGit(t, repo, "commit", "-qm", "after")
	afterTree, err := gitTrimmed(repo, "rev-parse", "HEAD^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "reset", "--hard", "HEAD~1")
	before, err := sourceImportTreeEntries(repo, beforeTree)
	if err != nil {
		t.Fatal(err)
	}
	after, err := sourceImportTreeEntries(repo, afterTree)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := applySourceTreeEntries(repo, before, after); err != nil {
			t.Fatalf("symlink retarget attempt %d: %v", attempt, err)
		}
		target, err := os.Readlink(link)
		if err != nil || target != "new-target.txt" {
			t.Fatalf("retargeted link = %q, %v", target, err)
		}
		for _, item := range []struct{ path, want string }{
			{"old-target.txt", "old target untouched\n"},
			{"new-target.txt", "new target untouched\n"},
		} {
			data, err := os.ReadFile(filepath.Join(repo, item.path))
			if err != nil || string(data) != item.want {
				t.Fatalf("target %s modified: %q, %v", item.path, data, err)
			}
		}
	}
}
