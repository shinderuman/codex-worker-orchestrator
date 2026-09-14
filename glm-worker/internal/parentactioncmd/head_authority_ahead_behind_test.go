package parentactioncmd

import (
	"strings"
	"testing"
)

func TestGitAheadBehindUsesCapturedHeadOID(t *testing.T) {
	fixture := newPushBindingFixture(t)
	capturedHead := fixture.baseOID

	writePushBindingFile(t, fixture.repo, "binding.txt", "base\nsecond\n")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-am", "second")
	currentHead := strings.TrimSpace(pushBindingGitOutput(t, fixture.repo, "rev-parse", "HEAD"))

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
	fixture := newPushBindingFixture(t)
	capturedHead := fixture.baseOID

	writePushBindingFile(t, fixture.repo, "binding.txt", "base\nsecond\n")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-am", "second")

	remote, remoteState := finalizationRemoteState(fixture.repo, fixture.branch, false, capturedHead)
	if remoteState != finalizationRemoteStateSynced {
		t.Fatalf("remote state = %s want %s", remoteState, finalizationRemoteStateSynced)
	}
	if remote == nil || remote.Ahead != 0 || remote.Behind != 0 || remote.TrackingOID != capturedHead {
		t.Fatalf("remote = %#v", remote)
	}
}
