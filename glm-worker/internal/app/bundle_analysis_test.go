package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type analysisBundleFixture struct {
	st            *state.StateStore
	codexHome     string
	taskID        string
	start         time.Time
	inWindow      []string
	rolloutRel    string
	guardedBefore []byte
	manifest      bundleManifest
	index         bundleAnalysisIndex
}

type analysisTerminalTask struct {
	cfg        config.AppConfig
	st         *state.StateStore
	codexHome  string
	taskID     string
	start      time.Time
	completeAt time.Time
}

const analysisRunExternal = "11111111111111111111111111111111"

const analysisRunEventLinked = "22222222222222222222222222222222"

const analysisRunPreviousFail = "55555555555555555555555555555555"

const analysisRunDigestMatched = "33333333333333333333333333333333"

const analysisRunUnmatched = "44444444444444444444444444444444"

const analysisOwningTurnID = "turn-owning"

const analysisLaterTurnID = "turn-later"

const analysisOpenTurnID = "turn-open"

const analysisStraddleTurnID = "turn-straddle"

const analysisPostArchiveTurnID = "turn-post-archive"

func TestBundleAnalysisIndexWindowSummaries(t *testing.T) {
	fixture := newAnalysisBundleFixture(t)

	if fixture.manifest.Format != bundleFormat || fixture.manifest.AnalysisIndex != bundleAnalysisEntryPath {
		t.Fatalf("manifest = %s/%s", fixture.manifest.Format, fixture.manifest.AnalysisIndex)
	}
	if !slices.Contains(fixture.manifest.Included, bundleAnalysisEntryPath) {
		t.Fatalf("included = %v", fixture.manifest.Included)
	}
	index := fixture.index
	if index.Version != bundleAnalysisIndexVersion || index.TaskID != fixture.taskID {
		t.Fatalf("index = %#v", index)
	}
	collection := index.Intervals.Collection
	if collection.Status != analysisStatusAvailable || collection.End == nil ||
		collection.EndBasis != analysisWindowEndBasisBundleTime ||
		*collection.Start != fixture.start.Format(time.RFC3339Nano) {
		t.Fatalf("collection interval = %#v", collection)
	}
	execution := index.Intervals.TaskExecution
	if execution.Status != analysisStatusOpen || execution.End != nil ||
		execution.EndBasis != "" || *execution.Start != fixture.start.Format(time.RFC3339Nano) {
		t.Fatalf("execution interval = %#v", execution)
	}
	finalization := index.Intervals.ParentFinalization
	if finalization.Status != analysisStatusOpen || finalization.Start != nil || finalization.End != nil {
		t.Fatalf("finalization interval = %#v", finalization)
	}
	subsequent := index.Intervals.SubsequentRequests
	if subsequent.Status != analysisStatusOpen || subsequent.Attribution != analysisAttributionSubsequent ||
		len(subsequent.Turns) != 0 {
		t.Fatalf("subsequent requests = %#v", subsequent)
	}
	if index.ParentSession.Status != codexStatusIncluded || index.ParentSession.ThreadID != codexTestParentThreadID {
		t.Fatalf("parent session = %#v", index.ParentSession)
	}
	if index.WaitCalls.Status != analysisStatusOpen || index.WaitCalls.Count != 3 || len(index.WaitCalls.Calls) != 3 {
		t.Fatalf("wait calls = %#v", index.WaitCalls)
	}
	short := index.WaitCalls.Calls[0]
	if short.CallID != "wait-analysis-short" || short.YieldClass != analysisWaitYieldClassShort ||
		short.RequestedYieldMS == nil || *short.RequestedYieldMS != 1000 ||
		!slices.Equal(short.RequestLines, []int{7}) || !slices.Equal(short.ReturnLines, []int{8}) {
		t.Fatalf("short wait = %#v", short)
	}
	bounded := index.WaitCalls.Calls[1]
	if bounded.CallID != "wait-analysis-bounded" || bounded.YieldClass != analysisWaitYieldClassBounded ||
		bounded.RequestedYieldMS == nil || *bounded.RequestedYieldMS != 60000 ||
		!slices.Equal(bounded.RequestLines, []int{11}) || len(bounded.ReturnLines) != 0 {
		t.Fatalf("bounded wait = %#v", bounded)
	}
	unknown := index.WaitCalls.Calls[2]
	if unknown.CallID != "wait-analysis-unknown" || unknown.YieldClass != analysisStatusUnknown ||
		unknown.RequestedYieldMS != nil || !slices.Equal(unknown.RequestLines, []int{12}) {
		t.Fatalf("unknown wait = %#v", unknown)
	}
	if index.TokenDelta.Status != analysisStatusOpen ||
		index.TokenDelta.InputTokens != 500 || index.TokenDelta.CachedInputTokens != 1000 {
		t.Fatalf("token delta = %#v", index.TokenDelta)
	}
	if index.Finalization.Status != analysisStatusOpen {
		t.Fatalf("finalization delta = %#v", index.Finalization)
	}
	if !bytes.Equal(fixture.guardedBefore, readAnalysisGuardedFiles(t, fixture.st, fixture.taskID)) {
		t.Fatal("bundle analysis index生成が原本stateを変更しました")
	}

	raw, err := os.ReadFile(filepath.Join(fixture.codexHome, filepath.FromSlash(fixture.rolloutRel)))
	if err != nil {
		t.Fatal(err)
	}
	rollout := index.RolloutWindow
	if rollout.TotalBytes != int64(len(raw)) {
		t.Fatalf("total bytes = %d want %d", rollout.TotalBytes, len(raw))
	}
	if string(raw[rollout.WindowStartOffset:rollout.WindowEndOffset]) != strings.Join(fixture.inWindow, "") {
		t.Fatal("window byte range does not reproduce the in-window records")
	}
	if rollout.BaselineOffset >= rollout.WindowStartOffset {
		t.Fatalf("baseline offset = %d window start = %d", rollout.BaselineOffset, rollout.WindowStartOffset)
	}
}

