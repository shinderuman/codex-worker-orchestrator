package app

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type analysisRelationRecord struct {
	callID      string
	phase       string
	outcome     string
	retryOf     string
	retryReason string
	resumed     bool
}

func writeAnalysisRelationTelemetry(t *testing.T, st *state.StateStore, taskID string, records []analysisRelationRecord) {
	t.Helper()
	now := time.Now().UTC()
	for _, row := range records {
		st.RecordModelCallLog(state.ModelCallLog{
			TaskID:      taskID,
			CallType:    state.CallTypeTask,
			SessionID:   "session-relations",
			Role:        state.WorkerRole,
			ModelAlias:  "opus",
			StartedAt:   now,
			CompletedAt: now,
			CallID:      row.callID,
			Phase:       row.phase,
			Outcome:     row.outcome,
			RetryOf:     row.retryOf,
			RetryReason: row.retryReason,
			Resumed:     row.resumed,
		})
	}
}

func TestAnalysisModelCallRelations(t *testing.T) {
	_, st := newBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	writeAnalysisRelationTelemetry(t, st, taskID, []analysisRelationRecord{
		{callID: "call-original", phase: "worker-new", outcome: "invalid_packet"},
		{callID: "call-retry", phase: "worker-new-result-correct", outcome: "success",
			retryOf: "call-original", retryReason: "invalid-packet-result-correction", resumed: true},
		{callID: "call-retry", phase: "worker-new-result-correct", outcome: "success",
			retryOf: "call-original", retryReason: "invalid-packet-result-correction", resumed: true},
		{callID: "call-auto-fix", phase: "worker-auto-fix-1", outcome: "success", resumed: true},
		{callID: "call-reason-only", phase: "worker-new", outcome: "transient_error",
			retryReason: "transient-provider-failure:rate-limited"},
		{callID: "call-dangling", phase: "worker-new-result-correct", outcome: "success",
			retryOf: "call-absent", retryReason: "invalid-packet-result-correction", resumed: true},
	})
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID: taskID, CallType: state.CallTypeTask, SessionID: "session-relations",
		Role: state.WorkerRole, ModelAlias: "opus", Version: state.ModelCallLogVersion - 1,
		CallID: "call-legacy", Phase: "worker-new", Outcome: "success", RetryOf: "call-original",
		StartedAt: time.Now().UTC(), CompletedAt: time.Now().UTC(),
	})
	unidentifiedLine := `{"version":` + strconv.Itoa(state.ModelCallLogVersion) + `,"call_id":"","phase":"worker-new","outcome":"success","resumed":true}` + "\n"
	appendAnalysisTelemetryLine(t, st.ModelCallLogPath(taskID), unidentifiedLine)

	scan := scanAnalysisTelemetryCalls(st, taskID)
	if scan.status != analysisStatusAvailable {
		t.Fatalf("scan status = %s", scan.status)
	}
	relations := analysisModelCallRelations(scan, taskID)

	if len(relations.Resolved) != 1 {
		t.Fatalf("resolved = %#v", relations.Resolved)
	}
	resolved := relations.Resolved[0]
	if resolved.CallID != "call-retry" || resolved.RetryOf != "call-original" ||
		resolved.RetryReason != "invalid-packet-result-correction" ||
		resolved.Phase != "worker-new-result-correct" || resolved.Outcome != "success" || !resolved.Resumed ||
		resolved.Source.ArchivePath != "task/telemetry/"+taskID+".jsonl" ||
		!slices.Equal(resolved.Source.Lines, []int{2, 3}) {
		t.Fatalf("resolved edge = %#v", resolved)
	}
	if len(relations.Dangling) != 1 || relations.Dangling[0].CallID != "call-dangling" ||
		relations.Dangling[0].RetryOf != "call-absent" ||
		!slices.Equal(relations.Dangling[0].Source.Lines, []int{6}) {
		t.Fatalf("dangling = %#v", relations.Dangling)
	}
	if len(relations.Unlinked) != 2 {
		t.Fatalf("unlinked = %#v", relations.Unlinked)
	}
	autoFix := relations.Unlinked[0]
	if autoFix.CallID != "call-auto-fix" || !autoFix.Resumed || autoFix.RetryReason != "" ||
		autoFix.Phase != "worker-auto-fix-1" || !slices.Equal(autoFix.Source.Lines, []int{4}) {
		t.Fatalf("auto fix unlinked = %#v", autoFix)
	}
	reasonOnly := relations.Unlinked[1]
	if reasonOnly.CallID != "call-reason-only" || reasonOnly.Resumed ||
		reasonOnly.RetryReason != "transient-provider-failure:rate-limited" ||
		!slices.Equal(reasonOnly.Source.Lines, []int{5}) {
		t.Fatalf("reason only unlinked = %#v", reasonOnly)
	}
	if len(relations.Ambiguous) != 0 || len(relations.DuplicateCallIDs) != 0 {
		t.Fatalf("ambiguous/duplicates = %#v/%#v", relations.Ambiguous, relations.DuplicateCallIDs)
	}
	resumed := analysisResumedModelCalls(scan)
	if resumed.Status != analysisStatusAvailable || resumed.Count != 3 || resumed.Count == len(relations.Resolved) {
		t.Fatalf("resumed count = %#v", resumed)
	}
}

