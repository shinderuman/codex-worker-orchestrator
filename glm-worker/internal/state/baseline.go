package state

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

type GitBaselineEvidence struct {
	Head          string `json:"head,omitempty"`
	Status        string `json:"status,omitempty"`
	WorktreePatch string `json:"worktree_patch,omitempty"`
	IndexPatch    string `json:"index_patch,omitempty"`
	Untracked     string `json:"untracked,omitempty"`
}

type GitHeadAuthority struct {
	Head         string
	SymbolicHead string
	Unborn       bool
	Detached     bool
}

const baselineUntrackedFile = "baseline-untracked"

func CaptureGitBaseline(cfg config.AppConfig, state *StateStore) error {
	head, unborn, err := resolveRepoHead(cfg.RepoRoot)
	if err != nil {
		if cleanupErr := removeGitBaseline(state); cleanupErr != nil {
			return fmt.Errorf("git baseline HEAD resolution failed: %v; cleanup failed: %w", err, cleanupErr)
		}
		return fmt.Errorf("git baseline HEAD resolution failed: %w", err)
	}

	commands := []struct {
		name string
		args []string
	}{
		{name: "baseline-status", args: []string{"status", "--porcelain=v1", "--untracked-files=all"}},
		{name: "baseline-worktree.patch", args: []string{"diff", "--binary", "--no-ext-diff"}},
		{name: "baseline-index.patch", args: []string{"diff", "--cached", "--binary", "--no-ext-diff"}},
	}

	for _, item := range commands {
		command := exec.Command("git", item.args...)
		command.Dir = cfg.RepoRoot
		output, err := command.Output()
		if err != nil {
			if err := removeGitBaseline(state); err != nil {
				return err
			}
			return nil
		}
		if err := state.Write(item.name, string(output)); err != nil {
			return err
		}
	}
	untracked := exec.Command("git", "ls-files", "-z", "--others", "--exclude-standard")
	untracked.Dir = cfg.RepoRoot
	untrackedOutput, err := untracked.Output()
	if err != nil {
		if err := removeGitBaseline(state); err != nil {
			return err
		}
		return nil
	}
	if err := writeFileAtomic(state.Path(baselineUntrackedFile), untrackedOutput, 0o600); err != nil {
		return err
	}

	if unborn {
		return state.Remove("baseline-head")
	}
	return state.Write("baseline-head", head)
}

func removeGitBaseline(state *StateStore) error {
	return state.Remove("baseline-head", "baseline-status", "baseline-worktree.patch", "baseline-index.patch", baselineUntrackedFile)
}

func ResolveGitHeadAuthority(gitPath, repoRoot string) (GitHeadAuthority, error) {
	if _, err := exec.Command(gitPath, "-C", repoRoot, "rev-parse", "--git-dir").Output(); err != nil {
		return GitHeadAuthority{}, fmt.Errorf("git rev-parse --git-dir: %w", err)
	}

	headOutput, headErr := exec.Command(gitPath, "-C", repoRoot, "rev-parse", "--verify", "-q", "HEAD^{commit}").Output()
	if headErr == nil {
		head := strings.TrimSpace(string(headOutput))
		if head == "" {
			return GitHeadAuthority{}, fmt.Errorf("git rev-parse HEAD returned empty commit")
		}
		symbolicOutput, symbolicErr := exec.Command(gitPath, "-C", repoRoot, "symbolic-ref", "-q", "HEAD").Output()
		if symbolicErr == nil {
			return GitHeadAuthority{Head: head, SymbolicHead: strings.TrimSpace(string(symbolicOutput))}, nil
		}
		if gitExitCode(symbolicErr) == 1 {
			return GitHeadAuthority{Head: head, Detached: true}, nil
		}
		return GitHeadAuthority{}, fmt.Errorf("git symbolic-ref -q HEAD: %w", symbolicErr)
	}
	if gitExitCode(headErr) != 1 {
		return GitHeadAuthority{}, fmt.Errorf("git rev-parse --verify HEAD^{commit}: %w", headErr)
	}

	target, err := exec.Command(gitPath, "-C", repoRoot, "symbolic-ref", "-q", "HEAD").Output()
	if err != nil {
		return GitHeadAuthority{}, fmt.Errorf("HEAD does not peel to a commit and is not a valid symbolic ref: %w", err)
	}
	ref := strings.TrimSpace(string(target))
	if !strings.HasPrefix(ref, "refs/heads/") {
		return GitHeadAuthority{}, fmt.Errorf("HEAD symbolic target %q is not under refs/heads", ref)
	}

	refs, err := exec.Command(gitPath, "-C", repoRoot, "for-each-ref", "--format=%(refname)", ref).Output()
	if err != nil {
		return GitHeadAuthority{}, fmt.Errorf("ref lookup %s failed: %w", ref, err)
	}
	if len(strings.TrimSpace(string(refs))) > 0 {
		return GitHeadAuthority{}, fmt.Errorf("HEAD symbolic target %s exists but does not peel to a commit", ref)
	}
	return GitHeadAuthority{SymbolicHead: ref, Unborn: true}, nil
}

func gitExitCode(err error) int {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return -1
	}
	return exitErr.ExitCode()
}

func resolveRepoHead(repoRoot string) (head string, unborn bool, err error) {
	authority, err := ResolveGitHeadAuthority("git", repoRoot)
	if err != nil {
		return "", false, err
	}
	return authority.Head, authority.Unborn, nil
}

func (s *StateStore) BaselineEvidence() *GitBaselineEvidence {
	evidence := GitBaselineEvidence{Head: s.ReadOr("baseline-head", "")}
	if s.Exists("baseline-status") {
		evidence.Status = s.Path("baseline-status")
	}
	if s.Exists("baseline-worktree.patch") {
		evidence.WorktreePatch = s.Path("baseline-worktree.patch")
	}
	if s.Exists("baseline-index.patch") {
		evidence.IndexPatch = s.Path("baseline-index.patch")
	}
	if s.Exists(baselineUntrackedFile) {
		evidence.Untracked = s.Path(baselineUntrackedFile)
	}
	if evidence.Head == "" && evidence.Status == "" && evidence.WorktreePatch == "" && evidence.IndexPatch == "" && evidence.Untracked == "" {
		return nil
	}
	return &evidence
}

func (s *StateStore) BaselineDescription() string {
	if !s.Exists("baseline-status") {
		return "none"
	}

	return fmt.Sprintf(
		"status=%s\nworktree_diff=%s\nstaged_diff=%s",
		s.Path("baseline-status"),
		s.Path("baseline-worktree.patch"),
		s.Path("baseline-index.patch"),
	)
}
