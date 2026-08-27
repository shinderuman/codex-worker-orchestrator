package runner

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestGitAuthorityGuardBlocksModelGitMutationsBeforeExecution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-oriented")
	}
	cases := []struct {
		name    string
		command string
		want    string
		dirty   bool
	}{
		{name: "commit", command: "git commit --allow-empty -m blocked", want: "command:commit"},
		{name: "branch", command: "git branch blocked", want: "command:branch"},
		{name: "reset", command: "git reset --hard HEAD", want: "command:reset"},
		{name: "checkout destruction", command: "git checkout -- tracked.txt", want: "command:checkout", dirty: true},
		{name: "push", command: "git push origin HEAD", want: "command:push"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newGitAuthorityRepo(t)
			if tc.dirty {
				writeGitAuthorityFile(t, root, "tracked.txt", "dirty\n")
			}
			guarded, st := newGitAuthorityProductionRunner(t, root, tc.command, nil)
			_, err := guarded.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt", filepath.Join(t.TempDir(), "output"))
			var guardErr *GitAuthorityGuardError
			if !errors.As(err, &guardErr) || guardErr.Stage != "blocked-command" {
				t.Fatalf("guard error = %#v", err)
			}
			if !containsGitAuthorityMutation(guardErr.Mutations, tc.want) {
				t.Fatalf("mutations = %v want %s", guardErr.Mutations, tc.want)
			}
			if st.Exists("worker.id") || st.Exists("worker.ready") || st.Exists("reviewer.id") || st.Exists("reviewer.ready") {
				t.Fatal("blocked git mutation left reusable model session")
			}
			if tc.dirty {
				if got := readGitAuthorityFile(t, root, "tracked.txt"); got != "dirty\n" {
					t.Fatalf("checkout attempt changed worktree = %q", got)
				}
			}
		})
	}
}

func TestGitAuthorityGuardAllowsSourceEditAndReadOnlyGit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-oriented")
	}
	root := newGitAuthorityRepo(t)
	command := "printf '%s\\n' changed >\"$GLM_REPO/tracked.txt\"; git status --short >/dev/null"
	guarded, _ := newGitAuthorityProductionRunner(t, root, command, nil)
	if _, err := guarded.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt", filepath.Join(t.TempDir(), "output")); err != nil {
		t.Fatalf("normal source edit/read-only git failed: %v", err)
	}
	if got := readGitAuthorityFile(t, root, "tracked.txt"); got != "changed\n" {
		t.Fatalf("source edit = %q", got)
	}
}

func TestGitAuthorityGuardDetectsProxyBypassRefMutation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-oriented")
	}
	root := newGitAuthorityRepo(t)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	guarded, _ := newGitAuthorityProductionRunner(t, root, "\"$REAL_GIT\" branch bypass", map[string]string{"REAL_GIT": realGit})
	_, err = guarded.Run(state.ReviewerRole, "reviewer-1", "reviewer-model", false, "high", "prompt", filepath.Join(t.TempDir(), "output"))
	var guardErr *GitAuthorityGuardError
	if !errors.As(err, &guardErr) || guardErr.Stage != "after-call-mutation" {
		t.Fatalf("guard error = %#v", err)
	}
	if !containsGitAuthorityMutation(guardErr.Mutations, "refs") {
		t.Fatalf("mutations = %v", guardErr.Mutations)
	}
}

func TestGitAuthorityGuardLeavesParentCommitAndPushAuthorityUntouched(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-oriented")
	}
	root := newGitAuthorityRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGitAuthorityCommand(t, "", "init", "--bare", "-q", remote)
	runGitAuthorityCommand(t, root, "remote", "add", "origin", remote)
	guarded, _ := newGitAuthorityProductionRunner(t, root, "printf '%s\\n' parent-next >\"$GLM_REPO/tracked.txt\"", nil)
	if _, err := guarded.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt", filepath.Join(t.TempDir(), "output")); err != nil {
		t.Fatal(err)
	}
	runGitAuthorityCommand(t, root, "add", "tracked.txt")
	runGitAuthorityCommand(t, root, "commit", "-q", "-m", "parent commit")
	runGitAuthorityCommand(t, root, "push", "-q", "origin", "HEAD:refs/heads/main")
	if got := strings.TrimSpace(runGitAuthorityOutput(t, remote, "rev-parse", "refs/heads/main")); got == "" {
		t.Fatal("parent push did not create remote main")
	}
}

func newGitAuthorityRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGitAuthorityCommand(t, "", "init", "-q", root)
	runGitAuthorityCommand(t, root, "config", "user.email", "git-authority@example.invalid")
	runGitAuthorityCommand(t, root, "config", "user.name", "git authority test")
	writeGitAuthorityFile(t, root, "tracked.txt", "base\n")
	runGitAuthorityCommand(t, root, "add", "tracked.txt")
	runGitAuthorityCommand(t, root, "commit", "-q", "-m", "initial")
	return root
}

func newGitAuthorityProductionRunner(t *testing.T, root, modelCommand string, extraEnv map[string]string) (*InstructionSurfaceGuardRunner, *state.StateStore) {
	t.Helper()
	promptDir := t.TempDir()
	writeGitAuthorityFile(t, promptDir, "WORKER.md", "system")
	writeGitAuthorityFile(t, promptDir, "REVIEWER.md", "system")
	commandPath := filepath.Join(t.TempDir(), "fake-claude")
	script := "#!/bin/sh\ncd \"$GLM_REPO\"\n" + modelCommand + "\nprintf '%s\\n' '" + gitAuthorityResultJSON() + "'\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GLM_REPO", root)
	allow := []string{"GLM_REPO"}
	for key, value := range extraEnv {
		t.Setenv(key, value)
		allow = append(allow, key)
	}
	st := newTestStateStore(t)
	if err := st.Write("task.id", "task-git-authority"); err != nil {
		t.Fatal(err)
	}
	base := NewClaudeRunner(config.AppConfig{
		RepoRoot:        root,
		RepoShort:       "gitguard",
		PromptDir:       promptDir,
		ClaudeBin:       commandPath,
		ClaudeConfigDir: t.TempDir(),
		EnvAllowlist:    allow,
	}, st)
	return NewInstructionSurfaceGuardRunner(base), st
}

func gitAuthorityResultJSON() string {
	return `{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"structured_output\":{\"status\":\"IMPLEMENTED\",\"risk\":\"LOW\",\"summary\":\"done\",\"requirement_coverage\":\"covered\",\"tests\":\"pass\",\"unverified\":\"none\"},\"result\":\"runner output\"}`
}

func containsGitAuthorityMutation(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func runGitAuthorityCommand(t *testing.T, root string, args ...string) {
	t.Helper()
	commandArgs := args
	if root != "" {
		commandArgs = append([]string{"-C", root}, args...)
	}
	command := exec.Command("git", commandArgs...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(commandArgs, " "), err, strings.TrimSpace(string(output)))
	}
}

func runGitAuthorityOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", root}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(commandArgs, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output)
}

func writeGitAuthorityFile(t *testing.T, root, relative, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readGitAuthorityFile(t *testing.T, root, relative string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
