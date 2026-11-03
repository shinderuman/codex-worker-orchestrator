package app

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExecuteNewTaskInterruptedPathKeepsStartupStageTimings(t *testing.T) {
	cfg, rootTask, _, _ := newCanonicalWorkflowCorpus(t)
	scripted := runCanonicalWorkflow(t, cfg, ModeNewTask, "startup timing request", rootTask, "")

	st := state.AttachStateStore(scripted.cfg)
	taskID := st.ReadOr("task.id", "")
	if taskID == "" {
		t.Fatal("new taskが生成されていません")
	}
	logs, err := st.ReadStageLogs(taskID)
	if err != nil {
		t.Fatal(err)
	}
	stages := startupStageOutcomes(logs)
	for _, stage := range []string{
		state.StageWorkflowLock,
		state.StageParentAdmission,
		state.StageControllerActivate,
		state.StageQualityPreflight,
		state.StageTaskInit,
		state.StageGitQualityBaseline,
		state.StageHarnessTaskResolution,
		state.StageExternalFeasibility,
		state.StageModelCallAdmission,
	} {
		if outcome, ok := stages[stage]; !ok {
			t.Fatalf("stage %qがtelemetryへ記録されていません: %#v", stage, stages)
		} else if outcome != state.StageOutcomeCompleted {
			t.Fatalf("stage %qのoutcome = %q", stage, outcome)
		}
	}
	if outcome, ok := stages[state.StageRepoSearchContext]; !ok || (outcome != state.StageOutcomeCompleted && outcome != state.StageOutcomeNotRun) {
		t.Fatalf("repo search stage = %#v", stages)
	}
	interrupted := false
	modelLogs, err := st.ReadModelCallLogs(taskID)
	if err != nil {
		t.Fatal(err)
	}
	for _, log := range modelLogs {
		if log.CallType == state.CallTypeTask && log.Outcome == "interrupted" {
			interrupted = true
		}
	}
	if !interrupted {
		t.Fatal("中断pathでtask呼出記録がありません")
	}
}

func startupStageOutcomes(logs []state.ModelCallLog) map[string]string {
	outcomes := map[string]string{}
	for _, log := range logs {
		if log.CallType != state.CallTypeStage || log.Stage == nil {
			continue
		}
		outcomes[log.Stage.Stage] = log.Outcome
	}
	return outcomes
}

func TestExecuteNewTaskPreflightFailureArchivesStartupStages(t *testing.T) {
	cfg := newQualityContractConfig(t)
	fresh, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{steps: []fakeStep{{structured: implementedPacketApp("done")}}}
	stubQualityPreflight(t, func(string) error {
		return &harnesslint.QualityToolVersionMismatch{Tool: "shfmt", Observed: "3.8.0", Required: "3.13.1"}
	})

	err = Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, r.factory(), io.Discard, io.Discard)
	var preflightErr *QualityPreflightError
	if err == nil || !errors.As(err, &preflightErr) {
		t.Fatalf("preflight mismatchをtyped errorで返す必要があります: %v", err)
	}
	if len(r.prompts) != 0 {
		t.Fatalf("model callが実行されました: %d", len(r.prompts))
	}

	archived, err := fresh.ReadStartupStageArchive()
	if err != nil {
		t.Fatal(err)
	}
	outcomes := map[string]string{}
	for _, record := range archived {
		if record.Stage == nil {
			continue
		}
		outcomes[record.Stage.Stage] = record.Outcome
	}
	for _, stage := range []string{state.StageWorkflowLock, state.StageParentAdmission, state.StageControllerActivate} {
		if outcomes[stage] != state.StageOutcomeCompleted {
			t.Fatalf("pre-model失敗でも実行済みstageがarchiveされていません: %s = %q (%#v)", stage, outcomes[stage], outcomes)
		}
	}
	if outcomes[state.StageQualityPreflight] != state.StageOutcomeFailed {
		t.Fatalf("失敗stage = %#v", outcomes)
	}
	for _, stage := range []string{state.StageTaskInit, state.StageRunnerSetup, state.StageClaudeProcess} {
		if _, ok := outcomes[stage]; ok {
			t.Fatalf("未実行stage %qが0ms実行として記録されました", stage)
		}
	}
	entries, err := os.ReadDir(fresh.Path("telemetry"))
	if err == nil && len(entries) > 0 {
		t.Fatalf("task未生成の失敗でtelemetryが書かれました: %#v", entries)
	}
}

func TestExecuteNewTaskPreflightFailureDoesNotAttributeToResidualTaskID(t *testing.T) {
	cfg := newQualityContractConfig(t)
	st, residualTaskID := seedPreflightTaskID(t, cfg)
	r := &fakeRunner{steps: []fakeStep{{structured: implementedPacketApp("done")}}}
	stubQualityPreflight(t, func(string) error {
		return &harnesslint.QualityToolVersionMismatch{Tool: "golangci-lint", Observed: "2.13.1", Required: "2.7.0"}
	})

	err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, r.factory(), io.Discard, io.Discard)
	var preflightErr *QualityPreflightError
	if err == nil || !errors.As(err, &preflightErr) {
		t.Fatalf("preflight mismatchをtyped errorで返す必要があります: %v", err)
	}
	if _, statErr := os.Stat(st.ModelCallLogPath(residualTaskID)); !os.IsNotExist(statErr) {
		t.Fatalf("残留task idへの誤帰属を防いでいません: %v", statErr)
	}
	archived, err := st.ReadStartupStageArchive()
	if err != nil {
		t.Fatal(err)
	}
	if len(archived) == 0 {
		t.Fatal("残留task idがある場合もstage bufferがarchiveされていません")
	}
	for _, record := range archived {
		if record.TaskID != "" {
			t.Fatalf("archive記録が残留task idへ帰属しています: %q", record.TaskID)
		}
	}
}

func TestStartupStageRecordsBindParentActionAttemptIdentity(t *testing.T) {
	cfg, rootTask, _, _ := newCanonicalWorkflowCorpus(t)
	t.Setenv(state.ParentActionAttemptIDEnv, "01a0463c-d477-4410-9efd-cb34ff2e0b0e")
	t.Setenv(state.ParentActionAttemptEnterEnv, "2026-10-29T01:02:03Z")
	scripted := runCanonicalWorkflow(t, cfg, ModeNewTask, "parent dispatch request", rootTask, "")

	st := state.AttachStateStore(scripted.cfg)
	taskID := st.ReadOr("task.id", "")
	if taskID == "" {
		t.Fatal("new taskが生成されていません")
	}
	logs, err := st.ReadStageLogs(taskID)
	if err != nil {
		t.Fatal(err)
	}
	var dispatchSeen bool
	stageCount := 0
	for _, log := range logs {
		if log.CallType != state.CallTypeStage || log.Stage == nil {
			continue
		}
		stageCount++
		if log.Stage.AttemptID != "01a0463c-d477-4410-9efd-cb34ff2e0b0e" {
			t.Fatalf("stage %qのattempt binding = %q", log.Stage.Stage, log.Stage.AttemptID)
		}
		if log.Stage.Stage == state.StageParentDispatch {
			dispatchSeen = true
			if log.Stage.ClockOrigin != state.ClockOriginParentToWorker {
				t.Fatalf("parent-dispatchのclock origin = %q", log.Stage.ClockOrigin)
			}
		}
	}
	if stageCount == 0 || !dispatchSeen {
		t.Fatalf("stage記録またはparent-dispatchがありません: stages=%d dispatch=%v", stageCount, dispatchSeen)
	}
}