func TestBundleAnalysisIntervalPhases(t *testing.T) {
	task := newAnalysisTerminalTask(t)
	writeAnalysisRollout(t, task.codexHome, analysisRolloutRel(), codexTestParentThreadID,
		task.start.Add(-3*time.Hour), analysisPhaseRolloutLines(t, task.start, task.completeAt))
	index := runAnalysisBundle(t, task.cfg, "")

	execution := index.Intervals.TaskExecution
	if execution.Status != analysisStatusAvailable || execution.EndBasis != analysisExecutionEndBasisLifecycleComplete ||
		execution.End == nil || *execution.Start != task.start.Format(time.RFC3339Nano) ||
		*execution.End != task.completeAt.Format(time.RFC3339Nano) {
		t.Fatalf("execution interval = %#v", execution)
	}
	finalization := index.Intervals.ParentFinalization
	finalizationEnd := task.completeAt.Add(2 * time.Minute)
	if finalization.Status != analysisStatusAvailable || finalization.Start == nil || finalization.End == nil ||
		*finalization.Start != task.completeAt.Format(time.RFC3339Nano) ||
		*finalization.End != finalizationEnd.Format(time.RFC3339Nano) {
		t.Fatalf("finalization interval = %#v", finalization)
	}
	subsequent := index.Intervals.SubsequentRequests
	if subsequent.Status != analysisStatusAvailable || len(subsequent.Turns) != 2 {
		t.Fatalf("subsequent requests = %#v", subsequent)
	}
	later := subsequent.Turns[0]
	if later.TurnID != analysisLaterTurnID || later.Status != analysisStatusAvailable || later.CompletedAt == nil ||
		later.InputTokens != 600 || later.CachedInputTokens != 300 ||
		later.BaselineAt != task.completeAt.Add(30*time.Second).Format(time.RFC3339Nano) ||
		later.EndAt != task.completeAt.Add(6*time.Minute+10*time.Second).Format(time.RFC3339Nano) {
		t.Fatalf("later turn = %#v", later)
	}
	open := subsequent.Turns[1]
	if open.TurnID != analysisOpenTurnID || open.Status != analysisStatusOpen || open.CompletedAt != nil ||
		open.InputTokens != 0 || open.EndAt != "" {
		t.Fatalf("open turn = %#v", open)
	}
	if index.TokenDelta.Status != analysisStatusAvailable ||
		index.TokenDelta.InputTokens != 1000 || index.TokenDelta.CachedInputTokens != 500 ||
		index.TokenDelta.BaselineAt != task.start.Add(-time.Minute).Format(time.RFC3339Nano) ||
		index.TokenDelta.EndAt != task.completeAt.Add(-time.Second).Format(time.RFC3339Nano) {
		t.Fatalf("token delta = %#v", index.TokenDelta)
	}
	if index.Finalization.Status != analysisStatusAvailable ||
		index.Finalization.InputTokens != 600 || index.Finalization.CachedInputTokens != 300 ||
		index.Finalization.BaselineAt != task.completeAt.Add(-time.Second).Format(time.RFC3339Nano) ||
		index.Finalization.EndAt != task.completeAt.Add(30*time.Second).Format(time.RFC3339Nano) {
		t.Fatalf("finalization delta = %#v", index.Finalization)
	}
	if index.WaitCalls.Status != analysisStatusCounted || index.WaitCalls.Count != 1 {
		t.Fatalf("wait calls = %#v", index.WaitCalls)
	}
	collection := index.Intervals.Collection
	if collection.Status != analysisStatusAvailable || collection.End == nil ||
		collection.EndBasis != analysisWindowEndBasisBundleTime {
		t.Fatalf("collection interval = %#v", collection)
	}
}

