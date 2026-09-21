package parentactioncmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPublicationReferenceTransactionAllowsOrdinaryCommit(t *testing.T) {
	repo := continuationHookRepo(t)
	hooks := t.TempDir()
	copyPublicationHook(t, "reference-transaction", hooks)
	runContinuationHookGit(t, repo, "config", "core.hooksPath", hooks)

	bin, calls := publicationGuardStub(t, true)
	if err := os.WriteFile(filepath.Join(repo, "code.txt"), []byte("ordinary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runContinuationHookGit(t, repo, "add", "code.txt")
	before := strings.TrimSpace(pushBindingGitOutput(t, repo, "rev-parse", "HEAD"))
	cmd := exec.Command("git", "commit", "--no-verify", "-m", "ordinary local commit")
	cmd.Dir = repo
	cmd.Env = publicationHookEnv(bin, calls)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ordinary commit rejected by reference transaction hook: %v: %s", err, output)
	}
	after := strings.TrimSpace(pushBindingGitOutput(t, repo, "rev-parse", "HEAD"))
	if after == before {
		t.Fatal("ordinary commit did not advance HEAD")
	}
	callData, readErr := os.ReadFile(calls)
	if readErr != nil || !strings.Contains(string(callData), "authority= push-binding ref-guard --old") {
		t.Fatalf("reference guard call = %q err=%v", callData, readErr)
	}
}

func TestPublicationReferenceTransactionAllowsOrdinaryFastForward(t *testing.T) {
	repo := continuationHookRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "fast-forward.txt"), []byte("ahead\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runContinuationHookGit(t, repo, "add", "fast-forward.txt")
	runContinuationHookGit(t, repo, "commit", "-m", "ahead")
	ahead := strings.TrimSpace(pushBindingGitOutput(t, repo, "rev-parse", "HEAD"))
	runContinuationHookGit(t, repo, "reset", "--hard", "HEAD^")

	hooks := t.TempDir()
	copyPublicationHook(t, "reference-transaction", hooks)
	runContinuationHookGit(t, repo, "config", "core.hooksPath", hooks)
	bin, calls := publicationGuardStub(t, true)
	cmd := exec.Command("git", "merge", "--ff-only", ahead)
	cmd.Dir = repo
	cmd.Env = publicationHookEnv(bin, calls)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ordinary fast-forward rejected by reference transaction hook: %v: %s", err, output)
	}
	if got := strings.TrimSpace(pushBindingGitOutput(t, repo, "rev-parse", "HEAD")); got != ahead {
		t.Fatalf("fast-forward HEAD = %s, want %s", got, ahead)
	}
	callData, readErr := os.ReadFile(calls)
	if readErr != nil || !strings.Contains(string(callData), "authority= push-binding ref-guard --old") {
		t.Fatalf("reference guard call = %q err=%v", callData, readErr)
	}
}

func TestPublicationReferenceTransactionCarriesOwnerAuthority(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	writePushBindingFile(t, cfg.RepoRoot, "README.md", "guard candidate\n")
	publicationGit(t, cfg.RepoRoot, "add", "README.md")
	candidate, failure := preparePublicationCandidate(cfg, st, "guard publication")
	if failure != nil {
		t.Fatalf("prepare failed: %#v", failure)
	}
	branchRef, _, headFailure := publicationPromotionHead(cfg.RepoRoot)
	if headFailure != nil {
		t.Fatalf("head = %#v", headFailure)
	}

	hooks := t.TempDir()
	copyPublicationHook(t, "reference-transaction", hooks)
	runContinuationHookGit(t, cfg.RepoRoot, "config", "core.hooksPath", hooks)
	bin, calls := publicationGuardStub(t, true)
	t.Setenv("PATH", bin)
	t.Setenv("HOOK_CALLS", calls)
	if err := updatePublicationRef(cfg.RepoRoot, candidate, branchRef, candidate.CommitOID, candidate.BaseHead); err != nil {
		t.Fatalf("publication ref update failed: %v", err)
	}
	callData, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	want := "authority=" + candidate.SnapshotID + " push-binding ref-guard --old"
	if !strings.Contains(string(callData), want) {
		t.Fatalf("reference guard call = %q, want %q", callData, want)
	}
}

func TestPublicationReferenceTransactionFailsClosedWhenGuardRejects(t *testing.T) {
	repo := continuationHookRepo(t)
	hooks := t.TempDir()
	copyPublicationHook(t, "reference-transaction", hooks)
	runContinuationHookGit(t, repo, "config", "core.hooksPath", hooks)

	bin, calls := publicationGuardStub(t, false)
	if err := os.WriteFile(filepath.Join(repo, "blocked.txt"), []byte("blocked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runContinuationHookGit(t, repo, "add", "blocked.txt")
	before := strings.TrimSpace(pushBindingGitOutput(t, repo, "rev-parse", "HEAD"))
	cmd := exec.Command("git", "commit", "--no-verify", "-m", "guard rejection")
	cmd.Dir = repo
	cmd.Env = publicationHookEnv(bin, calls)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("reference transaction hook ignored guard rejection: %s", output)
	}
	after := strings.TrimSpace(pushBindingGitOutput(t, repo, "rev-parse", "HEAD"))
	if after != before {
		t.Fatalf("blocked commit advanced HEAD: %s != %s", after, before)
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
	stub := "#!/bin/sh\nprintf 'authority=%s %s\\n' \"${" + publicationRefTransactionEnv + ":-}\" \"$*\" >> \"$HOOK_CALLS\"\nexit " + exitCode + "\n"
	if err := os.WriteFile(filepath.Join(bin, "glm-parent-action"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, calls
}

func publicationHookEnv(bin, calls string) []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "PATH=") || strings.HasPrefix(item, "HOOK_CALLS=") || strings.HasPrefix(item, publicationRefTransactionEnv+"=") {
			continue
		}
		env = append(env, item)
	}
	return append(env, "PATH="+bin, "HOOK_CALLS="+calls)
}
