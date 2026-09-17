package parentactioncmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestContinuationStopHookSmoke(t *testing.T) {
	hook := continuationHookPath(t, ".codex", "hooks", "continuation-stop-hook.sh")

	t.Run("delegates to canonical action", func(t *testing.T) {
		bin, calls := continuationHookBin(t, true)
		cmd := exec.Command("sh", hook)
		cmd.Env = continuationHookEnv(bin, calls)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("stop hook failed: %v: %s", err, output)
		}
		if got := strings.TrimSpace(readContinuationHookFile(t, calls)); got != "continuation-stop-hook" {
			t.Fatalf("canonical action = %q, want continuation-stop-hook", got)
		}
	})

	t.Run("fails closed when canonical action unavailable", func(t *testing.T) {
		bin, calls := continuationHookBin(t, false)
		cmd := exec.Command("sh", hook)
		cmd.Env = continuationHookEnv(bin, calls)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("stop hook should block with successful hook exit: %v: %s", err, output)
		}
		if !strings.Contains(string(output), `"decision":"block"`) {
			t.Fatalf("stop hook output = %q, want block decision", output)
		}
	})
}

func TestContinuationMetadataPreCommitHookSmoke(t *testing.T) {
	hook := continuationHookPath(t, ".githooks", "pre-commit")

	t.Run("ignores non metadata commits", func(t *testing.T) {
		repo := continuationHookRepo(t)
		bin, calls := continuationHookBin(t, false)
		if err := os.WriteFile(filepath.Join(repo, "code.txt"), []byte("change\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runContinuationHookGit(t, repo, "add", "code.txt")
		cmd := exec.Command("sh", hook)
		cmd.Dir = repo
		cmd.Env = continuationHookEnv(bin, calls)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("pre-commit rejected non-metadata commit: %v: %s", err, output)
		}
	})

	t.Run("delegates clean staged metadata", func(t *testing.T) {
		repo := continuationHookRepo(t)
		bin, calls := continuationHookBin(t, true)
		plan := filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md")
		if err := os.WriteFile(plan, []byte("# Plan\n\nchanged\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runContinuationHookGit(t, repo, "add", "IMPLEMENTATION_PLAN.local.md")
		cmd := exec.Command("sh", hook)
		cmd.Dir = repo
		cmd.Env = continuationHookEnv(bin, calls)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("pre-commit failed: %v: %s", err, output)
		}
		if got := strings.TrimSpace(readContinuationHookFile(t, calls)); got != "continuation-metadata-guard" {
			t.Fatalf("canonical action = %q, want continuation-metadata-guard", got)
		}
	})

	t.Run("rejects staged worktree divergence", func(t *testing.T) {
		repo := continuationHookRepo(t)
		bin, calls := continuationHookBin(t, true)
		plan := filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md")
		if err := os.WriteFile(plan, []byte("# Plan\n\nstaged\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runContinuationHookGit(t, repo, "add", "IMPLEMENTATION_PLAN.local.md")
		if err := os.WriteFile(plan, []byte("# Plan\n\nunstaged\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("sh", hook)
		cmd.Dir = repo
		cmd.Env = continuationHookEnv(bin, calls)
		output, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("pre-commit accepted staged/worktree divergence: %s", output)
		}
		if !strings.Contains(string(output), "differs from worktree") {
			t.Fatalf("pre-commit output = %q, want divergence reason", output)
		}
	})
}

func continuationHookPath(t *testing.T, elements ...string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	parts := append([]string{wd, "..", "..", ".."}, elements...)
	path := filepath.Clean(filepath.Join(parts...))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("hook path %s: %v", path, err)
	}
	return path
}

func continuationHookRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runContinuationHookGit(t, repo, "init", "-q")
	runContinuationHookGit(t, repo, "config", "user.email", "test@example.com")
	runContinuationHookGit(t, repo, "config", "user.name", "Test")
	if err := os.MkdirAll(filepath.Join(repo, "IMPLEMENTATION_TASKS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte("# Plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_TASKS", "current.md"), []byte("# Task\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runContinuationHookGit(t, repo, "add", "IMPLEMENTATION_PLAN.local.md", "IMPLEMENTATION_TASKS/current.md")
	runContinuationHookGit(t, repo, "commit", "-q", "-m", "initial")
	return repo
}

func continuationHookBin(t *testing.T, withAction bool) (string, string) {
	t.Helper()
	bin := t.TempDir()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(gitPath, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(bin, "calls")
	if withAction {
		stub := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$HOOK_CALLS\"\n"
		if err := os.WriteFile(filepath.Join(bin, "glm-parent-action"), []byte(stub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return bin, calls
}

func continuationHookEnv(bin, calls string) []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "PATH=") || strings.HasPrefix(item, "HOOK_CALLS=") {
			continue
		}
		env = append(env, item)
	}
	return append(env, "PATH="+bin, "HOOK_CALLS="+calls)
}

func runContinuationHookGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, output)
	}
}

func readContinuationHookFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