func TestAnalysisModelCallRelationsConflicted(t *testing.T) {
	_, st := newBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	writeAnalysisRelationTelemetry(t, st, taskID, []analysisRelationRecord{
		{callID: "call-original", phase: "worker-new", outcome: "invalid_packet"},
		{callID: "call-conflict", phase: "worker-new", outcome: "invalid_packet"},
		{callID: "call-conflict", phase: "worker-new", outcome: "success"},
		{callID: "call-conflict-target", phase: "worker-new-result-correct", outcome: "success",
			retryOf: "call-conflict", retryReason: "invalid-packet-result-correction", resumed: true},
		{callID: "call-src-conflict", phase: "worker-new", outcome: "invalid_packet",
			retryOf: "call-original", retryReason: "invalid-packet-result-correction"},
		{callID: "call-src-conflict", phase: "worker-new-result-correct", outcome: "success",
			retryOf: "call-original", retryReason: "invalid-packet-result-correction"},
		{callID: "call-unlinked-conflict", phase: "worker-new", outcome: "transient_error", resumed: true},
		{callID: "call-unlinked-conflict", phase: "worker-fix", outcome: "success",
			retryReason: "transient-provider-failure:rate-limited", resumed: true},
		{callID: "call-both-conflict", phase: "worker-new", outcome: "invalid_packet",
			retryOf: "call-conflict", retryReason: "invalid-packet-result-correction"},
		{callID: "call-both-conflict", phase: "worker-new-result-correct", outcome: "success",
			retryOf: "call-conflict", retryReason: "invalid-packet-result-correction"},
	})

	scan := scanAnalysisTelemetryCalls(st, taskID)
	if scan.status != analysisStatusAvailable {
		t.Fatalf("scan status = %s", scan.status)
	}
	relations := analysisModelCallRelations(scan, taskID)

	wantDuplicates := []bundleAnalysisDuplicateCalls{
		{CallID: "call-conflict", Lines: []int{2, 3}},
		{CallID: "call-src-conflict", Lines: []int{5, 6}},
		{CallID: "call-unlinked-conflict", Lines: []int{7, 8}},
		{CallID: "call-both-conflict", Lines: []int{9, 10}},
	}
	if len(relations.DuplicateCallIDs) != len(wantDuplicates) {
		t.Fatalf("duplicate call ids = %#v", relations.DuplicateCallIDs)
	}
	for i, want := range wantDuplicates {
		got := relations.DuplicateCallIDs[i]
		if got.CallID != want.CallID || !slices.Equal(got.Lines, want.Lines) {
			t.Fatalf("duplicate %d = %#v want %#v", i, got, want)
		}
	}

	if len(relations.Ambiguous) != 7 {
		t.Fatalf("ambiguous = %#v", relations.Ambiguous)
	}
	targetConflicted := relations.Ambiguous[0]
	if targetConflicted.CallID != "call-conflict-target" || targetConflicted.RetryOf != "call-conflict" ||
		targetConflicted.RetryReason != "invalid-packet-result-correction" ||
		targetConflicted.Phase != "worker-new-result-correct" || targetConflicted.Outcome != "success" ||
		!targetConflicted.Resumed ||
		!slices.Equal(targetConflicted.Ambiguity, []string{analysisAmbiguityTargetConflicted}) ||
		!slices.Equal(targetConflicted.Source.Lines, []int{4}) {
		t.Fatalf("target conflicted relation = %#v", targetConflicted)
	}
	for i, wantLines := range [][]int{{5}, {6}} {
		sourceConflicted := relations.Ambiguous[i+1]
		if sourceConflicted.CallID != "call-src-conflict" || sourceConflicted.RetryOf != "call-original" ||
			!slices.Equal(sourceConflicted.Ambiguity, []string{analysisAmbiguitySourceConflicted}) ||
			!slices.Equal(sourceConflicted.Source.Lines, wantLines) {
			t.Fatalf("source conflicted relation %d = %#v", i, sourceConflicted)
		}
	}
	resumedCandidate := relations.Ambiguous[3]
	if resumedCandidate.CallID != "call-unlinked-conflict" || resumedCandidate.RetryOf != "" ||
		resumedCandidate.RetryReason != "" || !resumedCandidate.Resumed ||
		resumedCandidate.Phase != "worker-new" || resumedCandidate.Outcome != "transient_error" ||
		!slices.Equal(resumedCandidate.Ambiguity, []string{analysisAmbiguitySourceConflicted}) ||
		!slices.Equal(resumedCandidate.Source.Lines, []int{7}) {
		t.Fatalf("resumed candidate relation = %#v", resumedCandidate)
	}
	reasonCandidate := relations.Ambiguous[4]
	if reasonCandidate.CallID != "call-unlinked-conflict" || reasonCandidate.RetryOf != "" ||
		reasonCandidate.RetryReason != "transient-provider-failure:rate-limited" ||
		reasonCandidate.Phase != "worker-fix" || reasonCandidate.Outcome != "success" ||
		!slices.Equal(reasonCandidate.Ambiguity, []string{analysisAmbiguitySourceConflicted}) ||
		!slices.Equal(reasonCandidate.Source.Lines, []int{8}) {
		t.Fatalf("reason candidate relation = %#v", reasonCandidate)
	}
	for i, wantLines := range [][]int{{9}, {10}} {
		bothConflicted := relations.Ambiguous[i+5]
		if bothConflicted.CallID != "call-both-conflict" || bothConflicted.RetryOf != "call-conflict" ||
			!slices.Equal(bothConflicted.Ambiguity, []string{analysisAmbiguitySourceConflicted, analysisAmbiguityTargetConflicted}) ||
			!slices.Equal(bothConflicted.Source.Lines, wantLines) {
			t.Fatalf("both conflicted relation %d = %#v", i, bothConflicted)
		}
	}
	if len(relations.Resolved) != 0 || len(relations.Dangling) != 0 || len(relations.Unlinked) != 0 {
		t.Fatalf("conflicted relations leaked into definitive lists: %#v", relations)
	}
	resumed := analysisResumedModelCalls(scan)
	if resumed.Status != analysisStatusAvailable || resumed.Count != 2 {
		t.Fatalf("resumed count = %#v", resumed)
	}
}

