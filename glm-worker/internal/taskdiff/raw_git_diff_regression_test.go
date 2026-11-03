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

func TestMachineDiffUsesRawBytesDespiteExternalDiffAndTextconv(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	driver := filepath.Join(root, "driver.sh")
	marker := filepath.Join(root, "driver-executed")
	writeFile(t, driver, fmt.Sprintf("#!/bin/sh\nprintf invoked >> %q\nprintf constant\n", marker))
	if err := os.Chmod(driver, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "config", "diff.audit.textconv", driver)
	writeFile(t, filepath.Join(repo, ".gitattributes"), "tracked.txt diff=audit\n")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "before\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "baseline")
	cfg := config.AppConfig{RepoRoot: repo, RepoHash: strings.Repeat("a", 64),
		StateBase: filepath.Join(root, "state")}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	before, err := state.CaptureGitSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_EXTERNAL_DIFF", driver)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "after\n")
	after, err := state.CaptureGitSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	if before.WorktreeDigest == after.WorktreeDigest {
		t.Fatal("raw tracked bytes changed but snapshot worktree digest stayed unchanged")
	}
	patch, available, err := Capture(repo, st)
	if err != nil {
		t.Fatal(err)
	}
	if !available || !bytes.Contains(patch, []byte("+after")) || !bytes.Contains(patch, []byte("-before")) {
		t.Fatalf("changed bytes were not exported as a raw available patch: available=%v patch=%q", available, patch)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("external diff/textconv driver was executed: %v", err)
	}
}
