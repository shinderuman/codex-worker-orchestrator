package repositoryprojecthead

import (
	"os/exec"
	"strings"
	"testing"
)

func TestGitOutputKeepsSuccessfulDiagnosticsOutOfStdout(t *testing.T) {
	root := t.TempDir()
	command := exec.Command("git", "init", "-q")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	t.Setenv("GIT_TRACE", "1")

	output, err := gitOutput(root, "rev-parse", "--git-dir")
	if err != nil {
		t.Fatal(err)
	}
	if output != ".git" {
		t.Fatalf("git stdout = %q want .git", output)
	}
	if strings.Contains(output, "trace:") {
		t.Fatalf("git diagnostics leaked into stdout: %q", output)
	}
}

func TestGitOutputKeepsFailureDiagnosticsInError(t *testing.T) {
	root := t.TempDir()
	command := exec.Command("git", "init", "-q")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	t.Setenv("GIT_TRACE", "1")

	if _, err := gitOutput(root, "show", "missing-ref"); err == nil {
		t.Fatal("git show unexpectedly succeeded")
	} else if !strings.Contains(err.Error(), "trace:") {
		t.Fatalf("git stderr diagnostics missing from error: %v", err)
	}
}
