package state

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

type MeasureStageFailure struct{}

func (MeasureStageFailure) Error() string { return "preflight失敗" }

func TestStageRecorderBuffersUntilTaskExists(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	t.Setenv(ParentActionAttemptIDEnv, "01a0463c-d477-4410-9efd-cb34ff2e0b0e")
	t.Setenv(ParentActionAttemptEnterEnv, "2026-10-29T01:02:03.123456789Z")
	recorder := NewStageRecorder(st)
	if !recorder.ParentBound() {
		t.Fatal("parent action attempt envがbindingされません")
	}

	recorder.RecordParentDispatch(time.Now().UTC())
	if err := recorder.Measure(StageWorkflowLock, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReadModelCallLogs("01a0463c-d477-4410-9efd-cb34ff2e0b0e"); !os.IsNotExist(err) {
		t.Fatalf("task生成前のstage記録がtelemetryへ直接書かれました: %v", err)
	}

	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	recorder.BindTaskID(taskID)
	logs, err := st.ReadStageLogs(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 3 {
		t.Fatalf("flush後のstage記録 = %d want 2", len(logs))
	}
	dispatch := logs[0]
	if dispatch.CallType != CallTypeStage || dispatch.Stage == nil || dispatch.Stage.Stage != StageParentDispatch {
		t.Fatalf("parent-dispatch記録が不正です: %#v", dispatch)
	}
	if dispatch.Stage.ClockOrigin != ClockOriginParentToWorker {
		t.Fatalf("parent-dispatchのclock origin = %q", dispatch.Stage.ClockOrigin)
	}
	lock := logs[2]
	if lock.Stage.Stage != StageWorkflowLock || lock.Outcome != StageOutcomeCompleted {
		t.Fatalf("workflow-lock記録が不正です: %#v", lock)
	}
	if lock.Stage.AttemptID != "01a0463c-d477-4410-9efd-cb34ff2e0b0e" {
		t.Fatalf("stage記録のattempt bindingがありません: %#v", lock.Stage)
	}
}

func TestStageRecorderMeasureRecordsFailure(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	recorder := NewStageRecorder(st)
	recorder.BindTaskID(taskID)
	measureErr := MeasureStageFailure{}
	if err := recorder.Measure(StageQualityPreflight, func() error { return measureErr }); err == nil {
		t.Fatal("Measureがfnのerrorを隠しました")
	}
	logs, err := st.ReadStageLogs(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 || logs[1].Outcome != StageOutcomeFailed || logs[1].Error != "stage-failed" {
		t.Fatalf("失敗stage記録が不正です: %#v", logs)
	}
	if logs[0].Phase != StagePhasePrefix+StageQualityPreflight {
		t.Fatalf("stage記録のphase = %q", logs[0].Phase)
	}
	if count, err := st.CountFinalizedTaskCalls(taskID); err != nil || count != 0 {
		t.Fatalf("stage記録がmodel call数へ加算されました: count=%d err=%v", count, err)
	}
}

func TestStageRecorderWithoutTaskArchivesBuffer(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	recorder := NewStageRecorder(st)
	if err := recorder.Measure(StageParentAdmission, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	recorder.FlushOrArchive()

	archived, err := st.ReadStartupStageArchive()
	if err != nil {
		t.Fatal(err)
	}
	if len(archived) != 3 || archived[0].Stage == nil || archived[0].Stage.Stage != StageParentAdmission {
		t.Fatalf("task無しworkerのstage bufferがarchiveされません: %#v", archived)
	}
	if archived[0].TaskID != "" {
		t.Fatalf("archive記録にtask IDが設定されています: %q", archived[0].TaskID)
	}
}

func TestStageRecordWireCarriesCurrentSchema(t *testing.T) {
	now := time.Now().UTC()
	record := ModelCallLog{
		Version:     ModelCallLogVersion,
		TaskID:      "01a0463c-d477-4410-9efd-cb34ff2e0b0e",
		CallType:    CallTypeStage,
		StartedAt:   now,
		CompletedAt: now.Add(time.Second),
		Phase:       StagePhasePrefix + StageRunnerSetup,
		Outcome:     StageOutcomeCompleted,
		Stage:       &StageTiming{Stage: StageRunnerSetup, ClockOrigin: ClockOriginWorker},
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var header struct {
		Version        int `json:"version"`
		SchemaRevision int `json:"schema_revision"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		t.Fatal(err)
	}
	if header.Version != ModelCallLogVersion || header.SchemaRevision != ModelCallLogSchemaRevision {
		t.Fatalf("stage記録のwire schema = version %d revision %d", header.Version, header.SchemaRevision)
	}
	var decoded ModelCallLog
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Stage == nil || decoded.Stage.Stage != StageRunnerSetup {
		t.Fatalf("stage欄がround-tripしません: %#v", decoded.Stage)
	}
}

func TestStartupStageSequenceCoversCanonicalBoundaries(t *testing.T) {
	required := []string{
		StageParentDispatch, StageWorkflowLock, StageParentAdmission, StageControllerActivate,
		StageQualityPreflight, StageTaskInit, StageGitQualityBaseline, StageHarnessTaskResolution,
		StageExternalFeasibility, StageRepoSearchContext, StageModelCallAdmission,
		StageRunnerSetup, StageClaudeProcess,
	}
	if len(StartupStageSequence) != len(required) {
		t.Fatalf("startup stage sequence = %v", StartupStageSequence)
	}
	for index, stage := range required {
		if StartupStageSequence[index] != stage {
			t.Fatalf("startup stage sequence[%d] = %q want %q", index, StartupStageSequence[index], stage)
		}
	}
}

func TestParentActionAttemptLedgerAppendReadPrune(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if records, err := st.ReadParentActionAttempts(); err != nil || records != nil {
		t.Fatalf("空ledger読み出し = %#v err=%v", records, err)
	}
	entered := time.Now().UTC().Add(-time.Minute)
	for index := 0; index < parentActionAttemptRecordKeep+25; index++ {
		st.RecordParentActionAttempt(ParentActionAttemptRecord{
			AttemptID:  attemptIDForIndex(index),
			Action:     "start",
			EnteredAt:  entered,
			ExitedAt:   entered.Add(time.Duration(index+1) * time.Second),
			DurationMS: int64(index+1) * 1000,
			Outcome:    ParentActionAttemptOutcomeSuccess,
		})
	}
	records, err := st.ReadParentActionAttempts()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != parentActionAttemptRecordKeep {
		t.Fatalf("prune後のledger件数 = %d want %d", len(records), parentActionAttemptRecordKeep)
	}
	if records[0].AttemptID != attemptIDForIndex(25) {
		t.Fatalf("prune後に古い記録が残っています: %q", records[0].AttemptID)
	}
	if records[len(records)-1].Version != parentActionAttemptRecordVersion {
		t.Fatalf("ledger記録のversion = %d", records[len(records)-1].Version)
	}
}

func TestParentActionAttemptLedgerFailedOutcomeErrorBounded(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	longError := make([]byte, parentActionAttemptErrorMaxLen*4)
	for index := range longError {
		longError[index] = 'x'
	}
	entered := time.Now().UTC()
	st.RecordParentActionAttempt(ParentActionAttemptRecord{
		AttemptID: "01a0463c-d477-4410-9efd-cb34ff2e0b0e",
		Action:    "start",
		EnteredAt: entered,
		Outcome:   ParentActionAttemptOutcomeFailed,
		Error:     string(longError),
	})
	records, err := st.ReadParentActionAttempts()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("ledger件数 = %d", len(records))
	}
	if records[0].Outcome != ParentActionAttemptOutcomeFailed {
		t.Fatalf("outcome = %q", records[0].Outcome)
	}
	if len(records[0].Error) > parentActionAttemptErrorMaxLen {
		t.Fatalf("error textがboundされていません: %d", len(records[0].Error))
	}
	if records[0].ExitedAt.IsZero() {
		t.Fatal("exit時刻が補完されていません")
	}
}

func attemptIDForIndex(index int) string {
	raw, err := json.Marshal(struct {
		Index int `json:"index"`
	}{Index: index})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func TestStageRecorderEnvWithoutAttemptStaysUnbound(t *testing.T) {
	t.Setenv(ParentActionAttemptIDEnv, "")
	t.Setenv(ParentActionAttemptEnterEnv, "")
	st := &StateStore{dir: t.TempDir()}
	recorder := NewStageRecorder(st)
	if recorder.ParentBound() {
		t.Fatal("attempt envなしでbindingされました")
	}
	recorder.RecordParentDispatch(time.Now().UTC())
	if _, err := os.Stat(st.ModelCallLogPath("")); err == nil {
		t.Fatal("unbound parent dispatchが記録されました")
	}
}

func TestStageRecorderRetainsStartedStageAndRedactsError(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	recorder := NewStageRecorder(st)
	recorder.BindTaskID(taskID)
	span := recorder.Span(StageRunnerSetup)
	logs, err := st.ReadStageLogs(taskID)
	if err != nil || len(logs) != 1 || logs[0].Outcome != StageOutcomeStarted || !logs[0].CompletedAt.IsZero() {
		t.Fatalf("終了前のstage開始が保持されていません: %+v %v", logs, err)
	}
	span.End(fmt.Errorf("token=private-sensitive-value"))
	recorder.FlushOrArchive()
	raw, err := os.ReadFile(st.TaskStageLogPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("private-sensitive-value")) {
		t.Fatal("error本文が観測telemetryへ混入しました")
	}
	logs, err = st.ReadStageLogs(taskID)
	if err != nil {
		t.Fatal(err)
	}
	last := logs[len(logs)-1]
	if last.CallType != CallTypeEvent || last.Stage.WriteCalls != 2 || last.Stage.WriteWallNS <= 0 {
		t.Fatalf("観測overheadがありません: %+v", last)
	}
}
