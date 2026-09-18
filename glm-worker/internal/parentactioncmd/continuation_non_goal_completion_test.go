package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskview"
)

func TestNonGoalOrdinaryCompletionCommitsAndCompletesInOrder(t *testing.T) {
	fixture := newNonGoalCompletionFixture(t, completeInitialPlan(), "IMPLEMENTATION_TASKS/active.md", "IMPLEMENTATION_TASKS/next.md")
	requireNonGoalPreSyncStopBlocked(t, fixture)
	preSync := runCompleteCommand(t, fixture)
	if preSync.Status != completeStatusAwaiting || preSync.Completed ||
		preSync.Failure == nil || preSync.Failure.Reason != "completed_task_file_still_tracked" {
		t.Fatalf("同期前complete = %#v", preSync)
	}

	fixture.stageNonGoalCompletionSync(t, completePromotedPlan(), "IMPLEMENTATION_TASKS/active.md")
	if err := executeContinuationGate(fixture.cfg, []string{actionContinuationMetadataGuard}, io.Discard); err != nil {
		t.Fatalf("pre-commit guardが正当なnon-GOAL完了同期を拒否しました: %v", err)
	}
	fixture.commitStagedCompletionSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusComplete || !output.Completed {
		t.Fatalf("output = %#v", output)
	}
	if output.ParentRequest == nil ||
		output.ParentRequest.Continuation.State != repositoryproject.ContinuationContinueNow ||
		output.ParentRequest.Continuation.Task != "IMPLEMENTATION_TASKS/next.md" ||
		output.ParentRequest.Continuation.RequiredAction != repositoryproject.ActionStart {
		t.Fatalf("parent request = %#v", output.ParentRequest)
	}
	if got := fixture.st.TaskStatus(); got != state.TaskStatusComplete {
		t.Fatalf("status = %q", got)
	}
	assertNoModelCallStarted(t, fixture)
}

func TestNonGoalBlockedOnlyCompletionAdmitsStopWithoutStart(t *testing.T) {
	initialPlan := "# plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/active.md`\n\n" +
		"## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n\n- `IMPLEMENTATION_TASKS/blocked.md`\n\n" +
		"## 現在の停止理由\n\nなし\n"
	blockedOnlyPlan := "# plan\n\n## ACTIVE\n\n## NEXT（優先順）\n\n" +
		"## BLOCKED / USER_PERMISSION_WAIT\n\n- `IMPLEMENTATION_TASKS/blocked.md`\n\n" +
		"## 現在の停止理由\n\nなし\n"
	fixture := newNonGoalCompletionFixture(t, initialPlan, "IMPLEMENTATION_TASKS/active.md", "IMPLEMENTATION_TASKS/blocked.md")
	requireNonGoalPreSyncStopBlocked(t, fixture)
	fixture.stageNonGoalCompletionSync(t, blockedOnlyPlan, "IMPLEMENTATION_TASKS/active.md")

	if err := executeContinuationGate(fixture.cfg, []string{actionContinuationMetadataGuard}, io.Discard); err != nil {
		t.Fatalf("pre-commit guardがBLOCKEDのみのnon-GOAL完了同期を拒否しました: %v", err)
	}
	fixture.commitStagedCompletionSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusComplete || !output.Completed {
		t.Fatalf("output = %#v", output)
	}
	if output.ParentRequest == nil ||
		output.ParentRequest.Continuation.State != repositoryproject.ContinuationBlocked ||
		output.ParentRequest.Continuation.Task != "IMPLEMENTATION_TASKS/blocked.md" ||
		output.ParentRequest.Continuation.RequiredAction != "" {
		t.Fatalf("parent request = %#v", output.ParentRequest)
	}
	assertNoModelCallStarted(t, fixture)
}

func TestNonGoalExhaustedCompletionAdmitsTerminal(t *testing.T) {
	initialPlan := "# plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/active.md`\n\n" +
		"## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n\n" +
		"## 現在の停止理由\n\nなし\n"
	exhaustedPlan := "# plan\n\n## ACTIVE\n\n## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n\n" +
		"## 現在の停止理由\n\nなし\n"
	fixture := newNonGoalCompletionFixture(t, initialPlan, "IMPLEMENTATION_TASKS/active.md")
	requireNonGoalPreSyncStopBlocked(t, fixture)
	fixture.stageNonGoalCompletionSync(t, exhaustedPlan, "IMPLEMENTATION_TASKS/active.md")

	if err := executeContinuationGate(fixture.cfg, []string{actionContinuationMetadataGuard}, io.Discard); err != nil {
		t.Fatalf("pre-commit guardが空scheduleのnon-GOAL完了同期を拒否しました: %v", err)
	}
	fixture.commitStagedCompletionSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusComplete || !output.Completed {
		t.Fatalf("output = %#v", output)
	}
	if output.ParentRequest == nil ||
		output.ParentRequest.Continuation.State != repositoryproject.ContinuationTerminal ||
		output.ParentRequest.Continuation.Reason != repositoryproject.ReasonScheduleExhausted {
		t.Fatalf("parent request = %#v", output.ParentRequest)
	}
	assertNoModelCallStarted(t, fixture)
}

