package parentactioncmd

import (
	"os"
	"testing"
)

func TestPushBindingPreservesRemoteWriteOutsideManagedTask(t *testing.T) {
	fixture := newPushBindingFixture(t)
	writePushBindingFile(t, fixture.repo, "binding.txt", "base\nsecond\n")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-am", "manual change")

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(fixture.repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })
	t.Setenv("GLM_WORKER_HOME", t.TempDir())

	output := runPushBindingAuthorizationFixture(t, fixture.repo)
	if output.Status != "classified" || output.RemoteWrite == nil ||
		output.RemoteWrite.Authorization != pushBindingAuthorizationStanding {
		t.Fatalf("unmanaged push authorization = %#v", output)
	}
}