func TestBundleAnalysisRecollectionKeepsExecutionPinned(t *testing.T) {
	task := newAnalysisTerminalTask(t)
	straddleStart := task.completeAt.Add(8 * time.Minute)
	writeAnalysisRollout(t, task.codexHome, analysisRolloutRel(), codexTestParentThreadID,
		task.start.Add(-3*time.Hour), append(analysisPhaseRolloutLines(t, task.start, task.completeAt),
			analysisTurnLine(t, straddleStart, codexRolloutTaskStartedType, analysisStraddleTurnID)))
	first := runAnalysisBundle(t, task.cfg, "")
	firstSubsequent := first.Intervals.SubsequentRequests
	if len(firstSubsequent.Turns) != 3 {
		t.Fatalf("first subsequent requests = %#v", firstSubsequent)
	}
	if firstSubsequent.Turns[1].TurnID != analysisStraddleTurnID ||
		firstSubsequent.Turns[1].Status != analysisStatusOpen || firstSubsequent.Turns[1].CompletedAt != nil {
		t.Fatalf("straddle turn before completion = %#v", firstSubsequent.Turns[1])
	}

	appendAnalysisRolloutLines(t, task.codexHome, analysisRolloutRel(), []string{
		analysisTokenCountLine(t, task.completeAt.Add(11*time.Minute+30*time.Second), 4000, 2000),
		analysisTurnLine(t, task.completeAt.Add(12*time.Minute), codexRolloutTaskCompleteType, analysisOpenTurnID),
	})
	second := runAnalysisBundle(t, task.cfg, "")
	secondSubsequent := second.Intervals.SubsequentRequests
	if len(secondSubsequent.Turns) != 3 {
		t.Fatalf("second subsequent requests = %#v", secondSubsequent)
	}
	evolved := secondSubsequent.Turns[2]
	if evolved.TurnID != analysisOpenTurnID || evolved.Status != analysisStatusAvailable ||
		evolved.CompletedAt == nil || evolved.InputTokens != 800 || evolved.CachedInputTokens != 400 ||
		evolved.BaselineAt != task.completeAt.Add(6*time.Minute+10*time.Second).Format(time.RFC3339Nano) ||
		evolved.EndAt != task.completeAt.Add(11*time.Minute+30*time.Second).Format(time.RFC3339Nano) {
		t.Fatalf("open turn after in-window completion = %#v", evolved)
	}

	if _, err := task.st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	archivedAt := analysisArchivedAt(t, task.st, task.taskID)
	appendAnalysisRolloutLines(t, task.codexHome, analysisRolloutRel(), []string{
		analysisTurnLine(t, archivedAt.Add(time.Hour), codexRolloutTaskCompleteType, analysisStraddleTurnID),
		analysisTokenCountLine(t, archivedAt.Add(time.Hour+30*time.Minute), 5000, 2500),
		analysisTurnLine(t, archivedAt.Add(2*time.Hour), codexRolloutTaskStartedType, analysisPostArchiveTurnID),
		analysisTokenCountLine(t, archivedAt.Add(2*time.Hour+10*time.Minute), 6000, 3000),
	})
	third := runAnalysisBundle(t, task.cfg, task.taskID)

	collection := third.Intervals.Collection
	if collection.EndBasis != analysisWindowEndBasisArchivedAt || collection.End == nil ||
		*collection.End != archivedAt.Format(time.RFC3339Nano) {
		t.Fatalf("collection interval = %#v", collection)
	}
	if third.TokenDelta != first.TokenDelta || !reflect.DeepEqual(third.WaitCalls, first.WaitCalls) ||
		third.Finalization != first.Finalization {
		t.Fatalf("pinned values changed: %#v / %#v / %#v", third.TokenDelta, third.WaitCalls, third.Finalization)
	}
	if !analysisIntervalEqual(third.Intervals.TaskExecution, first.Intervals.TaskExecution) ||
		!analysisIntervalEqual(third.Intervals.ParentFinalization, first.Intervals.ParentFinalization) {
		t.Fatalf("pinned intervals changed: %#v / %#v",
			third.Intervals.TaskExecution, third.Intervals.ParentFinalization)
	}
	thirdSubsequent := third.Intervals.SubsequentRequests
	if len(thirdSubsequent.Turns) != 3 {
		t.Fatalf("third subsequent requests = %#v", thirdSubsequent)
	}
	if thirdSubsequent.Turns[0].TurnID != analysisLaterTurnID ||
		thirdSubsequent.Turns[0].InputTokens != 600 || thirdSubsequent.Turns[0].CachedInputTokens != 300 {
		t.Fatalf("later turn = %#v", thirdSubsequent.Turns[0])
	}
	straddle := thirdSubsequent.Turns[1]
	if straddle.Status != analysisStatusOpen || straddle.CompletedAt != nil ||
		straddle.InputTokens != 0 || straddle.EndAt != "" {
		t.Fatalf("straddle turn after archive = %#v", straddle)
	}
	if thirdSubsequent.Turns[2].TurnID != analysisOpenTurnID ||
		thirdSubsequent.Turns[2].InputTokens != 800 || thirdSubsequent.Turns[2].CachedInputTokens != 400 {
		t.Fatalf("open turn after archive = %#v", thirdSubsequent.Turns[2])
	}
}

