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
	writePushBindingFile(t, fixture.repo, "IMPLEMENTATION_PLAN.local.md", pushAuthorizationInitialPlan(fixture.branch))
	writePushBindingFile(t, fixture.repo, "IMPLEMENTATION_TASKS/active.md", "# active\n\n## External feasibility\n\nstatus: not-applicable\n")
	writePushBindingFile(t, fixture.repo, "IMPLEMENTATION_TASKS/next.md", "# next\n\n## External feasibility\n\nstatus: not-applicable\n")
	writePushBindingFile(t, fixture.repo, "binding.txt", "base\nsecond\n")
	runFinalizationGit(t, fixture.repo, "add", "-A")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-m", "implementation")

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
	if err := st.Write("active-task", "IMPLEMENTATION_TASKS/active.md"); err != nil {
		t.Fatal(err)
	}

	beforeReview := runPushBindingAuthorizationFixture(t, fixture.repo)
	if beforeReview.Status != completePushStatusBlocked || beforeReview.RemoteWrite != nil || beforeReview.Failure == nil ||
		beforeReview.Failure.Reason != pushBindingFailureParentCompletionNotReady {
		t.Fatalf("pre-review push authorization = %#v", beforeReview)
	}

	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	beforeMetadata := runPushBindingAuthorizationFixture(t, fixture.repo)
	if beforeMetadata.Status != completePushStatusBlocked || beforeMetadata.RemoteWrite != nil || beforeMetadata.Failure == nil ||
		beforeMetadata.Failure.Reason != "completed_task_file_still_tracked" {
		t.Fatalf("pre-metadata push authorization = %#v", beforeMetadata)
	}

	runFinalizationGit(t, fixture.repo, "rm", "-q", "IMPLEMENTATION_TASKS/active.md")
	writePushBindingFile(t, fixture.repo, "IMPLEMENTATION_PLAN.local.md", pushAuthorizationPromotedPlan(fixture.branch))
	runFinalizationGit(t, fixture.repo, "add", "-A")
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

func pushAuthorizationInitialPlan(branch string) string {
	return "# plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/active.md`\n\n" +
		"## NEXT（優先順）\n\n- `IMPLEMENTATION_TASKS/next.md`\n\n" +
		"## BLOCKED / USER_PERMISSION_WAIT\n\n" +
		"## 現在のGit境界\n\n- branch: `" + branch + "`\n\n" +
		"## 現在の停止理由\n\nなし\n"
}

func pushAuthorizationPromotedPlan(branch string) string {
	return "# plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/next.md`\n\n" +
		"## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n\n" +
		"## 現在のGit境界\n\n- branch: `" + branch + "`\n\n" +
		"## 現在の停止理由\n\nなし\n"
}
