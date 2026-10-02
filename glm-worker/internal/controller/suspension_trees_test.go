package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSuspensionCaptureAndTwoStageRebindPreserveWorkspaceSemantics(t *testing.T) {
	repo, linked := newControllerLinkedWorktree(t)
	files := map[string]string{"staged.txt": "original\n", "unstaged.txt": "original\n", "delete-index.txt": "original\n", "delete-worktree.txt": "original\n", "mode.txt": "original\n"}
	for path, data := range files {
		writeSuspensionTestFile(t, repo, path, data)
	}
	runControllerGit(t, repo, "add", ".")
	runControllerGit(t, repo, "commit", "-qm", "workspace fixture")
	base := controllerGitOutput(t, repo, "rev-parse", "HEAD")
	writeSuspensionTestFile(t, repo, "staged.txt", "baseline staged\n")
	runControllerGit(t, repo, "add", "staged.txt")
	writeSuspensionTestFile(t, repo, "staged.txt", "baseline unstaged\n")
	baseline, err := CaptureExecutionTrees(repo, base)
	if err != nil {
		t.Fatal(err)
	}
	writeSuspensionTestFile(t, repo, "staged.txt", "task staged\n")
	runControllerGit(t, repo, "add", "staged.txt")
	writeSuspensionTestFile(t, repo, "staged.txt", "task unstaged\n")
	writeSuspensionTestFile(t, repo, "unstaged.txt", "task worktree\n")
	runControllerGit(t, repo, "rm", "delete-index.txt")
	if err := os.Remove(filepath.Join(repo, "delete-worktree.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(repo, "mode.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("staged.txt", filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	writeSuspensionTestFile(t, repo, "untracked\nname.txt", "untracked\x00binary\n")
	writeSuspensionTestFile(t, repo, "IMPLEMENTATION_PLAN.local.md", "parent-owned changed state\n")
	current, err := CaptureExecutionTrees(repo, base)
	if err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, linked, "checkout", "--detach", base)
	writeSuspensionTestFile(t, linked, "blocker.txt", "integrated blocker\n")
	runControllerGit(t, linked, "add", "blocker.txt")
	runControllerGit(t, linked, "commit", "-qm", "blocker integration")
	newBase := controllerGitOutput(t, linked, "rev-parse", "HEAD")
	result, err := RebindSuspensionTrees(repo, SuspensionSnapshot{ExecutionBaseOID: base, Baseline: baseline, Current: current}, newBase)
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct{ tree, path, want string }{
		{result.Baseline.IndexTree, "staged.txt", "baseline staged\n"},
		{result.Baseline.WorktreeTree, "staged.txt", "baseline unstaged\n"},
		{result.Current.IndexTree, "staged.txt", "task staged\n"},
		{result.Current.WorktreeTree, "staged.txt", "task unstaged\n"},
		{result.Current.IndexTree, "unstaged.txt", "original\n"},
		{result.Current.WorktreeTree, "unstaged.txt", "task worktree\n"},
		{result.Current.WorktreeTree, "untracked\nname.txt", "untracked\x00binary\n"},
		{result.Current.WorktreeTree, "link", "staged.txt"},
		{result.Baseline.IndexTree, "blocker.txt", "integrated blocker\n"},
		{result.Current.WorktreeTree, "blocker.txt", "integrated blocker\n"},
	}
	for _, check := range checks {
		data, err := runGitBinary(repo, nil, "cat-file", "blob", check.tree+":"+check.path)
		if err != nil || string(data) != check.want {
			t.Fatalf("%q: got %q, want %q, error %v", check.path, data, check.want, err)
		}
	}
	for _, check := range []struct{ tree, path string }{{result.Current.IndexTree, "delete-index.txt"}, {result.Current.WorktreeTree, "delete-index.txt"}, {result.Current.WorktreeTree, "delete-worktree.txt"}} {
		if _, err := runGitBinary(repo, nil, "cat-file", "-e", check.tree+":"+check.path); err == nil {
			t.Fatalf("deleted path %s retained", check.path)
		}
	}
	mode := controllerGitOutput(t, repo, "ls-tree", result.Current.WorktreeTree, "--", "mode.txt")
	if !strings.HasPrefix(mode, "100755 ") {
		t.Fatalf("executable mode lost: %s", mode)
	}
	parent := controllerGitOutput(t, repo, "rev-parse", newBase+":IMPLEMENTATION_PLAN.local.md")
	actual := controllerGitOutput(t, repo, "rev-parse", result.Current.WorktreeTree+":IMPLEMENTATION_PLAN.local.md")
	if parent != actual {
		t.Fatal("parent-owned metadata replayed as Task work")
	}
}

func TestSuspensionCaptureRejectsUnsupportedIndexBeforeMutation(t *testing.T) {
	for _, kind := range []string{"intent-to-add", "assume-unchanged", "unresolved"} {
		t.Run(kind, func(t *testing.T) {
			repo, _ := newControllerLinkedWorktree(t)
			base := controllerGitOutput(t, repo, "rev-parse", "HEAD")
			switch kind {
			case "intent-to-add":
				writeSuspensionTestFile(t, repo, "intent.txt", "intent\n")
				runControllerGit(t, repo, "add", "-N", "intent.txt")
			case "assume-unchanged":
				runControllerGit(t, repo, "update-index", "--assume-unchanged", "source.txt")
			case "unresolved":
				oid := controllerGitOutput(t, repo, "rev-parse", "HEAD:source.txt")
				index, err := gitTrimmed(repo, "rev-parse", "--path-format=absolute", "--git-path", "index")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := suspensionGit(repo, index, []byte("100644 "+oid+" 1\tsource.txt\n"), "update-index", "--index-info"); err != nil {
					t.Fatal(err)
				}
			}
			before, err := CaptureWorkspaceSnapshot(repo)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := CaptureExecutionTrees(repo, base); err == nil {
				t.Fatal("unsupported index accepted")
			}
			after, err := CaptureWorkspaceSnapshot(repo)
			if err != nil {
				t.Fatal(err)
			}
			if before != after {
				t.Fatal("failed capture changed repository state")
			}
		})
	}
}

func TestSuspensionRebindConflictPreservesSourceWorkspace(t *testing.T) {
	repo, linked := newControllerLinkedWorktree(t)
	base := controllerGitOutput(t, repo, "rev-parse", "HEAD")
	baseline, err := CaptureExecutionTrees(repo, base)
	if err != nil {
		t.Fatal(err)
	}
	writeSuspensionTestFile(t, repo, "source.txt", "task change\n")
	current, err := CaptureExecutionTrees(repo, base)
	if err != nil {
		t.Fatal(err)
	}
	writeSuspensionTestFile(t, linked, "source.txt", "blocker change\n")
	runControllerGit(t, linked, "add", "source.txt")
	runControllerGit(t, linked, "commit", "-qm", "overlapping blocker")
	before, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	newBase := controllerGitOutput(t, linked, "rev-parse", "HEAD")
	if _, err := RebindSuspensionTrees(repo, SuspensionSnapshot{ExecutionBaseOID: base, Baseline: baseline, Current: current}, newBase); err == nil {
		t.Fatal("conflicting rebind accepted")
	}
	after, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("failed preflight changed source workspace")
	}
}

func writeSuspensionTestFile(t *testing.T, repo, path, data string) {
	t.Helper()
	full := filepath.Join(repo, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSuspensionGitlinkCapturePreservesCleanAndRejectsDirtySubmodule(t *testing.T) {
	repo, _ := newControllerLinkedWorktree(t)
	child, _ := newControllerLinkedWorktree(t)
	runControllerGit(t, repo, "-c", "protocol.file.allow=always", "submodule", "add", child, "child")
	runControllerGit(t, repo, "commit", "-qm", "clean gitlink")
	base := controllerGitOutput(t, repo, "rev-parse", "HEAD")
	trees, err := CaptureExecutionTrees(repo, base)
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(controllerTestConfig(repo, filepath.Join(t.TempDir(), "state", "sessions")))
	if err != nil {
		t.Fatal(err)
	}
	archive, _, err := store.CaptureGitObjectArchive(repo, "clean-gitlink", []string{trees.WorktreeTree})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyGitObjectArchive(archive); err != nil {
		t.Fatal(err)
	}
	writeSuspensionTestFile(t, repo, "child/source.txt", "dirty nested work\n")
	if _, err := CaptureExecutionTrees(repo, base); err == nil {
		t.Fatal("dirty gitlink accepted")
	}
	runControllerGit(t, filepath.Join(repo, "child"), "checkout", "--", "source.txt")
	runControllerGit(t, filepath.Join(repo, "child"), "-c", "user.name=Test", "-c", "user.email=test@invalid", "commit", "--allow-empty", "-qm", "advanced gitlink")
	if _, err := CaptureExecutionTrees(repo, base); err == nil {
		t.Fatal("unstaged gitlink commit accepted without replay support")
	}
	runControllerGit(t, repo, "submodule", "deinit", "--force", "child")
	if _, err := CaptureExecutionTrees(repo, base); err != nil {
		t.Fatal(err)
	}
}