func TestAnalysisModelCallRelationsMissingTelemetry(t *testing.T) {
	_, st := newBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		corrupt func(t *testing.T, st *state.StateStore, taskID string)
	}{
		{
			name: "file-missing",
		},
		{
			name: "invalid-record",
			corrupt: func(t *testing.T, st *state.StateStore, taskID string) {
				t.Helper()
				st.RecordModelCallLog(state.ModelCallLog{
					TaskID: taskID, CallType: state.CallTypeTask, CallID: "call-partial",
					Phase: "worker-new", Outcome: "success", Resumed: true,
					StartedAt: time.Now().UTC(), CompletedAt: time.Now().UTC(),
				})
				appendAnalysisTelemetryLine(t, st.ModelCallLogPath(taskID), "{not-json}\n")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.corrupt != nil {
				tc.corrupt(t, st, taskID)
			}
			scan := scanAnalysisTelemetryCalls(st, taskID)
			if scan.status != analysisStatusMissing {
				t.Fatalf("scan status = %s", scan.status)
			}
			relations := analysisModelCallRelations(scan, taskID)
			if relations.Status != analysisStatusMissing || relations.Resolved != nil ||
				relations.Dangling != nil || relations.Unlinked != nil || relations.DuplicateCallIDs != nil {
				t.Fatalf("relations = %#v", relations)
			}
			if count := analysisResumedModelCalls(scan); count.Status != analysisStatusMissing || count.Count != 0 {
				t.Fatalf("resumed count = %#v", count)
			}
		})
	}
}

