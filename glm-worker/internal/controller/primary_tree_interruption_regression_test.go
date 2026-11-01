package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSourceTreeWriterResumesAfterFileToDirectoryRemoval(t *testing.T) {
	t.Parallel()
	repo := newControllerRepo(t)
	writeSuspensionTestFile(t, repo, "replaced", "old file\n")
	runControllerGit(t, repo, "add", "replaced")
	runControllerGit(t, repo, "commit", "-qm", "before")
	beforeTree, err := gitTrimmed(repo, "rev-parse", "HEAD^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "replaced")); err != nil {
		t.Fatal(err)
	}
	writeSuspensionTestFile(t, repo, "replaced/child", "new child\n")
	runControllerGit(t, repo, "add", "-A")
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
	if err := os.Remove(filepath.Join(repo, "replaced")); err != nil {
		t.Fatal(err)
	}
	if err := applySourceTreeEntries(repo, before, after); err != nil {
		t.Fatalf("replay after old file removal: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(repo, "replaced", "child"))
	if err != nil || string(content) != "new child\n" {
		t.Fatalf("new child = %q, %v", content, err)
	}
}

func TestSourceTreeWriterResumesAfterDirectoryChildrenRemoved(t *testing.T) {
	t.Parallel()
	repo := newControllerRepo(t)
	writeSuspensionTestFile(t, repo, "replaced/child", "old child\n")
	runControllerGit(t, repo, "add", "replaced")
	runControllerGit(t, repo, "commit", "-qm", "before")
	beforeTree, err := gitTrimmed(repo, "rev-parse", "HEAD^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "replaced", "child")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "replaced")); err != nil {
		t.Fatal(err)
	}
	writeSuspensionTestFile(t, repo, "replaced", "new file\n")
	runControllerGit(t, repo, "add", "-A")
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
	if err := os.Remove(filepath.Join(repo, "replaced", "child")); err != nil {
		t.Fatal(err)
	}
	if err := applySourceTreeEntries(repo, before, after); err != nil {
		t.Fatalf("replay after child removal: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(repo, "replaced"))
	if err != nil || string(content) != "new file\n" {
		t.Fatalf("new file = %q, %v", content, err)
	}
}

func TestSourceTreeWriterRetargetsSymlinkWithoutDereferencing(t *testing.T) {
	t.Parallel()
	repo := newControllerRepo(t)
	writeSuspensionTestFile(t, repo, "target-old", "unchanged old\n")
	writeSuspensionTestFile(t, repo, "target-new", "unchanged new\n")
	if err := os.Symlink("target-old", filepath.Join(repo, "replaced")); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "add", "replaced", "target-old", "target-new")
	runControllerGit(t, repo, "commit", "-qm", "before")
	beforeTree, err := gitTrimmed(repo, "rev-parse", "HEAD^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "replaced")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target-new", filepath.Join(repo, "replaced")); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "add", "-A")
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
	for iteration := 0; iteration < 2; iteration++ {
		if err := applySourceTreeEntries(repo, before, after); err != nil {
			t.Fatalf("retarget iteration %d: %v", iteration, err)
		}
	}
	got, err := os.Readlink(filepath.Join(repo, "replaced"))
	if err != nil || got != "target-new" {
		t.Fatalf("link target = %q, %v", got, err)
	}
	for _, target := range []struct{ path, want string }{
		{"target-old", "unchanged old\n"},
		{"target-new", "unchanged new\n"},
	} {
		got, err := os.ReadFile(filepath.Join(repo, target.path))
		if err != nil || string(got) != target.want {
			t.Fatalf("%s changed: %q, %v", target.path, got, err)
		}
	}
}
