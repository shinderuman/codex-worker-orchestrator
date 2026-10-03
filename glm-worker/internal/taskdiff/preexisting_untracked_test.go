package taskdiff

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPreexistingUntrackedChangeAndDeletionAreTaskDiff(t *testing.T) {
	repoRoot, cfg, st := newPreexistingUntrackedFixture(t, "text")
	writeFile(t, filepath.Join(repoRoot, "changed.txt"), "before\n")
	writeFile(t, filepath.Join(repoRoot, "deleted.txt"), "delete me\n")
	writeFile(t, filepath.Join(repoRoot, "unchanged.txt"), "same\n")
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(repoRoot, "changed.txt"), "after\n")
	if err := os.Remove(filepath.Join(repoRoot, "deleted.txt")); err != nil {
		t.Fatal(err)
	}

	paths, available, err := ChangedPaths(repoRoot, st)
	if err != nil || !available {
		t.Fatalf("ChangedPaths: available=%v err=%v", available, err)
	}
	seen := pathSet(paths)
	for _, want := range []string{"changed.txt", "deleted.txt"} {
		if !seen[want] {
			t.Fatalf("changed paths missing %s: %v", want, paths)
		}
	}
	if seen["unchanged.txt"] {
		t.Fatalf("unchanged pre-existing untracked path leaked into task scope: %v", paths)
	}

	diff, available, err := Capture(repoRoot, st)
	if err != nil || !available {
		t.Fatalf("Capture: available=%v err=%v", available, err)
	}
	for _, want := range [][]byte{
		[]byte("changed.txt"),
		[]byte("-before"),
		[]byte("+after"),
		[]byte("deleted.txt"),
		[]byte("-delete me"),
		[]byte("deleted file mode"),
	} {
		if !bytes.Contains(diff, want) {
			t.Fatalf("task diff missing %q:\n%s", want, diff)
		}
	}
	if bytes.Contains(diff, []byte("unchanged.txt")) {
		t.Fatalf("unchanged pre-existing untracked path leaked into diff:\n%s", diff)
	}
}

func TestPreexistingUntrackedBinarySymlinkModeAndTrackedTransitions(t *testing.T) {
	repoRoot, cfg, st := newPreexistingUntrackedFixture(t, "types")
	writeFile(t, filepath.Join(repoRoot, "binary.bin"), "\x00before\n")
	if err := os.Symlink("target-before", filepath.Join(repoRoot, "link")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repoRoot, "mode.sh"), "#!/bin/sh\necho mode\n")
	if err := os.Chmod(filepath.Join(repoRoot, "mode.sh"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repoRoot, "tracked-later.txt"), "same bytes\n")
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(repoRoot, "binary.bin"), "\x00after\n")
	if err := os.Remove(filepath.Join(repoRoot, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target-after", filepath.Join(repoRoot, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(repoRoot, "mode.sh"), 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoRoot, "add", "tracked-later.txt")

	paths, available, err := ChangedPaths(repoRoot, st)
	if err != nil || !available {
		t.Fatalf("ChangedPaths: available=%v err=%v", available, err)
	}
	seen := pathSet(paths)
	for _, want := range []string{"binary.bin", "link", "mode.sh", "tracked-later.txt"} {
		if !seen[want] {
			t.Fatalf("changed paths missing %s: %v", want, paths)
		}
	}

	diff, available, err := Capture(repoRoot, st)
	if err != nil || !available {
		t.Fatalf("Capture: available=%v err=%v", available, err)
	}
	for _, want := range [][]byte{
		[]byte("binary.bin"),
		[]byte("GIT binary patch"),
		[]byte("link"),
		[]byte("-target-before"),
		[]byte("+target-after"),
		[]byte("old mode 100644"),
		[]byte("new mode 100755"),
		[]byte("tracked-later.txt"),
		[]byte("new file mode 100644"),
		[]byte("+same bytes"),
	} {
		if !bytes.Contains(diff, want) {
			t.Fatalf("task diff missing %q:\n%s", want, diff)
		}
	}
}

func TestPreexistingUntrackedSnapshotMissingFailsClosed(t *testing.T) {
	repoRoot, cfg, st := newPreexistingUntrackedFixture(t, "missing")
	writeFile(t, filepath.Join(repoRoot, "preexisting.txt"), "before\n")
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(st.Path("baseline-untracked")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repoRoot, "preexisting.txt"), "after\n")

	if _, available, err := ChangedPaths(repoRoot, st); err == nil || available {
		t.Fatalf("ChangedPaths accepted missing snapshot: available=%v err=%v", available, err)
	}
	if _, available, err := Capture(repoRoot, st); err == nil || available {
		t.Fatalf("Capture accepted missing snapshot: available=%v err=%v", available, err)
	}
}

func TestPreexistingUntrackedLegacyPathListIsRejected(t *testing.T) {
	repoRoot, cfg, st := newPreexistingUntrackedFixture(t, "legacy")
	writeFile(t, filepath.Join(repoRoot, "preexisting.txt"), "before\n")
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(st.Path("baseline-untracked")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path("baseline-untracked"), []byte("preexisting.txt\x00"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := ChangedPaths(repoRoot, st)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("legacy path-only baseline was not rejected: %v", err)
	}
}

func newPreexistingUntrackedFixture(t *testing.T, suffix string) (string, config.AppConfig, *state.StateStore) {
	t.Helper()
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
		RepoHash:  "preexisting-untracked-" + suffix,
		StateBase: filepath.Join(root, ".glm-worker", "sessions"),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return repoRoot, cfg, st
}