func TestAnalysisResumedModelCallConflicts(t *testing.T) {
	cases := []struct {
		name      string
		records   []analysisRelationRecord
		wantCount int
		wantState string
	}{
		{
			name: "conflicted-resumed-consistent",
			records: []analysisRelationRecord{
				{callID: "call-consistent", phase: "worker-new", outcome: "invalid_packet", resumed: true},
				{callID: "call-consistent", phase: "worker-fix", outcome: "success", resumed: true},
				{callID: "call-plain", phase: "worker-new", outcome: "success", resumed: true},
			},
			wantCount: 2,
			wantState: analysisStatusAvailable,
		},
		{
			name: "conflicted-resumed-mixed",
			records: []analysisRelationRecord{
				{callID: "call-plain", phase: "worker-new", outcome: "success", resumed: true},
				{callID: "call-mixed", phase: "worker-new", outcome: "success", resumed: true},
				{callID: "call-mixed", phase: "worker-fix", outcome: "success"},
			},
			wantCount: 0,
			wantState: analysisStatusUnknown,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, st := newBundleTestState(t)
			taskID, err := st.StartNewTask()
			if err != nil {
				t.Fatal(err)
			}
			writeAnalysisRelationTelemetry(t, st, taskID, tc.records)
			scan := scanAnalysisTelemetryCalls(st, taskID)
			count := analysisResumedModelCalls(scan)
			if count.Status != tc.wantState || count.Count != tc.wantCount {
				t.Fatalf("resumed count = %#v want %s/%d", count, tc.wantState, tc.wantCount)
			}
		})
	}
}

