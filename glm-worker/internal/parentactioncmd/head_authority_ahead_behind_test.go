package parentactioncmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type finalizationRemoteFixture struct {
	repo    string
	remote  string
	branch  string
	baseOID string
}

func TestGitAheadBehindUsesCapturedHeadOID(t *testing.T) {
	fixture := newFinalizationRemoteFixture(t)
	capturedHead := fixture.baseOID

	writeFinalizationRemoteFile(t, fixture.repo, "binding.txt", "base\nsecond\n")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-am", "second")
	currentHead := finalizationGitOutputForTest(t, fixture.repo, "rev-parse", "HEAD")

	ahead, behind, err := gitAheadBehind(fixture.repo, "refs/remotes/origin/"+fixture.branch, capturedHead)
	if err != nil {
		t.Fatal(err)
	}
	if ahead != 0 || behind != 0 {
		t.Fatalf("captured head comparison = ahead %d behind %d, want synced", ahead, behind)
	}

	ahead, behind, err = gitAheadBehind(fixture.repo, "refs/remotes/origin/"+fixture.branch, currentHead)
	if err != nil {
		t.Fatal(err)
	}
	if ahead != 1 || behind != 0 {
		t.Fatalf("current moved head comparison = ahead %d behind %d, want 1/0", ahead, behind)
	}
}

func TestFinalizationRemoteStateUsesCapturedHeadOID(t *testing.T) {
	fixture := newFinalizationRemoteFixture(t)
	capturedHead := fixture.baseOID

	writeFinalizationRemoteFile(t, fixture.repo, "binding.txt", "base\nsecond\n")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-am", "second")

	remote, remoteState := finalizationRemoteState(fixture.repo, fixture.branch, false, capturedHead)
	if remoteState != finalizationRemoteStateSynced {
		t.Fatalf("remote state = %s want %s", remoteState, finalizationRemoteStateSynced)
	}
	if remote == nil || remote.Ahead != 0 || remote.Behind != 0 || remote.TrackingOID != capturedHead {
		t.Fatalf("remote = %#v", remote)
	}
}

func newFinalizationRemoteFixture(t *testing.T) finalizationRemoteFixture {
	t.Helper()
	repo := t.TempDir()
	remote := filepath.Join(t.TempDir(), "remote.git")
	if err := os.MkdirAll(remote, 0o700); err != nil {
		t.Fatal(err)
	}
	runFinalizationGit(t, repo, "init", "-q")
	runFinalizationGit(t, repo, "config", "user.email", "finalization@example.invalid")
	runFinalizationGit(t, repo, "config", "user.name", "finalization test")
	runFinalizationGit(t, remote, "init", "-q", "--bare")
	writeFinalizationRemoteFile(t, repo, "binding.txt", "base\n")
	runFinalizationGit(t, repo, "add", "binding.txt")
	runFinalizationGit(t, repo, "commit", "-q", "-m", "initial")
	branch := finalizationGitOutputForTest(t, repo, "symbolic-ref", "--short", "HEAD")
	runFinalizationGit(t, repo, "remote", "add", "origin", remote)
	runFinalizationGit(t, repo, "push", "-q", "origin", branch)
	runFinalizationGit(t, repo, "branch", "--set-upstream-to=origin/"+branch, branch)
	return finalizationRemoteFixture{
		repo:    repo,
		remote:  remote,
		branch:  branch,
		baseOID: finalizationGitOutputForTest(t, repo, "rev-parse", "HEAD"),
	}
}

func writeFinalizationRemoteFile(t *testing.T, repo, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func finalizationGitOutputForTest(t *testing.T, repo string, args ...string) string {
	t.Helper()
	output, err := gitFinalizationOutput(repo, args...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(output)
}