func TestNonGoalMetadataGuardRejectsIncompleteTransitions(t *testing.T) {
	cases := []struct {
		name         string
		promotedPlan string
		completed    []string
		reason       string
	}{
		{
			name: "promotion gap",
			promotedPlan: "# plan\n\n## ACTIVE\n\n## NEXT（優先順）\n\n- `IMPLEMENTATION_TASKS/next.md`\n\n" +
				"## BLOCKED / USER_PERMISSION_WAIT\n\n## 現在の停止理由\n\nなし\n",
			completed: []string{"IMPLEMENTATION_TASKS/active.md"},
			reason:    "ACTIVE昇格済み",
		},
		{
			name: "closure gap",
			promotedPlan: "# plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/active.md`\n\n" +
				"## NEXT（優先順）\n\n- `IMPLEMENTATION_TASKS/next.md`\n\n" +
				"## BLOCKED / USER_PERMISSION_WAIT\n\n## 現在の停止理由\n\nなし\n",
			completed: []string{"IMPLEMENTATION_TASKS/active.md"},
			reason:    "closureが成立しません",
		},
		{
			name: "ambiguous active",
			promotedPlan: "# plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/active.md`\n\n- `IMPLEMENTATION_TASKS/next.md`\n\n" +
				"## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n\n## 現在の停止理由\n\nなし\n",
			reason: "一意ではありません",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newNonGoalCompletionFixture(t, completeInitialPlan(), "IMPLEMENTATION_TASKS/active.md", "IMPLEMENTATION_TASKS/next.md")
			fixture.stageNonGoalCompletionSync(t, tc.promotedPlan, tc.completed...)

			err := executeContinuationGate(fixture.cfg, []string{actionContinuationMetadataGuard}, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("guard err = %v want %s", err, tc.reason)
			}
			if got := fixture.st.TaskStatus(); got != state.TaskStatusAwaitingParentCompletion && got != state.TaskStatusComplete {
				t.Fatalf("status = %q", got)
			}
		})
	}
}

func newNonGoalCompletionFixture(t *testing.T, initialPlan string, taskFiles ...string) *completeFixture {
	t.Helper()
	fixture := newCompleteRepositoryFixtureWithPlan(t, initialPlan, taskFiles)
	st := fixture.st
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/active.md", []byte("# active\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AcceptParentReview(); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (f *completeFixture) stageNonGoalCompletionSync(t *testing.T, promotedPlan string, completedTasks ...string) {
	t.Helper()
	for _, task := range completedTasks {
		if err := os.Remove(filepath.Join(f.repo, filepath.FromSlash(task))); err != nil {
			t.Fatal(err)
		}
	}
	writePushBindingFile(t, f.repo, "IMPLEMENTATION_PLAN.local.md", promotedPlan)
	runFinalizationGit(t, f.repo, "add", "-A")
}

func (f *completeFixture) commitStagedCompletionSync(t *testing.T) {
	t.Helper()
	runFinalizationGit(t, f.repo, "commit", "-q", "-m", "completion sync")
}

func assertNoModelCallStarted(t *testing.T, fixture *completeFixture) {
	t.Helper()
	logs, err := taskview.ReadStatusTelemetry(fixture.st, fixture.st.ReadOr("task.id", ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, log := range logs {
		if log.CallType != state.CallTypeEvent {
			t.Fatalf("完了同期後にworker/model callが記録されました: type=%s phase=%s", log.CallType, log.Phase)
		}
	}
}

func requireNonGoalPreSyncStopBlocked(t *testing.T, fixture *completeFixture) {
	t.Helper()
	var stdout bytes.Buffer
	if err := executeContinuationGate(fixture.cfg, []string{actionContinuationStopHook}, &stdout); err != nil {
		t.Fatalf("同期前stop hook err = %v", err)
	}
	var hookOutput continuationStopHookOutput
	if err := json.Unmarshal(stdout.Bytes(), &hookOutput); err != nil {
		t.Fatalf("同期前stop hook出力 = %s", stdout.String())
	}
	if hookOutput.Decision != "block" || !strings.Contains(hookOutput.Reason, "continuation-scope-unbound") {
		t.Fatalf("同期前stop hook決定 = %#v", hookOutput)
	}
}
