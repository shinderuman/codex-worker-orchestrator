package parentactioncmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPreparePublicationCandidateLeavesBranchHeadUnchanged(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	baseHead := publicationGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, installScriptName), []byte("#!/bin/sh\nexit 0\n# candidate\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	publicationGit(t, cfg.RepoRoot, "add", installScriptName)

	candidate, failure := preparePublicationCandidate(cfg, st, "candidate publication")
	if failure != nil {
		t.Fatalf("prepare failed: %#v", failure)
	}
	if got := publicationGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD"); got != baseHead {
		t.Fatalf("prepare advanced branch HEAD: want %s got %s", baseHead, got)
	}
	if candidate.BaseHead != baseHead {
		t.Fatalf("candidate base = %s, want %s", candidate.BaseHead, baseHead)
	}
	if got := publicationGitOutput(t, cfg.RepoRoot, "rev-parse", candidate.CommitOID+"^"); got != baseHead {
		t.Fatalf("candidate parent = %s, want %s", got, baseHead)
	}
	if got := publicationGitOutput(t, cfg.RepoRoot, "rev-parse", candidate.CommitOID+"^{tree}"); got != candidate.TreeOID {
		t.Fatalf("candidate tree = %s, want %s", got, candidate.TreeOID)
	}
	if got := publicationGitOutput(t, cfg.RepoRoot, "write-tree"); got != candidate.TreeOID {
		t.Fatalf("index tree = %s, want %s", got, candidate.TreeOID)
	}

	repeated, repeatedFailure := preparePublicationCandidate(cfg, st, "candidate publication")
	if repeatedFailure != nil {
		t.Fatalf("retry failed: %#v", repeatedFailure)
	}
	if repeated.CommitOID != candidate.CommitOID {
		t.Fatalf("retry created a different candidate: %s != %s", repeated.CommitOID, candidate.CommitOID)
	}
}

func TestPreparePublicationCandidateRejectsUnstagedAndUntrackedSource(t *testing.T) {
	t.Run("unstaged", func(t *testing.T) {
		cfg, st := newInstallActionRepo(t)
		if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfg.RepoRoot, installScriptName), []byte("#!/bin/sh\nexit 0\n# unstaged\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		_, failure := preparePublicationCandidate(cfg, st, "candidate publication")
		if failure == nil || failure.Reason != publicationFailureSourceDirty {
			t.Fatalf("unstaged source was accepted: %#v", failure)
		}
	})

	t.Run("untracked", func(t *testing.T) {
		cfg, st := newInstallActionRepo(t)
		if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfg.RepoRoot, "candidate.txt"), []byte("candidate\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, failure := preparePublicationCandidate(cfg, st, "candidate publication")
		if failure == nil || failure.Reason != publicationFailureSourceUntracked {
			t.Fatalf("untracked source was accepted: %#v", failure)
		}
	})
}

func publicationGit(t *testing.T, repoRoot string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func publicationGitOutput(t *testing.T, repoRoot string, args ...string) string {
	t.Helper()
	output, err := exec.Command("git", append([]string{"-C", repoRoot}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(output))
}
