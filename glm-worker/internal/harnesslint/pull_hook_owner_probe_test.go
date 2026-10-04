package harnesslint

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPullHookOwnerLookupFailsClosedWhenIdentityProbeFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hook installer is not exercised on Windows")
	}
	repo := t.TempDir()
	runHookOwnerGit(t, repo, "init", "-q")
	runHookOwnerGit(t, repo, "config", "user.email", "hook@example.invalid")
	runHookOwnerGit(t, repo, "config", "user.name", "hook test")
	for _, hook := range []string{"post-merge", "pre-commit", "reference-transaction", "pre-push"} {
		path := filepath.Join(repo, ".githooks", hook)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runHookOwnerGit(t, repo, "add", ".githooks")
	runHookOwnerGit(t, repo, "commit", "-qm", "hooks")

	guard := filepath.Join(t.TempDir(), "glm-parent-action")
	if err := os.WriteFile(guard, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	lockRoot := filepath.Join(repo, ".git", "codex-worker-orchestrator", "hooks-install.lock")
	ticket := filepath.Join(lockRoot, "424242")
	if err := os.MkdirAll(ticket, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ticket, "started"), []byte("identity\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	realPS, err := exec.LookPath("ps")
	if err != nil {
		t.Fatal(err)
	}
	fakeBin := t.TempDir()
	fakePS := filepath.Join(fakeBin, "ps")
	fakeScript := `#!/bin/sh
if [ "$1" = -p ] && [ "$2" = 424242 ] && [ "$3" = -o ]; then
	case "$4" in
	pid=) printf '%s\n' 424242; exit 0 ;;
	lstart=) exit 1 ;;
	esac
fi
exec "$REAL_PS" "$@"
`
	if err := os.WriteFile(fakePS, []byte(fakeScript), 0o755); err != nil {
		t.Fatal(err)
	}

	script, err := filepath.Abs("../../../scripts/manage-pull-hook.sh")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh", script, "install", repo, guard)
	command.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"), "REAL_PS="+realPS)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("ambiguous live owner identity unexpectedly allowed takeover: %s", output)
	}
	if !strings.Contains(string(output), "cannot verify hook activation owner identity") {
		t.Fatalf("unexpected owner lookup failure: %s", output)
	}
	if _, err := os.Stat(filepath.Join(ticket, "started")); err != nil {
		t.Fatalf("ambiguous owner ticket was modified: %v", err)
	}
}

func runHookOwnerGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
}
