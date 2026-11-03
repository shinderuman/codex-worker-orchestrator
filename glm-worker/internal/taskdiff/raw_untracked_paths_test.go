package taskdiff

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestCaptureRawUntrackedPathsIgnoresPresentationDrivers(t *testing.T) {
	for _, route := range []string{"task-created-untracked", "preexisting-staged-then-removed", "preexisting-modified"} {
		t.Run(route, func(t *testing.T) {
			repo, cfg, st := newPreexistingUntrackedFixture(t, "raw-"+route)
			root := t.TempDir()
			marker := filepath.Join(root, "presentation-driver-executed")
			driver := filepath.Join(root, "driver.sh")
			writeFile(t, driver, fmt.Sprintf("#!/bin/sh\nprintf invoked >> %q\nprintf 'constant\\n'\n", marker))
			if err := os.Chmod(driver, 0o700); err != nil {
				t.Fatal(err)
			}
			attributes := filepath.Join(repo, ".git", "raw-audit.attributes")
			writeFile(t, attributes, "*.txt diff=raw-audit\n")
			globalConfig := filepath.Join(repo, ".git", "raw-audit.config")
			runGit(t, repo, "config", "--file", globalConfig, "core.attributesFile", attributes)
			runGit(t, repo, "config", "--file", globalConfig, "diff.raw-audit.command", driver)
			runGit(t, repo, "config", "--file", globalConfig, "diff.raw-audit.textconv", driver)
			t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
			t.Setenv("GIT_EXTERNAL_DIFF", driver)
			path := filepath.Join(repo, "review.txt")
			if route != "task-created-untracked" {
				writeFile(t, path, "preexisting untracked bytes\n")
			}
			if err := state.CaptureGitBaseline(cfg, st); err != nil {
				t.Fatal(err)
			}
			unchanged, available, err := Capture(repo, st)
			if err != nil || !available || len(unchanged) != 0 {
				t.Fatalf("unchanged baseline diff: available=%v err=%v patch=%s", available, err, unchanged)
			}
			writeFile(t, path, "task raw bytes\n")
			if route == "preexisting-staged-then-removed" {
				runGit(t, repo, "add", "review.txt")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			patch, available, err := Capture(repo, st)
			if err != nil || !available {
				t.Fatalf("raw untracked task patch unavailable: available=%v err=%v", available, err)
			}
			for _, want := range []string{"diff --git ", "review.txt", "\n+task raw bytes\n"} {
				if !bytes.Contains(patch, []byte(want)) {
					t.Fatalf("%s lost raw task bytes %q: %s", route, want, patch)
				}
			}
			if route == "preexisting-modified" {
				if !bytes.Contains(patch, []byte("\n-preexisting untracked bytes\n")) {
					t.Fatalf("modified untracked patch lost captured baseline bytes: %s", patch)
				}
			} else if !bytes.Contains(patch, []byte("new file mode")) {
				t.Fatalf("added path is not represented as a standard new-file patch: %s", patch)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("%s executed an external diff/textconv driver: %v", route, err)
			}
			writeFile(t, filepath.Join(root, "control-before.txt"), "before control bytes\n")
			writeFile(t, filepath.Join(root, "control-after.txt"), "after control bytes\n")
			control, stderr, err := runGitCommand(root, nil, nil, "diff", "--no-index", "--no-ext-diff", "--textconv", "--", "control-before.txt", "control-after.txt")
			if err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
					t.Fatalf("textconv control failed outside the no-index exit protocol: err=%v stdout=%s stderr=%s", err, control, stderr)
				}
			}
			if len(control) != 0 {
				t.Fatalf("constant textconv did not hide distinct raw control bytes: stdout=%s stderr=%s", control, stderr)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("presentation control did not execute configured textconv: %v", err)
			}
		})
	}
}