func TestBundleAnalysisBoundaryEvidenceMissing(t *testing.T) {
	cases := []struct {
		name               string
		removeLifecycle    bool
		lines              func(start, completeAt time.Time) []string
		executionStatus    string
		finalizationStatus string
		subsequentStatus   string
		tokenDeltaStatus   string
		waitCallsStatus    string
	}{
		{
			name:            "lifecycle-missing",
			removeLifecycle: true,
			lines:           func(start, completeAt time.Time) []string { return analysisPhaseRolloutLines(t, start, completeAt) },
			executionStatus: analysisStatusUnknown, finalizationStatus: analysisStatusUnknown,
			subsequentStatus: analysisStatusAvailable, tokenDeltaStatus: analysisStatusUnknown,
			waitCallsStatus: analysisStatusUnknown,
		},
		{
			name: "no-containing-turn",
			lines: func(start, _ time.Time) []string {
				return []string{
					analysisTurnLine(t, start.Add(-2*time.Hour), codexRolloutTaskStartedType, "turn-early"),
					analysisTokenCountLine(t, start.Add(-90*time.Minute), 100, 50),
					analysisTurnLine(t, start.Add(-time.Hour), codexRolloutTaskCompleteType, "turn-early"),
					analysisTokenCountLine(t, start.Add(10*time.Minute), 200, 100),
					analysisTurnLine(t, start.Add(time.Hour), codexRolloutTaskStartedType, "turn-next"),
				}
			},
			executionStatus: analysisStatusAvailable, finalizationStatus: analysisStatusUnknown,
			subsequentStatus: analysisStatusUnknown, tokenDeltaStatus: analysisStatusAvailable,
			waitCallsStatus: analysisStatusCounted,
		},
		{
			name: "ambiguous-containing-turn",
			lines: func(start, _ time.Time) []string {
				return []string{
					analysisTokenCountLine(t, start.Add(-time.Minute), 100, 50),
					analysisTurnLine(t, start.Add(-30*time.Second), codexRolloutTaskStartedType, "turn-a"),
					analysisTurnLine(t, start.Add(-20*time.Second), codexRolloutTaskStartedType, "turn-b"),
					analysisTokenCountLine(t, start.Add(10*time.Minute), 200, 100),
				}
			},
			executionStatus: analysisStatusAvailable, finalizationStatus: analysisStatusUnknown,
			subsequentStatus: analysisStatusUnknown, tokenDeltaStatus: analysisStatusAvailable,
			waitCallsStatus: analysisStatusCounted,
		},
		{
			name: "finalization-boundary-inverted",
			lines: func(start, _ time.Time) []string {
				return []string{
					analysisTokenCountLine(t, start.Add(-time.Minute), 100, 50),
					analysisTurnLine(t, start.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
					analysisTurnLine(t, start.Add(10*time.Minute), codexRolloutTaskCompleteType, analysisOwningTurnID),
					analysisTokenCountLine(t, start.Add(20*time.Minute), 200, 100),
				}
			},
			executionStatus: analysisStatusAvailable, finalizationStatus: analysisStatusUnknown,
			subsequentStatus: analysisStatusAvailable, tokenDeltaStatus: analysisStatusAvailable,
			waitCallsStatus: analysisStatusCounted,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := newAnalysisTerminalTask(t)
			if tc.removeLifecycle {
				if err := os.Remove(task.st.TaskLifecycleLogPath(task.taskID)); err != nil {
					t.Fatal(err)
				}
			}
			writeAnalysisRollout(t, task.codexHome, analysisRolloutRel(), codexTestParentThreadID,
				task.start.Add(-3*time.Hour), tc.lines(task.start, task.completeAt))
			index := runAnalysisBundle(t, task.cfg, "")

			intervals := index.Intervals
			if intervals.TaskExecution.Status != tc.executionStatus {
				t.Fatalf("execution = %#v", intervals.TaskExecution)
			}
			if intervals.ParentFinalization.Status != tc.finalizationStatus {
				t.Fatalf("finalization = %#v", intervals.ParentFinalization)
			}
			if intervals.SubsequentRequests.Status != tc.subsequentStatus {
				t.Fatalf("subsequent = %#v", intervals.SubsequentRequests)
			}
			if index.TokenDelta.Status != tc.tokenDeltaStatus {
				t.Fatalf("token delta = %#v", index.TokenDelta)
			}
			if index.WaitCalls.Status != tc.waitCallsStatus {
				t.Fatalf("wait calls = %#v", index.WaitCalls)
			}
			if index.Finalization.InputTokens != 0 || index.Finalization.CachedInputTokens != 0 ||
				index.Finalization.BaselineAt != "" || index.Finalization.EndAt != "" {
				t.Fatalf("finalization delta = %#v", index.Finalization)
			}
		})
	}
}

