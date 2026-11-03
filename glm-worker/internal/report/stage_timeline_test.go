package report

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskview"
)

func newStageTimelineStore(t *testing.T) *state.StateStore {
	t.Helper()
	home := t.TempDir()
	st, err := state.NewStateStore(config.AppConfig{
		StateBase: home,
		RepoHash:  "stagetimelinehash",
		RepoRoot:  home,
	})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestStageTimelineProjectsMergedBoundariesAndGaps(t *testing.T) {
	st := newStageTimelineStore(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	seedStageTimelineCorpus(t, st, taskID)

	var output bytes.Buffer
	if err := PrintStageTimeline(st, taskID, &output); err != nil {
		t.Fatal(err)
	}
	var decoded StageTimelineOutput
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("stage timeline出力をparseできません: %v: %s", err, output.String())
	}

	kinds := map[string]int{}
	modelCallFirstEvent := false
	for _, entry := range decoded.Entries {
		kinds[entry.Kind]++
		if entry.Kind == stageTimelineKindModelCall && entry.FirstEventAt != nil {
			modelCallFirstEvent = true
		}
	}
	if kinds[stageTimelineKindParentAction] != 2 || kinds[stageTimelineKindModelCall] != 1 {
		t.Fatalf("entries = %#v", decoded.Entries)
	}
	if !modelCallFirstEvent {
		t.Fatalf("model callのfirst event境界がありません: %#v", decoded.Entries)
	}

	var parentWait *stageTimelineGap
	var tailOpen bool
	for index := range decoded.Gaps {
		gap := decoded.Gaps[index]
		if gap.Classification == stageTimelineGapParentWait && gap.DurationMS == 20*60*1000 {
			parentWait = &decoded.Gaps[index]
		}
		if gap.Classification == stageTimelineGapTailOpen {
			tailOpen = true
		}
	}
	if parentWait == nil {
		t.Fatalf("20分の親待機gapが分類されていません: %#v", decoded.Gaps)
	}
	if parentWait.WallShare <= 0.5 {
		t.Fatalf("親待機gapのwall占有率が不正です: %#v", parentWait)
	}
	if tailOpen {
		t.Fatalf("収集時刻までの未観測期間をtask実測へ加算しています: %#v", decoded.Gaps)
	}

	if len(decoded.NotRunStages) != 1 {
		t.Fatalf("not-run一覧 = %#v", decoded.NotRunStages)
	}
	notRun := decoded.NotRunStages[0]
	if notRun.AttemptID != "01a0463c-d477-4410-9efd-cb34ff2e0b0e" {
		t.Fatalf("not-run attempt = %#v", notRun)
	}
	for _, stage := range []string{state.StageRunnerSetup, state.StageClaudeProcess, state.StageTaskInit} {
		found := false
		for _, name := range notRun.NotRun {
			if name == stage {
				found = true
			}
		}
		if !found {
			t.Fatalf("未実行stage %qが表現されていません: %#v", stage, notRun.NotRun)
		}
	}

	if len(decoded.UnattributedAttempts) != 1 || decoded.UnattributedAttempts[0].AttemptID != "01a0463c-d477-4410-9efd-cb34ff2e0b0e10" {
		t.Fatalf("unattributed attempts = %#v", decoded.UnattributedAttempts)
	}
	if decoded.Wall.WallMS != 27*60*1000 || decoded.Wall.MeasuredUnionMS <= 0 || decoded.Wall.GapUnionMS <= 0 {
		t.Fatalf("wall = %#v", decoded.Wall)
	}
	if decoded.Coverage.ProviderSendBoundary == "" || decoded.Coverage.ParentTurnBoundary == "" {
		t.Fatalf("coverage注記がありません: %#v", decoded.Coverage)
	}
}

func TestStageTimelineWithoutAnySourcesIsEmptyButValid(t *testing.T) {
	st := newStageTimelineStore(t)
	var output bytes.Buffer
	if err := PrintStageTimeline(st, "01a0463c-d477-4410-9efd-cb34ff2e0b0e", &output); err != nil {
		t.Fatal(err)
	}
	var decoded StageTimelineOutput
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.TaskID == "" || len(decoded.Entries) != 0 || len(decoded.Gaps) != 0 {
		t.Fatalf("空task projection = %#v", decoded)
	}
	if decoded.Sources.Telemetry.Status != taskview.StatusNone {
		t.Fatalf("欠測sourceがnone扱いされていません: %#v", decoded.Sources)
	}
}

func TestStageTimelineRejectsNonUUIDTaskID(t *testing.T) {
	st := newStageTimelineStore(t)
	if err := PrintStageTimeline(st, "not-a-task", io.Discard); err == nil {
		t.Fatal("不正task idが受理されました")
	}
	if err := PrintStageTimeline(st, "", io.Discard); err == nil {
		t.Fatal("task id無しstart相当の呼び出しが受理されました")
	}
}

func seedStageTimelineCorpus(t *testing.T, st *state.StateStore, taskID string) {
	t.Helper()
	if err := os.Remove(st.TaskLifecycleLogPath(taskID)); err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Add(-35 * time.Minute)
	st.RecordParentActionAttempt(state.ParentActionAttemptRecord{
		AttemptID:  "01a0463c-d477-4410-9efd-cb34ff2e0b0e",
		Action:     "start",
		EnteredAt:  base,
		ExitedAt:   base.Add(6 * time.Minute),
		DurationMS: 6 * 60 * 1000,
		Outcome:    state.ParentActionAttemptOutcomeSuccess,
		TaskID:     taskID,
	})
	st.RecordParentActionAttempt(state.ParentActionAttemptRecord{
		AttemptID:  "01a0463c-d477-4410-9efd-cb34ff2e0b0f",
		Action:     "decision",
		EnteredAt:  base.Add(26 * time.Minute),
		ExitedAt:   base.Add(27 * time.Minute),
		DurationMS: 60 * 1000,
		Outcome:    state.ParentActionAttemptOutcomeSuccess,
		TaskID:     taskID,
	})
	st.RecordParentActionAttempt(state.ParentActionAttemptRecord{
		AttemptID:  "01a0463c-d477-4410-9efd-cb34ff2e0b0e10",
		Action:     "start",
		EnteredAt:  base.Add(time.Minute),
		ExitedAt:   base.Add(90 * time.Second),
		DurationMS: 30 * 1000,
		Outcome:    state.ParentActionAttemptOutcomeFailed,
	})
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID:         taskID,
		CallType:       state.CallTypeTask,
		CallID:         "01a0463c-d477-4410-9efd-cb34ff2e0b0e11",
		StartedAt:      base.Add(5 * time.Minute),
		CompletedAt:    base.Add(5*time.Minute + 30*time.Second),
		WallDurationMS: 30 * 1000,
		Phase:          "worker-new",
		Role:           state.WorkerRole,
		Outcome:        "interrupted",
	})
	if err := st.AppendStageLog(state.ModelCallLog{
		TaskID:      taskID,
		CallID:      "01a0463c-d477-4410-9efd-cb34ff2e0b0f",
		CallType:    state.CallTypeStage,
		StartedAt:   base.Add(30 * time.Second),
		CompletedAt: base.Add(31 * time.Second),
		Phase:       state.StagePhasePrefix + state.StageWorkflowLock,
		Outcome:     state.StageOutcomeCompleted,
		Stage:       &state.StageTiming{Stage: state.StageWorkflowLock, AttemptID: "01a0463c-d477-4410-9efd-cb34ff2e0b0e", ClockOrigin: state.ClockOriginWorker},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendTaskEvent(state.TaskEventRecord{
		TaskID:    taskID,
		CallID:    "01a0463c-d477-4410-9efd-cb34ff2e0b0e11",
		Role:      string(state.WorkerRole),
		Phase:     "worker-new",
		Kind:      "system",
		Subtype:   "init",
		Timestamp: base.Add(5*time.Minute + 2*time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendTaskLifecycle(state.TaskLifecycleRecord{
		TaskID:    taskID,
		Timestamp: base.Add(6 * time.Minute),
		From:      "active",
		To:        "interrupted",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestStageTimelineDoesNotInventGapInsideOverlappingCommand(t *testing.T) {
	base := time.Now()
	end := base.Add(10 * time.Second)
	childStart := base.Add(time.Second)
	childEnd := base.Add(2 * time.Second)
	nextStart := base.Add(9 * time.Second)
	entries := []stageTimelineEntry{
		{Index: 1, Kind: stageTimelineKindParentAction, StartedAt: &base, CompletedAt: &end},
		{Index: 2, Kind: stageTimelineKindStartupStage, StartedAt: &childStart, CompletedAt: &childEnd},
		{Index: 3, Kind: stageTimelineKindLifecycle, StartedAt: &nextStart},
	}
	wall := stageTimelineWallFromEntries(entries, end)
	gaps := stageTimelineGaps(entries, wall, nil, nil, end)
	if len(gaps) != 0 || wall.WallMS != 10000 || wall.MeasuredUnionMS != 10000 || wall.OverlapMS != 1000 {
		t.Fatalf("overlapがgap/追加wallへ変換されました: wall=%+v gaps=%+v", wall, gaps)
	}
	inner := stageTimelineInnerGaps(entries, []state.ParentActionAttemptRecord{{EnteredAt: base, ExitedAt: end}})
	if len(inner) != 2 || inner[0].DurationMS != 1000 || inner[1].DurationMS != 8000 {
		t.Fatalf("command内部の未計測区間が隠れました: %+v", inner)
	}
}

func TestStageTimelineCoalescesInterruptedAndResumedAttempts(t *testing.T) {
	taskID := "01a0463c-d477-4410-9efd-cb34ff2e0b0e"
	attemptID := "01a0463c-d477-4410-9efd-cb34ff2e0b0f"
	base := time.Now()
	attempts, unknown := splitAttempts([]state.ParentActionAttemptRecord{
		{AttemptID: attemptID, Action: "resume", Outcome: "started", EnteredAt: base},
		{AttemptID: attemptID, Action: "resume", Outcome: "failed", EnteredAt: base, ExitedAt: base.Add(time.Second), TaskID: taskID},
		{AttemptID: "foreign", Action: "start", TaskID: "foreign-task"},
	}, taskID)
	if len(attempts) != 1 || len(unknown) != 0 || attempts[0].Outcome != "failed" {
		t.Fatalf("started/finalや別taskが二重に混入しました: %+v %+v", attempts, unknown)
	}
	logs := []state.ModelCallLog{
		{CallID: "span", CallType: state.CallTypeStage, Outcome: state.StageOutcomeStarted, StartedAt: base, Stage: &state.StageTiming{AttemptID: attemptID}},
		{CallID: "span", CallType: state.CallTypeStage, Outcome: state.StageOutcomeCompleted, StartedAt: base, CompletedAt: base.Add(time.Second), Stage: &state.StageTiming{AttemptID: attemptID}},
	}
	merged := finalizedStageLogs(logs)
	if len(merged) != 1 || merged[0].CompletedAt.IsZero() {
		t.Fatalf("span終端が対応しません: %+v", merged)
	}
}

func TestStageTimelineKeepsValidationSourceBinding(t *testing.T) {
	st := newStageTimelineStore(t)
	taskID := "01a0463c-d477-4410-9efd-cb34ff2e0b0e"
	runID := "0123456789abcdef0123456789abcdef"
	start := time.Now().UTC().Add(-time.Minute)
	end := start.Add(4 * time.Second)
	record := qualitygate.RunRecord{ValidationRunID: runID, TaskID: taskID, Form: "go-test", Head: "source-head", WorktreeDigest: "source-digest", StartedAt: start, CompletedAt: &end, DurationMS: 4000, Status: qualitygate.StatusFail}
	path := st.Path(qualitygate.RunRelativePath(runID))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := qualitygate.Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	entries, source := stageTimelineValidationEntries(st, taskID)
	if len(entries) != 1 || source.Status != TimelineSourceOK || entries[0].SourceHead != record.Head || entries[0].SourceDigest != record.WorktreeDigest || entries[0].Locator != path || entries[0].Outcome != qualitygate.StatusFail {
		t.Fatalf("失敗validationのsource bindingが失われました: %+v %+v", entries, source)
	}
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	entries, source = stageTimelineValidationEntries(st, taskID)
	if len(entries) != 0 || source.Status != taskview.StatusUnreadable {
		t.Fatalf("破損runを成功扱いしています: %+v %+v", entries, source)
	}
}

func TestStageTimelineDoesNotUseUnattributedAttemptOrWorkerRevisionAsSource(t *testing.T) {
	base := time.Now()
	classification := classifyStageTimelineGap(base.Add(time.Second), base.Add(2*time.Second), nil, []state.ParentActionAttemptRecord{{EnteredAt: base, ExitedAt: base.Add(3 * time.Second)}})
	if classification != stageTimelineGapUnattributed {
		t.Fatalf("未帰属attemptから分類を推定しました: %s", classification)
	}
	entry, ok := stageTimelineLogEntry(state.ModelCallLog{CallType: state.CallTypeTask, Runtime: &state.CallRuntime{WorkerRevision: "runtime-head"}}, nil)
	if !ok || entry.SourceHead != "" || entry.WorkerRevision != "runtime-head" {
		t.Fatalf("runtime revisionとrepository sourceが混同されました: %+v", entry)
	}
}

func TestStageTimelineRetainsArchiveLocator(t *testing.T) {
	st := newStageTimelineStore(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	attemptID := "01a0463c-d477-4410-9efd-cb34ff2e0b0e"
	at := time.Now().UTC()
	st.RecordParentActionAttempt(state.ParentActionAttemptRecord{AttemptID: attemptID, TaskID: taskID, Action: "resume", EnteredAt: at, ExitedAt: at.Add(time.Second)})
	if err := st.AppendStartupStageArchive([]state.ModelCallLog{{TaskID: "", CallID: "span-in-archive", CallType: state.CallTypeStage, StartedAt: at, CompletedAt: at.Add(time.Millisecond), Stage: &state.StageTiming{AttemptID: attemptID, Stage: state.StageQualityPreflight}}}); err != nil {
		t.Fatal(err)
	}
	output := buildStageTimeline(st, taskID, true)
	for _, entry := range output.Entries {
		if entry.Name == state.StageQualityPreflight {
			if !strings.HasPrefix(entry.Locator, st.Path("startup-stage-archive.jsonl")+"#call_id=span-in-archive") {
				t.Fatalf("archive sourceが誤ったpathです: %+v", entry)
			}
			return
		}
	}
	t.Fatal("archive entryがありません")
}
