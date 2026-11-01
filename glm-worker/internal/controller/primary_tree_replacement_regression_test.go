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

func TestSourceTreeWriterReplacesSymlinkWithExecutableWithoutFollowingTarget(t *testing.T) {
	t.Parallel()
	repo := newControllerRepo(t)
	writeSuspensionTestFile(t, repo, "target.txt", "target untouched\n")
	if err := os.Symlink("target.txt", filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "add", "link", "target.txt")
	runControllerGit(t, repo, "commit", "-qm", "before")
	beforeTree, err := gitTrimmed(repo, "rev-parse", "HEAD^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	writeSuspensionTestFile(t, repo, "link", "executable replacement\n")
	if err := os.Chmod(filepath.Join(repo, "link"), 0o755); err != nil {
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
	for attempt := 0; attempt < 2; attempt++ {
		if err := applySourceTreeEntries(repo, before, after); err != nil {
			t.Fatalf("retry %d: %v", attempt, err)
		}
	}
	info, err := os.Lstat(filepath.Join(repo, "link"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("replacement lost executable mode: %v", info.Mode())
	}
	data, err := os.ReadFile(filepath.Join(repo, "link"))
	if err != nil || string(data) != "executable replacement\n" {
		t.Fatalf("replacement content: %q %v", data, err)
	}
	data, err = os.ReadFile(filepath.Join(repo, "target.txt"))
	if err != nil || string(data) != "target untouched\n" {
		t.Fatalf("symlink target was modified: %q %v", data, err)
	}
}

func TestSourceTreeWriterRetargetsSymlinkWithoutFollowingEitherTarget(t *testing.T) {
	t.Parallel()
	repo := newControllerRepo(t)
	writeSuspensionTestFile(t, repo, "original.txt", "original content\n")
	writeSuspensionTestFile(t, repo, "replacement.txt", "replacement content\n")
	if err := os.Symlink("original.txt", filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "add", "link", "original.txt", "replacement.txt")
	runControllerGit(t, repo, "commit", "-qm", "before")
	beforeTree, err := gitTrimmed(repo, "rev-parse", "HEAD^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("replacement.txt", filepath.Join(repo, "link")); err != nil {
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
	for attempt := 0; attempt < 2; attempt++ {
		if err := applySourceTreeEntries(repo, before, after); err != nil {
			t.Fatalf("retarget attempt %d: %v", attempt, err)
		}
	}
	link, err := os.Readlink(filepath.Join(repo, "link"))
	if err != nil || link != "replacement.txt" {
		t.Fatalf("link target = %q, err=%v", link, err)
	}
	for _, target := range []struct{ name, content string }{
		{"original.txt", "original content\n"},
		{"replacement.txt", "replacement content\n"},
	} {
		data, err := os.ReadFile(filepath.Join(repo, target.name))
		if err != nil || string(data) != target.content {
			t.Fatalf("symlink retarget altered %s: %q, err=%v", target.name, data, err)
		}
	}
}