func TestBundleAnalysisSharedRolloutTwoTasks(t *testing.T) {
	cfg, st, codexHome := newCodexBundleTestState(t)
	firstTaskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetParentCodexIdentity(codexTestParentThreadID, codexTestParentSessionID); err != nil {
		t.Fatal(err)
	}
	firstStart := time.Now().UTC().Add(-3 * time.Hour)
	firstComplete := firstStart.Add(20 * time.Minute)
	analysisRetireCurrentTask(t, st, firstTaskID, firstStart, firstComplete)

	secondTaskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetParentCodexIdentity(codexTestParentThreadID, codexTestParentSessionID); err != nil {
		t.Fatal(err)
	}
	secondStart := firstStart.Add(40 * time.Minute)
	secondComplete := secondStart.Add(20 * time.Minute)
	analysisRetireCurrentTask(t, st, secondTaskID, secondStart, secondComplete)
	turnEnd := secondComplete.Add(time.Minute)

	writeAnalysisRollout(t, codexHome, analysisRolloutRel(), codexTestParentThreadID, firstStart.Add(-3*time.Hour), []string{
		analysisTurnLine(t, firstStart.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
		analysisTokenCountLine(t, firstStart.Add(-time.Minute), 100, 50),
		analysisTokenCountLine(t, firstComplete, 200, 100),
		analysisTokenCountLine(t, secondStart, 300, 150),
		analysisTokenCountLine(t, secondComplete, 400, 200),
		analysisTurnLine(t, turnEnd, codexRolloutTaskCompleteType, analysisOwningTurnID),
	})

	first := runAnalysisBundle(t, cfg, firstTaskID)
	if first.TokenDelta.Status != analysisStatusAvailable ||
		first.TokenDelta.InputTokens != 100 || first.TokenDelta.CachedInputTokens != 50 ||
		first.TokenDelta.EndAt != firstComplete.Format(time.RFC3339Nano) {
		t.Fatalf("first task token delta = %#v", first.TokenDelta)
	}
	if first.Finalization.Status != analysisStatusAvailable ||
		first.Finalization.InputTokens != 200 || first.Finalization.CachedInputTokens != 100 {
		t.Fatalf("first task finalization = %#v", first.Finalization)
	}

	second := runAnalysisBundle(t, cfg, secondTaskID)
	if second.TokenDelta.Status != analysisStatusAvailable ||
		second.TokenDelta.InputTokens != 100 || second.TokenDelta.CachedInputTokens != 50 ||
		second.TokenDelta.BaselineAt != secondStart.Format(time.RFC3339Nano) {
		t.Fatalf("second task token delta = %#v", second.TokenDelta)
	}
	if second.Finalization.Status != analysisStatusNoObservation {
		t.Fatalf("second task finalization = %#v", second.Finalization)
	}
	if len(second.Intervals.SubsequentRequests.Turns) != 0 {
		t.Fatalf("second task subsequent = %#v", second.Intervals.SubsequentRequests)
	}
	if second.Intervals.TaskExecution.Status != analysisStatusAvailable ||
		*second.Intervals.TaskExecution.End != secondComplete.Format(time.RFC3339Nano) {
		t.Fatalf("second task execution = %#v", second.Intervals.TaskExecution)
	}
}

func TestBundleAnalysisIndexValidationRunAttribution(t *testing.T) {
	index := newAnalysisBundleFixture(t).index

	attribution := map[string]bundleAnalysisRun{}
	for _, run := range index.ValidationRuns.Runs {
		attribution[run.RunID] = run
	}
	wantRuns := map[string]struct {
		attribution string
		bases       []string
	}{
		analysisRunEventLinked:   {analysisAttributionTask, []string{analysisBasisTaskEventValidation}},
		analysisRunDigestMatched: {analysisAttributionTask, []string{analysisBasisRoundSnapshotDigest, analysisBasisWindowOverlap}},
		analysisRunUnmatched:     {analysisAttributionWindowUnmatched, []string{analysisBasisWindowOverlap}},
	}
	for runID, want := range wantRuns {
		run, ok := attribution[runID]
		if !ok {
			t.Fatalf("run %s is missing from the index", runID)
		}
		if run.Attribution != want.attribution || !slices.Equal(run.Bases, want.bases) {
			t.Fatalf("run %s = %s/%v", runID, run.Attribution, run.Bases)
		}
	}
	if len(index.ValidationRuns.Runs) != len(wantRuns) {
		t.Fatalf("runs = %#v", index.ValidationRuns.Runs)
	}
	if attribution[analysisRunDigestMatched].RoundSeq != 1 {
		t.Fatalf("digest matched round seq = %d", attribution[analysisRunDigestMatched].RoundSeq)
	}
}

func TestBundleAnalysisIndexRetriesAndEvidence(t *testing.T) {
	fixture := newAnalysisBundleFixture(t)
	index := fixture.index

	if len(index.Retries.ValidationReruns) != 1 {
		t.Fatalf("reruns = %#v", index.Retries.ValidationReruns)
	}
	rerun := index.Retries.ValidationReruns[0]
	if rerun.RunID != analysisRunEventLinked || rerun.Reason != analysisRetryAfterFail ||
		rerun.PreviousRunID != analysisRunPreviousFail {
		t.Fatalf("rerun = %#v", rerun)
	}
	if index.Retries.WorkerCounters["rate_limits"] != 1 {
		t.Fatalf("worker counters = %#v", index.Retries.WorkerCounters)
	}
	if index.Retries.ResumedModelCalls.Status != analysisStatusAvailable || index.Retries.ResumedModelCalls.Count != 1 {
		t.Fatalf("resumed calls = %#v", index.Retries.ResumedModelCalls)
	}
	relations := index.Retries.ModelCallRelations
	if relations.Status != analysisStatusAvailable || len(relations.Resolved) != 1 ||
		len(relations.Dangling) != 0 || len(relations.Ambiguous) != 0 ||
		len(relations.Unlinked) != 0 || len(relations.DuplicateCallIDs) != 0 {
		t.Fatalf("model call relations = %#v", relations)
	}
	edge := relations.Resolved[0]
	if edge.CallID != "call-analysis-retry" || edge.RetryOf != "call-analysis-original" ||
		edge.RetryReason != "invalid-packet-result-correction" || edge.Phase != "worker-new-result-correct" ||
		edge.Outcome != "success" || !edge.Resumed ||
		edge.Source.ArchivePath != "task/telemetry/"+fixture.taskID+".jsonl" || !slices.Equal(edge.Source.Lines, []int{2}) {
		t.Fatalf("resolved edge = %#v", edge)
	}

	for _, ref := range index.Evidence.TaskExternal {
		if strings.Contains(ref.ArchivePath, analysisRunExternal) {
			t.Fatalf("unrelated historical run remains task-external evidence: %s", ref.ArchivePath)
		}
	}
	for _, ref := range index.Evidence.Unattributed {
		if strings.Contains(ref.ArchivePath, analysisRunEventLinked) ||
			strings.Contains(ref.ArchivePath, analysisRunDigestMatched) ||
			strings.Contains(ref.ArchivePath, analysisRunExternal) {
			t.Fatalf("explained or unrelated run files remain unattributed: %s", ref.ArchivePath)
		}
	}
	if !slices.ContainsFunc(index.Evidence.Unattributed, func(ref bundleAnalysisEvidenceRef) bool {
		return ref.ArchivePath == "current-state/diagnostics/quality-gate-runs/"+analysisRunUnmatched+"/run.json"
	}) {
		t.Fatalf("unmatched run files are not listed as unattributed: %#v", index.Evidence.Unattributed)
	}
	if !slices.ContainsFunc(index.Evidence.Task, func(ref bundleAnalysisEvidenceRef) bool {
		return ref.ArchivePath == "task/" && ref.Basis == analysisBasisTaskScopedState
	}) {
		t.Fatalf("task evidence = %#v", index.Evidence.Task)
	}
	if !slices.ContainsFunc(index.Evidence.ParentSession, func(ref bundleAnalysisEvidenceRef) bool {
		return ref.ArchivePath == "codex-parent/rollouts/"+codexTestParentThreadID+".jsonl"
	}) {
		t.Fatalf("parent evidence = %#v", index.Evidence.ParentSession)
	}
}

func TestBundleAnalysisIndexWithoutParentRolloutStaysExplicit(t *testing.T) {
	cfg, st := newBundleTestState(t)
	oldTaskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(state.WorkerRole, "opus")
	writeBundleModelCall(t, st, oldTaskID, "session-old", state.WorkerRole, "worker-new")
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}

	index := runAnalysisBundle(t, cfg, oldTaskID)
	if index.TaskID != oldTaskID {
		t.Fatalf("task = %s", index.TaskID)
	}
	if index.ParentSession.Status != codexStatusMissing {
		t.Fatalf("parent session = %#v", index.ParentSession)
	}
	if index.WaitCalls.Status != codexStatusMissing || index.TokenDelta.Status != codexStatusMissing {
		t.Fatalf("wait/tokens = %#v/%#v", index.WaitCalls, index.TokenDelta)
	}
	if index.ValidationRuns.Status != analysisStatusNotCollected || len(index.ValidationRuns.Runs) != 0 {
		t.Fatalf("validation runs = %#v", index.ValidationRuns)
	}
	if index.RolloutWindow.Status != codexStatusMissing {
		t.Fatalf("rollout window = %#v", index.RolloutWindow)
	}
	if index.Intervals.TaskExecution.Status != analysisStatusOpen || index.Intervals.TaskExecution.End != nil {
		t.Fatalf("execution interval = %#v", index.Intervals.TaskExecution)
	}
	if index.Intervals.ParentFinalization.Status != analysisStatusUnknown {
		t.Fatalf("finalization interval = %#v", index.Intervals.ParentFinalization)
	}
	if index.Intervals.SubsequentRequests.Status != analysisStatusUnknown {
		t.Fatalf("subsequent interval = %#v", index.Intervals.SubsequentRequests)
	}
	if index.Finalization.Status != analysisStatusUnknown {
		t.Fatalf("finalization delta = %#v", index.Finalization)
	}
}

