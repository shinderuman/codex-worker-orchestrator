package runner

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRunSessionRecordsRunnerSetupAndClaudeProcessStages(t *testing.T) {
	fixture := newRunnerSessionFixture(t)
	taskID, err := state.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.runner.state.Write("task.id", taskID); err != nil {
		t.Fatal(err)
	}

	fixture.runFirst(t)

	logs, err := fixture.runner.state.ReadStageLogs(taskID)
	if err != nil {
		t.Fatal(err)
	}
	stages := map[string]state.ModelCallLog{}
	for _, log := range logs {
		if log.CallType != state.CallTypeStage || log.Stage == nil {
			continue
		}
		stages[log.Stage.Stage] = log
	}
	setup, ok := stages[state.StageRunnerSetup]
	if !ok || setup.Outcome != state.StageOutcomeCompleted {
		t.Fatalf("runner-setup stage = %#v", setup)
	}
	process, ok := stages[state.StageClaudeProcess]
	if !ok || process.Outcome != state.StageOutcomeCompleted || process.WallDurationMS <= 0 {
		t.Fatalf("claude-process stage = %#v", process)
	}
	if process.Stage.ClockOrigin != state.ClockOriginWorker {
		t.Fatalf("claude-processのclock origin = %q", process.Stage.ClockOrigin)
	}
	if setup.CompletedAt.After(process.StartedAt) {
		t.Fatalf("runner-setup完了がclaude-process開始より後です: %#v %#v", setup, process)
	}
	if count, err := fixture.runner.state.CountFinalizedTaskCalls(taskID); err != nil || count != 0 {
		t.Fatalf("stage記録がmodel call数へ加算されました: count=%d err=%v", count, err)
	}
}
