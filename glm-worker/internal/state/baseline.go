package state

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func CaptureGitBaseline(cfg config.AppConfig, state *StateStore) error {
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
			if err := state.Remove("baseline-status", "baseline-worktree.patch", "baseline-index.patch"); err != nil {
				return err
			}
			return nil
		}
		if err := state.Write(item.name, string(output)); err != nil {
			return err
		}
	}

	head, unborn, err := resolveRepoHead(cfg.RepoRoot)
	if err != nil {

		return state.Remove("baseline-head")
	}
	if unborn {
		return state.Remove("baseline-head")
	}
	return state.Write("baseline-head", head)
}

func resolveRepoHead(repoRoot string) (head string, unborn bool, err error) {
	if _, e := exec.Command("git", "-C", repoRoot, "rev-parse", "--git-dir").Output(); e != nil {
		return "", false, e
	}

	output, e := exec.Command("git", "-C", repoRoot, "rev-parse", "--verify", "-q", "HEAD^{commit}").Output()
	if e == nil {
		return strings.TrimSpace(string(output)), false, nil
	}
	target, e2 := exec.Command("git", "-C", repoRoot, "symbolic-ref", "-q", "HEAD").Output()
	if e2 != nil {
		return "", false, fmt.Errorf("HEAD does not peel to a commit and is not a valid symbolic ref: %w", e2)
	}
	ref := strings.TrimSpace(string(target))
	if !strings.HasPrefix(ref, "refs/heads/") {
		return "", false, fmt.Errorf("HEAD symbolic target %q is not under refs/heads", ref)
	}

	refs, fe := exec.Command("git", "-C", repoRoot, "for-each-ref", "--format=%(refname)", ref).Output()
	if fe != nil {
		return "", false, fmt.Errorf("ref lookup %s failed: %w", ref, fe)
	}
	if len(strings.TrimSpace(string(refs))) > 0 {
		return "", false, fmt.Errorf("HEAD symbolic target %s exists but does not peel to a commit", ref)
	}
	return "", true, nil
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