func TestBundleAnalysisTokenDeltaDegradations(t *testing.T) {
	start := time.Date(2026, 8, 30, 16, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	terminal := analysisExecutionBoundary{status: analysisStatusAvailable, end: end}
	cases := []struct {
		name     string
		lines    []string
		boundary analysisExecutionBoundary
		status   string
	}{
		{
			name:     "baseline-missing",
			lines:    []string{analysisTokenCountLine(t, start.Add(time.Minute), 100, 50)},
			boundary: terminal,
			status:   analysisStatusMissing,
		},
		{
			name:     "no-in-window-observation",
			lines:    []string{analysisTokenCountLine(t, start.Add(-time.Minute), 100, 50)},
			boundary: terminal,
			status:   analysisStatusNoObservation,
		},
		{
			name: "counter-reset",
			lines: []string{
				analysisTokenCountLine(t, start.Add(-time.Minute), 100, 50),
				analysisTokenCountLine(t, start.Add(time.Minute), 40, 20),
			},
			boundary: terminal,
			status:   analysisStatusCounterReset,
		},
		{
			name: "counter-reset-recovery",
			lines: []string{
				analysisTokenCountLine(t, start.Add(-time.Minute), 100, 50),
				analysisTokenCountLine(t, start.Add(time.Minute), 40, 20),
				analysisTokenCountLine(t, start.Add(90*time.Minute), 200, 100),
			},
			boundary: terminal,
			status:   analysisStatusCounterReset,
		},
		{
			name:     "execution-boundary-unknown",
			lines:    []string{analysisTokenCountLine(t, start.Add(time.Minute), 100, 50)},
			boundary: analysisExecutionBoundary{status: analysisStatusUnknown},
			status:   analysisStatusUnknown,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rollout.jsonl")
			writeBundleFile(t, path, strings.Join(tc.lines, ""))
			scan, err := scanCodexRolloutWindow(path, start, end)
			if err != nil {
				t.Fatal(err)
			}
			delta := analysisExecutionTokenDelta(codexAssociation{ParentStatus: codexStatusIncluded}, scan, start, tc.boundary, end)
			if delta.Status != tc.status || delta.InputTokens != 0 || delta.CachedInputTokens != 0 {
				t.Fatalf("delta = %#v", delta)
			}
		})
	}
}

