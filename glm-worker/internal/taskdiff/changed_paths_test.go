package taskdiff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestChangedPathsUsesCapturedDirtyBaseline(t *testing.T) {
	root := t.TempDir()
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoRoot, "init", "-q")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "Test")
	for _, path := range []string{
		"clean-task.txt",
		"staged-unchanged.txt",
		"staged-task.txt",
		"unstaged-unchanged.txt",
		"unstaged-task.txt",
	} {
		writeFile(t, filepath.Join(repoRoot, path), "committed\n")
	}
	runGit(t, repoRoot, "add", ".")
	runGit(t, repoRoot, "commit", "-qm", "baseline")

	writeFile(t, filepath.Join(repoRoot, "staged-unchanged.txt"), "baseline staged unchanged\n")
	writeFile(t, filepath.Join(repoRoot, "staged-task.txt"), "baseline staged task\n")
	runGit(t, repoRoot, "add", "staged-unchanged.txt", "staged-task.txt")
	writeFile(t, filepath.Join(repoRoot, "unstaged-unchanged.txt"), "baseline unstaged unchanged\n")
	writeFile(t, filepath.Join(repoRoot, "unstaged-task.txt"), "baseline unstaged task\n")
	writeFile(t, filepath.Join(repoRoot, "baseline-untracked.txt"), "baseline untracked\n")

	cfg := config.AppConfig{
		RepoRoot:  repoRoot,
		RepoHash:  strings.Repeat("b", 64),
		StateBase: filepath.Join(root, ".glm-worker", "sessions"),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(repoRoot, "clean-task.txt"), "task clean\n")
	writeFile(t, filepath.Join(repoRoot, "staged-task.txt"), "task after staged baseline\n")
	writeFile(t, filepath.Join(repoRoot, "unstaged-task.txt"), "task after unstaged baseline\n")
	writeFile(t, filepath.Join(repoRoot, "task-untracked.txt"), "task untracked\n")

	paths, available, err := ChangedPaths(repoRoot, st)
	if err != nil {
		t.Fatal(err)
	}
	if !available {
		t.Fatal("baseline unexpectedly unavailable")
	}
	seen := pathSet(paths)
	for _, want := range []string{"clean-task.txt", "staged-task.txt", "unstaged-task.txt", "task-untracked.txt"} {
		if !seen[want] {
			t.Fatalf("changed paths missing %s: %v", want, paths)
		}
	}
	for _, unwanted := range []string{"staged-unchanged.txt", "unstaged-unchanged.txt", "baseline-untracked.txt"} {
		if seen[unwanted] {
			t.Fatalf("pre-task path %s was reclassified as task-owned: %v", unwanted, paths)
		}
	}
}

func TestChangedPathsKeepsTaskCreatedPathAfterCommit(t *testing.T) {
	root := t.TempDir()
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoRoot, "init", "-q")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "Test")
	writeFile(t, filepath.Join(repoRoot, "seed.txt"), "seed\n")
	runGit(t, repoRoot, "add", "seed.txt")
	runGit(t, repoRoot, "commit", "-qm", "baseline")

	cfg := config.AppConfig{
		RepoRoot:  repoRoot,
		RepoHash:  strings.Repeat("c", 64),
		StateBase: filepath.Join(root, ".glm-worker", "sessions"),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(repoRoot, "created.txt"), "created\n")
	before, available, err := ChangedPaths(repoRoot, st)
	if err != nil || !available {
		t.Fatalf("before commit: available=%v err=%v", available, err)
	}
	if !pathSet(before)["created.txt"] {
		t.Fatalf("before commit paths = %v", before)
	}

	runGit(t, repoRoot, "add", "created.txt")
	runGit(t, repoRoot, "commit", "-qm", "task change")
	after, available, err := ChangedPaths(repoRoot, st)
	if err != nil || !available {
		t.Fatalf("after commit: available=%v err=%v", available, err)
	}
	if !pathSet(after)["created.txt"] {
		t.Fatalf("after commit paths = %v", after)
	}
}

func TestChangedPathsSupportsCapturedUnbornBaseline(t *testing.T) {
	root := t.TempDir()
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoRoot, "init", "-q")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "Test")

	cfg := config.AppConfig{
		RepoRoot:  repoRoot,
		RepoHash:  strings.Repeat("d", 64),
		StateBase: filepath.Join(root, ".glm-worker", "sessions"),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	if st.Exists("baseline-head") {
		t.Fatal("unborn repository must not invent a baseline HEAD")
	}

	writeFile(t, filepath.Join(repoRoot, "created.txt"), "created\n")
	paths, available, err := ChangedPaths(repoRoot, st)
	if err != nil || !available {
		t.Fatalf("unborn changed paths: available=%v err=%v", available, err)
	}
	if !pathSet(paths)["created.txt"] {
		t.Fatalf("unborn changed paths = %v", paths)
	}
}

func pathSet(paths []string) map[string]bool {
	result := make(map[string]bool, len(paths))
	for _, path := range paths {
		result[path] = true
	}
	return result
}
