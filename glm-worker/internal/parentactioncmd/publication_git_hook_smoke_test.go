package parentactioncmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicationReferenceTransactionRejectsCommitNoVerify(t *testing.T) {
	repo := continuationHookRepo(t)
	hooks := t.TempDir()
	copyPublicationHook(t, "reference-transaction", hooks)
	runContinuationHookGit(t, repo, "config", "core.hooksPath", hooks)

	bin, calls := publicationGuardStub(t, false)
	if err := os.WriteFile(filepath.Join(repo, "code.txt"), []byte("blocked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runContinuationHookGit(t, repo, "add", "code.txt")
	before := strings.TrimSpace(pushBindingGitOutput(t, repo, "rev-parse", "HEAD"))
	cmd := exec.Command("git", "commit", "--no-verify", "-m", "must be blocked")
	cmd.Dir = repo
	cmd.Env = publicationHookEnv(bin, calls)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("git commit --no-verify bypassed reference transaction guard: %s", output)
	}
	after := strings.TrimSpace(pushBindingGitOutput(t, repo, "rev-parse", "HEAD"))
	if after != before {
		t.Fatalf("blocked commit advanced HEAD: %s != %s", after, before)
	}
	callData, readErr := os.ReadFile(calls)
	if readErr != nil || !strings.Contains(string(callData), "push-binding ref-guard --old") {
		t.Fatalf("reference guard call = %q err=%v", callData, readErr)
	}
}

func TestPublicationPrePushDelegatesAndFailsClosed(t *testing.T) {
	hook := continuationHookPath(t, ".githooks", "pre-push")
	oldOID := strings.Repeat("1", 40)
	newOID := strings.Repeat("2", 40)
	input := "refs/heads/main " + newOID + " refs/heads/main " + oldOID + "\n"

	t.Run("delegates", func(t *testing.T) {
		bin, calls := publicationGuardStub(t, true)
		cmd := exec.Command("sh", hook, "origin", "unused")
		cmd.Stdin = strings.NewReader(input)
		cmd.Env = publicationHookEnv(bin, calls)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("pre-push failed: %v: %s", err, output)
		}
		data, err := os.ReadFile(calls)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range []string{"push-binding push-guard", "--remote-name origin", "--local-oid " + newOID, "--remote-oid " + oldOID} {
			if !bytes.Contains(data, []byte(value)) {
				t.Fatalf("pre-push call %q lacks %q", data, value)
			}
		}
	})

	t.Run("fails-closed", func(t *testing.T) {
		bin, _ := publicationGuardStub(t, false)
		cmd := exec.Command("sh", hook, "origin", "unused")
		cmd.Stdin = strings.NewReader(input)
		cmd.Env = publicationHookEnv(bin, filepath.Join(t.TempDir(), "calls"))
		if output, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(output), "publication push rejected") {
			t.Fatalf("pre-push did not fail closed: err=%v output=%s", err, output)
		}
	})
}

func copyPublicationHook(t *testing.T, name, destination string) {
	t.Helper()
	source := continuationHookPath(t, ".githooks", name)
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, name), data, 0o755); err != nil {
		t.Fatal(err)
	}
}

func publicationGuardStub(t *testing.T, allow bool) (string, string) {
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
	exitCode := "1"
	if allow {
		exitCode = "0"
	}
	stub := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$HOOK_CALLS\"\nexit " + exitCode + "\n"
	if err := os.WriteFile(filepath.Join(bin, "glm-parent-action"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, calls
}

func publicationHookEnv(bin, calls string) []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "PATH=") || strings.HasPrefix(item, "HOOK_CALLS=") {
			continue
		}
		env = append(env, item)
	}
	return append(env, "PATH="+bin, "HOOK_CALLS="+calls)
}
