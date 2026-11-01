package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSourceTreeWriterFileToDirectoryAndRecovery(t *testing.T) {
	t.Parallel()
	repo := newControllerRepo(t)
	writeSuspensionTestFile(t, repo, "replacement", "old-file\n")
	runControllerGit(t, repo, "add", "replacement")
	runControllerGit(t, repo, "commit", "-qm", "before")
	before, err := gitTrimmed(repo, "rev-parse", "HEAD^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "replacement")); err != nil {
		t.Fatal(err)
	}
	writeSuspensionTestFile(t, repo, "replacement/new.txt", "new-child\n")
	runControllerGit(t, repo, "add", "-A")
	runControllerGit(t, repo, "commit", "-qm", "after")
	after, err := gitTrimmed(repo, "rev-parse", "HEAD^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "reset", "--hard", "HEAD~1")
	oldEntries, err := sourceImportTreeEntries(repo, before)
	if err != nil {
		t.Fatal(err)
	}
	newEntries, err := sourceImportTreeEntries(repo, after)
	if err != nil {
		t.Fatal(err)
	}
	if err := applySourceTreeEntries(repo, oldEntries, newEntries); err != nil {
		t.Fatal(err)
	}
	if err := applySourceTreeEntries(repo, oldEntries, newEntries); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repo, "replacement", "new.txt"))
	if err != nil || string(data) != "new-child\n" {
		t.Fatalf("replacement content: %q %v", data, err)
	}
}

func TestSourceTreeWriterDirectoryToFileDoesNotDeleteForeignContent(t *testing.T) {
	t.Parallel()
	repo := newControllerRepo(t)
	writeSuspensionTestFile(t, repo, "replacement/old.txt", "old-child\n")
	runControllerGit(t, repo, "add", "replacement")
	runControllerGit(t, repo, "commit", "-qm", "before")
	before, err := gitTrimmed(repo, "rev-parse", "HEAD^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "replacement", "old.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "replacement")); err != nil {
		t.Fatal(err)
	}
	writeSuspensionTestFile(t, repo, "replacement", "new-file\n")
	runControllerGit(t, repo, "add", "-A")
	runControllerGit(t, repo, "commit", "-qm", "after")
	after, err := gitTrimmed(repo, "rev-parse", "HEAD^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "reset", "--hard", "HEAD~1")
	oldEntries, err := sourceImportTreeEntries(repo, before)
	if err != nil {
		t.Fatal(err)
	}
	newEntries, err := sourceImportTreeEntries(repo, after)
	if err != nil {
		t.Fatal(err)
	}
	writeSuspensionTestFile(t, repo, "replacement/foreign.txt", "unattributed\n")
	if err := applySourceTreeEntries(repo, oldEntries, newEntries); err == nil {
		t.Fatal("foreign child was silently deleted")
	}
	data, err := os.ReadFile(filepath.Join(repo, "replacement", "foreign.txt"))
	if err != nil || string(data) != "unattributed\n" {
		t.Fatalf("foreign child mutated: %q %v", data, err)
	}
	if err := os.Remove(filepath.Join(repo, "replacement", "foreign.txt")); err != nil {
		t.Fatal(err)
	}
	if err := applySourceTreeEntries(repo, oldEntries, newEntries); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(repo, "replacement"))
	if err != nil || string(data) != "new-file\n" {
		t.Fatalf("directory to file result: %q %v", data, err)
	}
	if err := applySourceTreeEntries(repo, oldEntries, newEntries); err != nil {
		t.Fatalf("directory-to-file retry rejected already-applied target: %v", err)
	}
}

func TestSourceTreeWriterReplacesFileWithSymlinkWithoutFollowingTarget(t *testing.T) {
	t.Parallel()
	repo := newControllerRepo(t)
	writeSuspensionTestFile(t, repo, "target.txt", "target-unchanged\n")
	writeSuspensionTestFile(t, repo, "link", "before\n")
	runControllerGit(t, repo, "add", "link", "target.txt")
	runControllerGit(t, repo, "commit", "-qm", "before")
	before, err := gitTrimmed(repo, "rev-parse", "HEAD^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.txt", filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "add", "-A")
	runControllerGit(t, repo, "commit", "-qm", "after")
	after, err := gitTrimmed(repo, "rev-parse", "HEAD^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "reset", "--hard", "HEAD~1")
	oldEntries, err := sourceImportTreeEntries(repo, before)
	if err != nil {
		t.Fatal(err)
	}
	newEntries, err := sourceImportTreeEntries(repo, after)
	if err != nil {
		t.Fatal(err)
	}
	if err := applySourceTreeEntries(repo, oldEntries, newEntries); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(repo, "link"))
	if err != nil || target != "target.txt" {
		t.Fatalf("not a symlink: %q %v", target, err)
	}
	data, err := os.ReadFile(filepath.Join(repo, "target.txt"))
	if err != nil || string(data) != "target-unchanged\n" {
		t.Fatalf("symlink target modified: %q %v", data, err)
	}
}
