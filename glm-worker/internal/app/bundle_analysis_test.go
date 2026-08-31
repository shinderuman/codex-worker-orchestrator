package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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

const analysisRunExternal = "11111111111111111111111111111111"

const analysisRunEventLinked = "22222222222222222222222222222222"

const analysisRunPreviousFail = "55555555555555555555555555555555"

const analysisRunDigestMatched = "33333333333333333333333333333333"

const analysisRunUnmatched = "44444444444444444444444444444444"

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
	if index.Window.Start != fixture.start.Format(time.RFC3339Nano) || index.Window.EndBasis != analysisWindowEndBasisBundleTime {
		t.Fatalf("window = %#v", index.Window)
	}
	if index.ParentSession.Status != codexStatusIncluded || index.ParentSession.ThreadID != codexTestParentThreadID {
		t.Fatalf("parent session = %#v", index.ParentSession)
	}
	if index.WaitCalls.Status != analysisStatusCounted || index.WaitCalls.Count != 3 {
		t.Fatalf("wait calls = %#v", index.WaitCalls)
	}
	if index.TokenDelta.Status != analysisStatusAvailable ||
		index.TokenDelta.InputTokens != 500 || index.TokenDelta.CachedInputTokens != 1000 {
		t.Fatalf("token delta = %#v", index.TokenDelta)
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
		analysisRunExternal:      {analysisAttributionExternal, []string{analysisBasisOutsideWindow}},
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
	index := newAnalysisBundleFixture(t).index

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

	if !slices.ContainsFunc(index.Evidence.TaskExternal, func(ref bundleAnalysisEvidenceRef) bool {
		return ref.ArchivePath == "current-state/diagnostics/quality-gate-runs/"+analysisRunExternal+"/run.json"
	}) {
		t.Fatalf("task external = %#v", index.Evidence.TaskExternal)
	}
	for _, ref := range index.Evidence.Unattributed {
		if strings.Contains(ref.ArchivePath, analysisRunEventLinked) ||
			strings.Contains(ref.ArchivePath, analysisRunDigestMatched) ||
			strings.Contains(ref.ArchivePath, analysisRunExternal) {
			t.Fatalf("explained run files remain unattributed: %s", ref.ArchivePath)
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

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle, Payload: oldTaskID}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	archive := readBundleArchive(t, analysisArchivePath(t, stdout))
	var index bundleAnalysisIndex
	if err := json.Unmarshal(archive[bundleAnalysisEntryPath], &index); err != nil {
		t.Fatal(err)
	}
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
}

func TestBundleAnalysisTokenDeltaDegradations(t *testing.T) {
	start := time.Date(2026, 8, 30, 16, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	cases := []struct {
		name   string
		lines  []string
		status string
	}{
		{
			name:   "baseline-missing",
			lines:  []string{analysisTokenCountLine(t, start.Add(time.Minute), 100, 50)},
			status: analysisStatusMissing,
		},
		{
			name:   "no-in-window-observation",
			lines:  []string{analysisTokenCountLine(t, start.Add(-time.Minute), 100, 50)},
			status: analysisStatusNoObservation,
		},
		{
			name: "counter-reset",
			lines: []string{
				analysisTokenCountLine(t, start.Add(-time.Minute), 100, 50),
				analysisTokenCountLine(t, start.Add(time.Minute), 40, 20),
			},
			status: analysisStatusCounterReset,
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
			delta := analysisTokenDelta(codexAssociation{ParentStatus: codexStatusIncluded}, scan)
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

	inWindow := []string{
		analysisTokenCountLine(t, start.Add(100*time.Microsecond), 26011, 6912),
		analysisRolloutLine(t, start.Add(200*time.Microsecond), "response_item", map[string]any{"type": "function_call", "name": "wait"}),
		analysisRolloutLine(t, start.Add(300*time.Microsecond), "response_item", map[string]any{"type": "custom_tool_call", "name": "exec"}),
		analysisTokenCountLine(t, start.Add(400*time.Microsecond), 26511, 7912),
		analysisRolloutLine(t, start.Add(500*time.Microsecond), "response_item", map[string]any{"type": "function_call", "name": "wait"}),
		analysisRolloutLine(t, start.Add(600*time.Microsecond), "response_item", map[string]any{"type": "function_call", "name": "wait"}),
	}
	preWindow := []string{
		analysisTokenCountLine(t, start.Add(-2*time.Hour), 25011, 5912),
		analysisRolloutLine(t, start.Add(-90*time.Minute), "response_item", map[string]any{"type": "function_call", "name": "wait"}),
		analysisTokenCountLine(t, start.Add(-time.Minute), 26011, 6912),
	}
	rolloutRel := "sessions/2026/08/30/rollout-live-" + codexTestParentThreadID + ".jsonl"
	writeAnalysisRollout(t, codexHome, rolloutRel, codexTestParentThreadID, start.Add(-3*time.Hour), append(preWindow, inWindow...))

	st.RecordModelCall(state.WorkerRole, "opus")
	writeAnalysisModelCall(t, st, taskID, "session-worker", false)
	writeAnalysisModelCall(t, st, taskID, "session-worker", true)

	digest := state.SnapshotDigest{Head: "head-round", IndexDigest: "index-round", WorktreeDigest: "worktree-round"}
	if err := st.AppendRoundRecord(state.RoundRecord{
		Version: 1, TaskID: taskID, WorkerPhase: "baseline", CapturedAt: start.Add(time.Millisecond), Snapshot: digest,
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
	writeAnalysisRun(t, st, analysisRunEventLinked, "go-test", "pass", start.Add(time.Millisecond), start.Add(2*time.Millisecond), state.GitSnapshot{})
	writeAnalysisRun(t, st, analysisRunDigestMatched, "go-test-race", "pass", start.Add(3*time.Millisecond), start.Add(4*time.Millisecond), state.GitSnapshot{
		Head: digest.Head, IndexDigest: digest.IndexDigest, WorktreeDigest: digest.WorktreeDigest,
	})
	writeAnalysisRun(t, st, analysisRunUnmatched, "go-test", "fail", start.Add(5*time.Millisecond), start.Add(6*time.Millisecond), state.GitSnapshot{})

	guardedBefore := readAnalysisGuardedFiles(t, st, taskID)
	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	archive := readBundleArchive(t, analysisArchivePath(t, stdout))
	var manifest bundleManifest
	if err := json.Unmarshal(archive["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	var index bundleAnalysisIndex
	if err := json.Unmarshal(archive[bundleAnalysisEntryPath], &index); err != nil {
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

func analysisArchivePath(t *testing.T, stdout bytes.Buffer) string {
	t.Helper()
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	return output.ArchivePath
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

func writeAnalysisModelCall(t *testing.T, st *state.StateStore, taskID, sessionID string, resumed bool) {
	t.Helper()
	now := time.Now().UTC()
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID:      taskID,
		CallType:    state.CallTypeTask,
		SessionID:   sessionID,
		Role:        state.WorkerRole,
		Phase:       "worker-new",
		ModelAlias:  "opus",
		StartedAt:   now,
		CompletedAt: now,
		Outcome:     "success",
		Resumed:     resumed,
	})
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
