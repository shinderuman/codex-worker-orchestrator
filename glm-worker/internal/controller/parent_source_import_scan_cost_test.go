package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSourceTreeWriterMassDeletionDoesNotLoseUntrackedFiles(t *testing.T) {
	t.Parallel()
	repo := newControllerRepo(t)
	for index := range 320 {
		writeSuspensionTestFile(t, repo, fmt.Sprintf("deleted/%04d.txt", index), "old\n")
	}
	writeSuspensionTestFile(t, repo, "kept/unchanged.txt", "retained\n")
	runControllerGit(t, repo, "add", "-A")
	runControllerGit(t, repo, "commit", "-qm", "before mass deletion")
	beforeTree := controllerGitOutput(t, repo, "rev-parse", "HEAD^{tree}")

	if err := os.RemoveAll(filepath.Join(repo, "deleted")); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "add", "-A")
	runControllerGit(t, repo, "commit", "-qm", "after mass deletion")
	afterTree := controllerGitOutput(t, repo, "rev-parse", "HEAD^{tree}")
	runControllerGit(t, repo, "reset", "--hard", "HEAD~1")
	before, err := sourceImportTreeEntries(repo, beforeTree)
	if err != nil {
		t.Fatal(err)
	}
	after, err := sourceImportTreeEntries(repo, afterTree)
	if err != nil {
		t.Fatal(err)
	}

	writeSuspensionTestFile(t, repo, "deleted/foreign.txt", "untracked\n")
	for attempt := range 2 {
		if err := applySourceTreeEntries(repo, before, after); err != nil {
			t.Fatalf("mass deletion attempt %d: %v", attempt, err)
		}
	}
	foreign, err := os.ReadFile(filepath.Join(repo, "deleted", "foreign.txt"))
	if err != nil || string(foreign) != "untracked\n" {
		t.Fatalf("mass deletion changed untracked content: %q %v", foreign, err)
	}
	if _, err := os.Stat(filepath.Join(repo, "deleted", "0000.txt")); !os.IsNotExist(err) {
		t.Fatalf("deleted file remains: %v", err)
	}
	kept, err := os.ReadFile(filepath.Join(repo, "kept", "unchanged.txt"))
	if err != nil || string(kept) != "retained\n" {
		t.Fatalf("unchanged file was modified: %q %v", kept, err)
	}
}

func TestSourceTreeWriterNestedDirectoryAndFileReplacements(t *testing.T) {
	t.Parallel()
	repo := newControllerRepo(t)
	writeSuspensionTestFile(t, repo, "nested/swap/old.txt", "old\n")
	writeSuspensionTestFile(t, repo, "nested/swap/deep/old.txt", "deep old\n")
	writeSuspensionTestFile(t, repo, "nested/kept.txt", "unchanged\n")
	writeSuspensionTestFile(t, repo, "file-to-dir", "old file\n")
	runControllerGit(t, repo, "add", "-A")
	runControllerGit(t, repo, "commit", "-qm", "before nested replacement")
	beforeTree := controllerGitOutput(t, repo, "rev-parse", "HEAD^{tree}")

	if err := os.RemoveAll(filepath.Join(repo, "nested", "swap")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "file-to-dir")); err != nil {
		t.Fatal(err)
	}
	writeSuspensionTestFile(t, repo, "nested/swap", "new file\n")
	writeSuspensionTestFile(t, repo, "file-to-dir/child.txt", "new child\n")
	runControllerGit(t, repo, "add", "-A")
	runControllerGit(t, repo, "commit", "-qm", "after nested replacement")
	afterTree := controllerGitOutput(t, repo, "rev-parse", "HEAD^{tree}")
	runControllerGit(t, repo, "reset", "--hard", "HEAD~1")
	before, err := sourceImportTreeEntries(repo, beforeTree)
	if err != nil {
		t.Fatal(err)
	}
	after, err := sourceImportTreeEntries(repo, afterTree)
	if err != nil {
		t.Fatal(err)
	}

	writeSuspensionTestFile(t, repo, "nested/swap/deep/foreign.txt", "untracked\n")
	if err := applySourceTreeEntries(repo, before, after); err == nil {
		t.Fatal("nested replacement admitted an untracked child")
	}
	foreign, err := os.ReadFile(filepath.Join(repo, "nested", "swap", "deep", "foreign.txt"))
	if err != nil || string(foreign) != "untracked\n" {
		t.Fatalf("rejected nested replacement lost foreign content: %q %v", foreign, err)
	}
	if err := os.Remove(filepath.Join(repo, "nested", "swap", "deep", "foreign.txt")); err != nil {
		t.Fatal(err)
	}
	for attempt := range 2 {
		if err := applySourceTreeEntries(repo, before, after); err != nil {
			t.Fatalf("nested replacement attempt %d: %v", attempt, err)
		}
	}
	for _, target := range []struct{ path, want string }{
		{"nested/swap", "new file\n"},
		{"file-to-dir/child.txt", "new child\n"},
		{"nested/kept.txt", "unchanged\n"},
	} {
		got, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(target.path)))
		if err != nil || string(got) != target.want {
			t.Fatalf("%s: content %q, error %v", target.path, got, err)
		}
	}
}

func TestSourceTreeImportIndexedLargeUnchangedListing(t *testing.T) {
	t.Parallel()
	const fileCount = 30000
	entries := make([]workspaceTreeEntry, fileCount)
	for index := range entries {
		entries[index] = workspaceTreeEntry{path: fmt.Sprintf("unchanged/%05d.txt", index)}
	}
	directories := sourceTreeDirectories(entries)
	lookup := sourceImportEntryMap(entries)
	if len(directories) != 1 || !directories["unchanged"] {
		t.Fatalf("unexpected directory index: %v", directories)
	}
	if children := sourceTreeDirectoryChildren(directories); len(children) != 0 {
		t.Fatalf("flat listing has child directories: %v", children)
	}
	for _, entry := range entries {
		if directories[entry.path] {
			t.Fatalf("ordinary file incorrectly identified as a replacement directory: %s", entry.path)
		}
		if _, ok := lookup[entry.path]; !ok {
			t.Fatalf("ordinary file absent from after index: %s", entry.path)
		}
		if sourceTreeAncestorReplacementPresent("", entry.path, lookup) {
			t.Fatalf("ordinary file incorrectly identified as an ancestor replacement: %s", entry.path)
		}
	}
}

func BenchmarkSourceTreeImportIndexedLookup(b *testing.B) {
	for _, fileCount := range []int{1024, 8192, 32768} {
		b.Run(fmt.Sprintf("files-%d", fileCount), func(b *testing.B) {
			entries := make([]workspaceTreeEntry, fileCount)
			for index := range entries {
				entries[index] = workspaceTreeEntry{path: fmt.Sprintf("unchanged/%05d.txt", index)}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				directories := sourceTreeDirectories(entries)
				lookup := sourceImportEntryMap(entries)
				for _, entry := range entries {
					if directories[entry.path] || sourceTreeAncestorReplacementPresent("", entry.path, lookup) {
						b.Fatalf("ordinary file unexpectedly requires replacement: %s", entry.path)
					}
				}
			}
		})
	}
}
