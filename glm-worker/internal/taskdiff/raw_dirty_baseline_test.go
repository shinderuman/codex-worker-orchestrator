package taskdiff

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type rawDirtyTextconvBaseline struct {
	repo, marker, bytes string
	store               *state.StateStore
}

func TestCaptureRawChangesFromDirtyTextconvBaseline(t *testing.T) {
	for _, baselineMode := range []string{"staged", "unstaged", "staged-and-unstaged"} {
		for _, taskMode := range []string{"staged", "unstaged", "staged-and-unstaged"} {
			t.Run(baselineMode+"/"+taskMode, func(t *testing.T) {
				baseline := newRawDirtyTextconvBaseline(t, baselineMode)
				unchanged, available, err := Capture(baseline.repo, baseline.store)
				if err != nil || !available || len(unchanged) != 0 {
					t.Fatalf("dirty baseline is not a zero task diff: available=%v err=%v patch=%s", available, err, unchanged)
				}
				currentBytes := writeRawDirtyTask(t, baseline.repo, taskMode)
				patch, available, err := Capture(baseline.repo, baseline.store)
				if err != nil || !available {
					t.Fatalf("raw task patch unavailable: available=%v err=%v", available, err)
				}
				for _, want := range []string{"diff --git a/tracked.txt b/tracked.txt", "\n-" + baseline.bytes, "\n+" + currentBytes} {
					if !bytes.Contains(patch, []byte(want)) {
						t.Fatalf("task patch does not compare dirty baseline to current raw bytes, missing %q: %s", want, patch)
					}
				}
				if bytes.Contains(patch, []byte("-committed bytes")) {
					t.Fatalf("task patch leaked preexisting baseline changes: %s", patch)
				}
				pathPatches, err := BaselineWorktreePathPatches(baseline.repo, baseline.store, []string{"tracked.txt"})
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range []string{"\n-" + baseline.bytes, "\n+" + currentBytes} {
					if !bytes.Contains(pathPatches["tracked.txt"], []byte(want)) {
						t.Fatalf("path patch lost baseline-relative raw bytes %q: %s", want, pathPatches["tracked.txt"])
					}
				}
				if _, err := os.Stat(baseline.marker); !os.IsNotExist(err) {
					t.Fatalf("baseline/task diff executed textconv: %v", err)
				}
			})
		}
	}
}

func TestRawSnapshotIdentityFromDirtyTextconvBaseline(t *testing.T) {
	for _, baselineMode := range []string{"staged", "unstaged", "staged-and-unstaged"} {
		for _, taskMode := range []string{"staged", "unstaged", "staged-and-unstaged"} {
			t.Run(baselineMode+"/"+taskMode, func(t *testing.T) {
				baseline := newRawDirtyTextconvBaseline(t, baselineMode)
				before, err := state.CaptureGitSnapshot(baseline.repo)
				if err != nil {
					t.Fatal(err)
				}
				writeRawDirtyTask(t, baseline.repo, taskMode)
				after, err := state.CaptureGitSnapshot(baseline.repo)
				if err != nil {
					t.Fatal(err)
				}
				if state.EqualGitSnapshot(before, after) {
					t.Fatal("raw change from dirty baseline did not change snapshot")
				}
				if (before.IndexDigest != after.IndexDigest) != (taskMode != "unstaged") {
					t.Fatal("snapshot index identity does not reflect staging")
				}
				if taskMode == "unstaged" && before.WorktreeDigest == after.WorktreeDigest {
					t.Fatal("unstaged raw change did not change worktree identity")
				}
				if _, err := os.Stat(baseline.marker); !os.IsNotExist(err) {
					t.Fatalf("baseline/snapshot executed textconv: %v", err)
				}
			})
		}
	}
}

func newRawDirtyTextconvBaseline(t *testing.T, mode string) rawDirtyTextconvBaseline {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "committed bytes\n")
	writeFile(t, filepath.Join(repo, ".gitattributes"), "tracked.txt diff=raw-audit\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "raw baseline fixture")
	marker := filepath.Join(root, "textconv-executed")
	driver := filepath.Join(root, "textconv.sh")
	writeFile(t, driver, fmt.Sprintf("#!/bin/sh\nprintf invoked >> %q\nprintf 'constant\\n'\n", marker))
	if err := os.Chmod(driver, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "config", "diff.raw-audit.textconv", driver)
	baselineBytes := "baseline worktree bytes\n"
	if mode != "unstaged" {
		writeFile(t, filepath.Join(repo, "tracked.txt"), "baseline index bytes\n")
		runGit(t, repo, "add", "tracked.txt")
	}
	if mode == "staged" {
		baselineBytes = "baseline index bytes\n"
	} else {
		writeFile(t, filepath.Join(repo, "tracked.txt"), baselineBytes)
	}
	cfg := config.AppConfig{RepoRoot: repo, RepoHash: strings.Repeat("a", 64), StateBase: filepath.Join(root, "state")}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	return rawDirtyTextconvBaseline{repo: repo, marker: marker, bytes: baselineBytes, store: st}
}

func writeRawDirtyTask(t *testing.T, repo, mode string) string {
	t.Helper()
	if mode != "unstaged" {
		writeFile(t, filepath.Join(repo, "tracked.txt"), "task index bytes\n")
		runGit(t, repo, "add", "tracked.txt")
	}
	if mode == "staged" {
		return "task index bytes\n"
	}
	writeFile(t, filepath.Join(repo, "tracked.txt"), "task worktree bytes\n")
	return "task worktree bytes\n"
}