func newAnalysisBundleFixture(t *testing.T) analysisBundleFixture {
	t.Helper()
	cfg, st, codexHome := newCodexBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	start := stats.StartedAt.UTC()
	if err := st.SetParentCodexIdentity(codexTestParentThreadID, codexTestParentSessionID); err != nil {
		t.Fatal(err)
	}
	st.RecordRateLimit("opus")
	inWindowAt := time.Now().UTC()
	if inWindowAt.Before(start) {
		inWindowAt = start
	}

	inWindow := []string{
		analysisTokenCountLine(t, inWindowAt, 26011, 6912),
		analysisWaitRequestLine(t, inWindowAt, "wait-analysis-short", `{"yield_time_ms":1000}`),
		analysisWaitReturnLine(t, inWindowAt, "wait-analysis-short"),
		analysisRolloutLine(t, inWindowAt, "response_item", map[string]any{"type": "custom_tool_call", "name": "exec"}),
		analysisTokenCountLine(t, inWindowAt, 26511, 7912),
		analysisWaitRequestLine(t, inWindowAt, "wait-analysis-bounded", `{"yield_time_ms":60000}`),
		analysisWaitRequestLine(t, inWindowAt, "wait-analysis-unknown", ""),
	}
	preWindow := []string{
		analysisTokenCountLine(t, start.Add(-2*time.Hour), 25011, 5912),
		analysisRolloutLine(t, start.Add(-90*time.Minute), "response_item", map[string]any{"type": "function_call", "name": "wait"}),
		analysisTokenCountLine(t, start.Add(-time.Minute), 26011, 6912),
		analysisTurnLine(t, start.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
	}
	rolloutRel := analysisRolloutRel()
	writeAnalysisRollout(t, codexHome, rolloutRel, codexTestParentThreadID, start.Add(-3*time.Hour), append(preWindow, inWindow...))

	st.RecordModelCall(state.WorkerRole, "opus")
	writeAnalysisRetryModelCalls(t, st, taskID, "session-worker")

	digest := state.SnapshotDigest{Head: "head-round", IndexDigest: "index-round", WorktreeDigest: "worktree-round"}
	if err := st.AppendRoundRecord(state.RoundRecord{
		Version: 1, TaskID: taskID, WorkerPhase: "baseline", CapturedAt: inWindowAt, Snapshot: digest,
	}); err != nil {
		t.Fatal(err)
	}
	st.RecordValidation("quality-gate", "go-test", "", state.ValidationResultFail, 1, state.ValidationExitSourceTarget, 1,
		"quality-gate-runs/"+analysisRunPreviousFail+"/gate.log")
	st.RecordValidation("quality-gate", "go-test", "", state.ValidationResultFail, 1, state.ValidationExitSourceTarget, 1,
		"quality-gate-runs/"+analysisRunPreviousFail+"/gate.log")
	st.RecordValidation("quality-gate", "go-test", "", state.ValidationResultPass, 0, state.ValidationExitSourceTarget, 1,
		"quality-gate-runs/"+analysisRunEventLinked+"/gate.log")
	writeAnalysisRun(t, st, analysisRunExternal, "go-test", "pass", start.Add(-3*time.Hour), start.Add(-2*time.Hour), state.GitSnapshot{})
	writeAnalysisRun(t, st, analysisRunEventLinked, "go-test", "pass", inWindowAt, inWindowAt, state.GitSnapshot{})
	writeAnalysisRun(t, st, analysisRunDigestMatched, "go-test-race", "pass", inWindowAt, inWindowAt, state.GitSnapshot{
		Head: digest.Head, IndexDigest: digest.IndexDigest, WorktreeDigest: digest.WorktreeDigest,
	})
	writeAnalysisRun(t, st, analysisRunUnmatched, "go-test", "fail", inWindowAt, inWindowAt, state.GitSnapshot{})

	guardedBefore := readAnalysisGuardedFiles(t, st, taskID)
	index, archive := runAnalysisBundleFull(t, cfg, "")
	var manifest bundleManifest
	if err := json.Unmarshal(archive["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	return analysisBundleFixture{
		st:            st,
		taskID:        taskID,
		start:         start,
		inWindow:      inWindow,
		rolloutRel:    rolloutRel,
		guardedBefore: guardedBefore,
		manifest:      manifest,
		index:         index,
		codexHome:     codexHome,
	}
}

func newAnalysisTerminalTask(t *testing.T) analysisTerminalTask {
	t.Helper()
	cfg, st, codexHome := newCodexBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetParentCodexIdentity(codexTestParentThreadID, codexTestParentSessionID); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Add(-2 * time.Hour)
	completeAt := start.Add(30 * time.Minute)
	analysisRetireCurrentTask(t, st, taskID, start, completeAt)
	return analysisTerminalTask{cfg: cfg, st: st, codexHome: codexHome, taskID: taskID, start: start, completeAt: completeAt}
}

func analysisRetireCurrentTask(t *testing.T, st *state.StateStore, taskID string, start, completeAt time.Time) {
	t.Helper()
	st.UpdateTaskStats(func(stats *state.TaskStats) {
		stats.StartedAt = start
		stats.Status = state.TaskStatusComplete
	})
	if err := st.AppendTaskLifecycle(state.TaskLifecycleRecord{
		Version:   1,
		TaskID:    taskID,
		Timestamp: completeAt,
		From:      string(state.TaskStatusActive),
		To:        string(state.TaskStatusComplete),
	}); err != nil {
		t.Fatal(err)
	}
}

func analysisPhaseRolloutLines(t *testing.T, start, completeAt time.Time) []string {
	t.Helper()
	return []string{
		analysisTokenCountLine(t, start.Add(-time.Minute), 1000, 500),
		analysisTurnLine(t, start.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
		analysisTokenCountLine(t, start.Add(time.Minute), 1400, 700),
		analysisRolloutLine(t, start.Add(2*time.Minute), "response_item", map[string]any{"type": "function_call", "name": "wait"}),
		analysisTokenCountLine(t, completeAt.Add(-time.Second), 2000, 1000),
		analysisTokenCountLine(t, completeAt.Add(30*time.Second), 2600, 1300),
		analysisRolloutLine(t, completeAt.Add(time.Minute), "response_item", map[string]any{"type": "function_call", "name": "wait"}),
		analysisTurnLine(t, completeAt.Add(2*time.Minute), codexRolloutTaskCompleteType, analysisOwningTurnID),
		analysisTurnLine(t, completeAt.Add(5*time.Minute), codexRolloutTaskStartedType, analysisLaterTurnID),
		analysisTokenCountLine(t, completeAt.Add(6*time.Minute+10*time.Second), 3200, 1600),
		analysisRolloutLine(t, completeAt.Add(6*time.Minute+20*time.Second), "response_item", map[string]any{"type": "function_call", "name": "wait"}),
		analysisTurnLine(t, completeAt.Add(7*time.Minute), codexRolloutTaskCompleteType, analysisLaterTurnID),
		analysisTurnLine(t, completeAt.Add(10*time.Minute), codexRolloutTaskStartedType, analysisOpenTurnID),
		analysisTokenCountLine(t, completeAt.Add(11*time.Minute), 3800, 1900),
	}
}

func analysisRolloutRel() string {
	return "sessions/2026/08/30/rollout-live-" + codexTestParentThreadID + ".jsonl"
}

func analysisIntervalEqual(left, right bundleAnalysisInterval) bool {
	if left.Status != right.Status || left.EndBasis != right.EndBasis {
		return false
	}
	return analysisOptionalTimestampEqual(left.Start, right.Start) && analysisOptionalTimestampEqual(left.End, right.End)
}

func analysisOptionalTimestampEqual(left, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func runAnalysisBundle(t *testing.T, cfg config.AppConfig, taskID string) bundleAnalysisIndex {
	t.Helper()
	index, _ := runAnalysisBundleFull(t, cfg, taskID)
	return index
}

func runAnalysisBundleFull(t *testing.T, cfg config.AppConfig, taskID string) (bundleAnalysisIndex, map[string][]byte) {
	t.Helper()
	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle, Payload: taskID}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	archive := readBundleArchive(t, analysisArchivePath(t, stdout))
	var index bundleAnalysisIndex
	if err := json.Unmarshal(archive[bundleAnalysisEntryPath], &index); err != nil {
		t.Fatal(err)
	}
	return index, archive
}

func analysisArchivedAt(t *testing.T, st *state.StateStore, taskID string) time.Time {
	t.Helper()
	allStats, err := st.AllTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	for _, stats := range allStats {
		if stats.TaskID == taskID {
			if stats.ArchivedAt == nil {
				t.Fatalf("task %s is not archived: %#v", taskID, stats)
			}
			return stats.ArchivedAt.UTC()
		}
	}
	t.Fatalf("task %s is missing from stats", taskID)
	return time.Time{}
}

func appendAnalysisRolloutLines(t *testing.T, home, rel string, lines []string) {
	t.Helper()
	path := filepath.Join(home, filepath.FromSlash(rel))
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(strings.Join(lines, "")); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func analysisArchivePath(t *testing.T, stdout bytes.Buffer) string {
	t.Helper()
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	return output.ArchivePath
}

func analysisTurnLine(t *testing.T, timestamp time.Time, eventType, turnID string) string {
	t.Helper()
	return analysisRolloutLine(t, timestamp, "event_msg", map[string]any{"type": eventType, "turn_id": turnID})
}

func analysisRolloutLine(t *testing.T, timestamp time.Time, recordType string, payload map[string]any) string {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"timestamp": timestamp.UTC().Format(time.RFC3339Nano),
		"type":      recordType,
		"payload":   payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded) + "\n"
}

func analysisTokenCountLine(t *testing.T, timestamp time.Time, input, cached int64) string {
	t.Helper()
	return analysisRolloutLine(t, timestamp, "event_msg", map[string]any{
		"type": "token_count",
		"info": map[string]any{
			"total_token_usage": map[string]any{
				"input_tokens":        input,
				"cached_input_tokens": cached,
			},
		},
	})
}

func writeAnalysisRollout(t *testing.T, home, rel, threadID string, metaTimestamp time.Time, lines []string) {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"timestamp": metaTimestamp.UTC().Format(time.RFC3339Nano),
		"type":      "session_meta",
		"payload": map[string]any{
			"id": threadID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeBundleFile(t, filepath.Join(home, filepath.FromSlash(rel)), string(encoded)+"\n"+strings.Join(lines, ""))
}

func writeAnalysisRetryModelCalls(t *testing.T, st *state.StateStore, taskID, sessionID string) {
	t.Helper()
	now := time.Now().UTC()
	base := state.ModelCallLog{
		TaskID:      taskID,
		CallType:    state.CallTypeTask,
		CallID:      "call-analysis-original",
		SessionID:   sessionID,
		Role:        state.WorkerRole,
		Phase:       "worker-new",
		ModelAlias:  "opus",
		StartedAt:   now,
		CompletedAt: now,
		Outcome:     "invalid_packet",
	}
	st.RecordModelCallLog(base)
	base.CallID = "call-analysis-retry"
	base.Phase = "worker-new-result-correct"
	base.Outcome = "success"
	base.Resumed = true
	base.RetryOf = "call-analysis-original"
	base.RetryReason = "invalid-packet-result-correction"
	st.RecordModelCallLog(base)
}

func analysisWaitRequestLine(t *testing.T, timestamp time.Time, callID, arguments string) string {
	t.Helper()
	payload := map[string]any{"type": "function_call", "name": "wait", "call_id": callID}
	if arguments != "" {
		payload["arguments"] = arguments
	}
	return analysisRolloutLine(t, timestamp, "response_item", payload)
}

func analysisWaitReturnLine(t *testing.T, timestamp time.Time, callID string) string {
	t.Helper()
	return analysisRolloutLine(t, timestamp, "response_item", map[string]any{"type": "function_call_output", "call_id": callID})
}

func writeAnalysisRun(t *testing.T, st *state.StateStore, runID, form, status string, startedAt, completedAt time.Time, snapshot state.GitSnapshot) {
	t.Helper()
	completed := completedAt.UTC()
	record := qualityGateRunRecord{
		ValidationRunID: runID,
		Form:            form,
		Repository:      "/repo",
		WorkingDir:      "/repo",
		Head:            snapshot.Head,
		IndexDigest:     snapshot.IndexDigest,
		WorktreeDigest:  snapshot.WorktreeDigest,
		StartedAt:       startedAt.UTC(),
		CompletedAt:     &completed,
		Status:          status,
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	writeBundleFile(t, st.Path(filepath.Join(qualityGateRunDirectory, runID, qualityGateRunFile)), string(encoded)+"\n")
}

func readAnalysisGuardedFiles(t *testing.T, st *state.StateStore, taskID string) []byte {
	t.Helper()
	var combined bytes.Buffer
	for _, path := range []string{
		st.Path("task-stats.json"),
		st.TaskEventLogPath(taskID),
		st.RoundLogPath(taskID),
		st.ModelCallLogPath(taskID),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		combined.Write(data)
	}
	return combined.Bytes()
}
