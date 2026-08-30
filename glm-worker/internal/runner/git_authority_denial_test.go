package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const mutationDenialJSON = `{"error":{"kind":"containment_denial","owner":"git-authority-guard","reason":"git_mutation_blocked","next_action":"continue_source_edits_or_read_only_git_parent_owns_git_mutation"}}`
const transportDenialJSON = `{"error":{"kind":"containment_denial","owner":"git-authority-guard","reason":"git_transport_blocked","next_action":"do_not_retry_transport_parent_owns_git_transport"}}`

func TestGitAuthorityProxyReturnsActionableMutationDenial(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-oriented")
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if output, err := exec.Command(realGit, "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	workerTemp := t.TempDir()
	attemptLog := filepath.Join(t.TempDir(), "blocked-attempts")
	if err := os.WriteFile(attemptLog, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	proxy := writeGitAuthorityTestScript(t, gitAuthorityProxyScript(realGit, attemptLog, repo, workerTemp))
	command := exec.Command(proxy, "-C", repo, "commit", "--allow-empty", "-m", "blocked")
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("blocked mutation unexpectedly succeeded")
	}
	if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 97 {
		t.Fatalf("exit = %v, output = %s", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != mutationDenialJSON {
		t.Fatalf("denial = %q", got)
	}
	attempts, err := os.ReadFile(attemptLog)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(attempts)); got != "commit" {
		t.Fatalf("attempt log = %q", got)
	}
}

func TestGitAuthorityProxyKeepsDenialWhenAttemptLogCannotBeWritten(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-oriented")
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if output, err := exec.Command(realGit, "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	workerTemp := t.TempDir()
	attemptLog := t.TempDir() // directory: append must fail, but the denial must remain authoritative.
	proxy := writeGitAuthorityTestScript(t, gitAuthorityProxyScript(realGit, attemptLog, repo, workerTemp))
	command := exec.Command(proxy, "-C", repo, "branch", "blocked")
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("blocked mutation unexpectedly succeeded")
	}
	if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 97 {
		t.Fatalf("exit = %v, output = %s", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != mutationDenialJSON {
		t.Fatalf("denial = %q", got)
	}
}

func TestGitAuthorityTransportDenialIsActionableWithoutAttemptLog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-oriented")
	}
	attemptLog := t.TempDir() // force diagnostic append failure
	deny := writeGitAuthorityTestScript(t, gitAuthorityDenyTransportScript(attemptLog))
	output, err := exec.Command(deny, "unused").CombinedOutput()
	if err == nil {
		t.Fatal("transport denial unexpectedly succeeded")
	}
	if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 97 {
		t.Fatalf("exit = %v, output = %s", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != transportDenialJSON {
		t.Fatalf("denial = %q", got)
	}
}

func writeGitAuthorityTestScript(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "guard")
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
