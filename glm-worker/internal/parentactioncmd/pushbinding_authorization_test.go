package parentactioncmd

import (
	"bytes"
	"os"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPushBindingRemoteWriteRequiresParentCompletionBoundary(t *testing.T) {
	fixture := newPushBindingFixture(t)
	writePushBindingFile(t, fixture.repo, "binding.txt", "base\nsecond\n")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-am", "implementation")

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(fixture.repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })
	t.Setenv("GLM_WORKER_HOME", t.TempDir())

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("active-task", "binding.txt"); err != nil {
		t.Fatal(err)
	}

	beforeReview := runPushBindingAuthorizationFixture(t, fixture.repo)
	if beforeReview.Status != "blocked" || beforeReview.RemoteWrite != nil || beforeReview.Failure == nil ||
		beforeReview.Failure.Reason != pushBindingFailureParentCompletionNotReady {
		t.Fatalf("pre-review push authorization = %#v", beforeReview)
	}

	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	beforeMetadata := runPushBindingAuthorizationFixture(t, fixture.repo)
	if beforeMetadata.Status != "blocked" || beforeMetadata.RemoteWrite != nil || beforeMetadata.Failure == nil ||
		beforeMetadata.Failure.Reason != "completed_task_file_still_tracked" {
		t.Fatalf("pre-metadata push authorization = %#v", beforeMetadata)
	}

	runFinalizationGit(t, fixture.repo, "rm", "-q", "binding.txt")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-m", "completion metadata")
	authorized := runPushBindingAuthorizationFixture(t, fixture.repo)
	if authorized.Status != "classified" || authorized.Classification != pushBindingClassificationLocalAhead ||
		authorized.RemoteWrite == nil || authorized.RemoteWrite.Authorization != pushBindingAuthorizationStanding ||
		authorized.RemoteWrite.Executor != pushBindingExecutorParentOnly {
		t.Fatalf("post-review push authorization = %#v", authorized)
	}
}

func runPushBindingAuthorizationFixture(t *testing.T, repoRoot string) pushBindingOutput {
	t.Helper()
	var output bytes.Buffer
	if err := runPushBinding(repoRoot, nil, &output); err != nil {
		t.Fatal(err)
	}
	return decodePushBindingOutput(t, output)
}