func TestAnalysisWaitCallsDetailed(t *testing.T) {
	start := time.Date(2026, 8, 31, 19, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	at := start.Add(time.Minute)
	lines := []string{
		analysisWaitRequestLine(t, at, "wait-short", `{"yield_time_ms":1000}`),
		analysisWaitReturnLine(t, at, "wait-short"),
		analysisWaitRequestLine(t, at, "wait-bounded", `{"yield_time_ms":60000}`),
		analysisWaitRequestLine(t, at, "wait-long", `{"yield_time_ms":21600000}`),
		analysisWaitRequestLine(t, at, "wait-missing-yield", ""),
		analysisWaitRequestLine(t, at, "wait-invalid-yield", `{"yield_time_ms":"1000"}`),
		analysisWaitRequestLine(t, at, "wait-negative-yield", `{"yield_time_ms":-5}`),
		analysisWaitRequestLine(t, at, "wait-no-return", `{"yield_time_ms":2000}`),
		analysisWaitRequestLine(t, at, "wait-duplicate", `{"yield_time_ms":3000}`),
		analysisWaitReturnLine(t, at, "wait-duplicate"),
		analysisWaitRequestLine(t, at, "wait-duplicate", `{"yield_time_ms":3000}`),
		analysisWaitRequestLine(t, at, "wait-conflict", `{"yield_time_ms":4000}`),
		analysisWaitRequestLine(t, at, "wait-conflict", `{"yield_time_ms":5000}`),
		analysisWaitRequestLine(t, at, "wait-return-duplicate", `{"yield_time_ms":6000}`),
		analysisWaitReturnLine(t, at, "wait-return-duplicate"),
		analysisWaitReturnLine(t, at, "wait-return-duplicate"),
		analysisWaitRequestLine(t, at, "", `{"yield_time_ms":7000}`),
		analysisWaitRequestLine(t, at.Add(-2*time.Hour), "wait-before-window", `{"yield_time_ms":1000}`),
		analysisWaitReturnLine(t, at.Add(-2*time.Hour), "wait-before-window"),
	}
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeBundleFile(t, path, strings.Join(lines, ""))
	scan, err := scanCodexRolloutWindow(path, start, end)
	if err != nil {
		t.Fatal(err)
	}

	waits := analysisWaitCalls(codexAssociation{ParentStatus: codexStatusIncluded}, scan, start,
		analysisExecutionBoundary{status: analysisStatusAvailable, end: end}, end)

	if waits.Status != analysisStatusCounted {
		t.Fatalf("wait calls = %#v", waits)
	}
	byCall := map[string]bundleAnalysisWaitCall{}
	anonymous := make([]bundleAnalysisWaitCall, 0)
	for _, call := range waits.Calls {
		if call.CallID == "" {
			anonymous = append(anonymous, call)
			continue
		}
		byCall[call.CallID] = call
	}
	wantClasses := map[string]struct {
		class   string
		yield   *float64
		returns []int
	}{
		"wait-short":          {analysisWaitYieldClassShort, float64Ptr(1000), []int{2}},
		"wait-bounded":        {analysisWaitYieldClassBounded, float64Ptr(60000), nil},
		"wait-long":           {analysisWaitYieldClassLong, float64Ptr(21600000), nil},
		"wait-missing-yield":  {analysisStatusUnknown, nil, nil},
		"wait-invalid-yield":  {analysisStatusUnknown, nil, nil},
		"wait-negative-yield": {analysisStatusUnknown, nil, nil},
		"wait-no-return":      {analysisWaitYieldClassShort, float64Ptr(2000), nil},
	}
	for callID, want := range wantClasses {
		call, ok := byCall[callID]
		if !ok {
			t.Fatalf("wait %s is missing from calls: %#v", callID, waits.Calls)
		}
		if call.YieldClass != want.class {
			t.Fatalf("wait %s class = %s want %s", callID, call.YieldClass, want.class)
		}
		if want.yield == nil && call.RequestedYieldMS != nil || want.yield != nil && call.RequestedYieldMS == nil {
			t.Fatalf("wait %s yield = %#v want %#v", callID, call.RequestedYieldMS, want.yield)
		}
		if want.yield != nil && *call.RequestedYieldMS != *want.yield {
			t.Fatalf("wait %s yield = %#v want %#v", callID, *call.RequestedYieldMS, *want.yield)
		}
		if !slices.Equal(call.ReturnLines, want.returns) {
			t.Fatalf("wait %s returns = %#v want %#v", callID, call.ReturnLines, want.returns)
		}
	}

	duplicate, ok := byCall["wait-duplicate"]
	if !ok || !slices.Equal(duplicate.RequestLines, []int{9, 11}) || !slices.Equal(duplicate.ReturnLines, []int{10}) {
		t.Fatalf("collapsed duplicate wait = %#v", duplicate)
	}
	if _, conflicted := byCall["wait-conflict"]; conflicted {
		t.Fatalf("conflicting wait stayed in calls: %#v", byCall["wait-conflict"])
	}
	if _, conflicted := byCall["wait-return-duplicate"]; conflicted {
		t.Fatalf("return duplicated wait stayed in calls: %#v", byCall["wait-return-duplicate"])
	}
	if _, before := byCall["wait-before-window"]; before {
		t.Fatalf("out of window wait stayed in calls: %#v", byCall["wait-before-window"])
	}
	if len(anonymous) != 1 || anonymous[0].YieldClass != analysisWaitYieldClassShort ||
		anonymous[0].RequestedYieldMS == nil || *anonymous[0].RequestedYieldMS != 7000 {
		t.Fatalf("anonymous wait = %#v", anonymous)
	}

	if waits.Count != len(waits.Calls) {
		t.Fatalf("count = %d calls = %d", waits.Count, len(waits.Calls))
	}
	if len(waits.DuplicateCallIDs) != 2 {
		t.Fatalf("wait duplicates = %#v", waits.DuplicateCallIDs)
	}
	for _, conflict := range waits.DuplicateCallIDs {
		switch conflict.CallID {
		case "wait-conflict":
			if !slices.Equal(conflict.RequestLines, []int{12, 13}) || len(conflict.ReturnLines) != 0 {
				t.Fatalf("conflict duplicate = %#v", conflict)
			}
		case "wait-return-duplicate":
			if !slices.Equal(conflict.RequestLines, []int{14}) || !slices.Equal(conflict.ReturnLines, []int{15, 16}) {
				t.Fatalf("return duplicate = %#v", conflict)
			}
		default:
			t.Fatalf("unexpected wait duplicate = %#v", conflict)
		}
	}
}

func appendAnalysisTelemetryLine(t *testing.T, path, line string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(line); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func float64Ptr(value float64) *float64 {
	return &value
}
