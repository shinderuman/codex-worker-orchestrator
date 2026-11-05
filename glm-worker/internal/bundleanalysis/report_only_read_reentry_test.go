package bundleanalysis

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const reportOnlyReadFixtureTranscript = `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"r1","name":"Read","input":{"file_path":"/private/inventory/secret.txt","offset":0,"limit":1}},{"type":"tool_use","id":"r2","name":"Read","input":{"file_path":"/private/inventory/secret.txt","offset":1,"limit":1}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"r1","content":"secret data"},{"type":"tool_result","tool_use_id":"r2","content":"secret data"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"r3","name":"Read","input":{"file_path":"/private/inventory/secret.txt","offset":2,"limit":1}},{"type":"tool_use","id":"r4","name":"Read","input":{"file_path":"/other/file.txt","offset":0,"limit":3}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"r3","content":"secret data"},{"type":"tool_result","tool_use_id":"r4","content":"other content"}]}}
{"type":"result","subtype":"error","duration_api_ms":730149}
`

func reportOnlyReadFixtureCall() state.ModelCallLog {
	return state.ModelCallLog{
		CallID:              "a8cdeb86-585e-48e7-a92b-9365d37f6abe",
		CallType:            state.CallTypeTask,
		Phase:               "worker-report-only-1-result-correct",
		Role:                state.WorkerRole,
		Outcome:             "rate_limited",
		WallDurationMS:      923440,
		ClaudeAPIDurationMS: 730149,
	}
}

func reportOnlyReadFixtureEvents(callID string) []state.TaskEventRecord {
	var events []state.TaskEventRecord
	for i, id := range []string{"r1", "r2", "r3", "r4"} {
		events = append(events, state.TaskEventRecord{
			CallID: callID,
			Blocks: []state.TaskBlockSummary{{Type: "tool_result", ToolID: id, DurationMS: int64(i+1) * 10}},
		})
	}
	return events
}

func assertReportOnlyMetric(t *testing.T, name string, got ReportOnlyReadMetric, want int64) {
	t.Helper()
	if got.Status != "known" || got.Value == nil || *got.Value != want {
		t.Fatalf("%s = %#v want known %d", name, got, want)
	}
}

func assertReportOnlyUnknown(t *testing.T, name string, got ReportOnlyReadMetric) {
	t.Helper()
	if got.Status != "unknown" || got.Value != nil {
		t.Fatalf("%s = %#v want unknown without value", name, got)
	}
}

func TestReportOnlyReadReentryBundleTranscriptAndCallTelemetry(t *testing.T) {
	call := reportOnlyReadFixtureCall()
	got := AnalyzeReportOnlyReadReentry(call.CallID, []byte(reportOnlyReadFixtureTranscript), call, reportOnlyReadFixtureEvents(call.CallID))
	if got.Identity != "matched-call-id" || got.Phase != call.Phase || got.Outcome != "rate_limited" {
		t.Fatalf("bounded identity/phase/outcome = %#v", got)
	}
	assertReportOnlyMetric(t, "reads", got.ReadCalls, 4)
	assertReportOnlyMetric(t, "same-path-split", got.SamePathSplitReads, 3)
	assertReportOnlyMetric(t, "tool-ms", got.ToolDurationMS, 100)
	assertReportOnlyMetric(t, "api-ms", got.CLIApiDurationMS, 730149)
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"/private/inventory/secret.txt", "/other/file.txt", "secret data", "923440"} {
		if strings.Contains(string(encoded), raw) {
			t.Fatal("bounded evidence exposed transcript path, raw data, or conflated wall time")
		}
	}
}

func TestReportOnlyReadReentryMissingEvidenceStaysUnknown(t *testing.T) {
	call := reportOnlyReadFixtureCall()
	events := reportOnlyReadFixtureEvents(call.CallID)
	t.Run("unmatched-call-id", func(t *testing.T) {
		got := AnalyzeReportOnlyReadReentry("another-call", []byte(reportOnlyReadFixtureTranscript), call, events)
		if got.Identity != "unknown" || got.Phase != "unknown" || got.Outcome != "unknown" {
			t.Fatalf("unmatched identity leaked another call's values: %#v", got)
		}
		assertReportOnlyUnknown(t, "reads", got.ReadCalls)
		assertReportOnlyUnknown(t, "tool", got.ToolDurationMS)
		assertReportOnlyUnknown(t, "api", got.CLIApiDurationMS)
	})
	t.Run("missing-transcript", func(t *testing.T) {
		got := AnalyzeReportOnlyReadReentry(call.CallID, nil, call, events)
		assertReportOnlyUnknown(t, "reads", got.ReadCalls)
		assertReportOnlyUnknown(t, "same-path", got.SamePathSplitReads)
		assertReportOnlyUnknown(t, "tool", got.ToolDurationMS)
		assertReportOnlyMetric(t, "api", got.CLIApiDurationMS, 730149)
	})
	t.Run("missing-tool-observation", func(t *testing.T) {
		got := AnalyzeReportOnlyReadReentry(call.CallID, []byte(reportOnlyReadFixtureTranscript), call, nil)
		assertReportOnlyMetric(t, "reads", got.ReadCalls, 4)
		assertReportOnlyMetric(t, "same-path", got.SamePathSplitReads, 3)
		assertReportOnlyUnknown(t, "tool", got.ToolDurationMS)
	})
	t.Run("missing-range", func(t *testing.T) {
		partial := strings.Replace(reportOnlyReadFixtureTranscript, `"offset":1,"limit":1`, `"offset":1`, 1)
		got := AnalyzeReportOnlyReadReentry(call.CallID, []byte(partial), call, events)
		assertReportOnlyMetric(t, "reads", got.ReadCalls, 4)
		assertReportOnlyUnknown(t, "same-path", got.SamePathSplitReads)
	})
	t.Run("missing-api-duration", func(t *testing.T) {
		noAPI := call
		noAPI.ClaudeAPIDurationMS = 0
		got := AnalyzeReportOnlyReadReentry(call.CallID, []byte(reportOnlyReadFixtureTranscript), noAPI, events)
		assertReportOnlyUnknown(t, "api", got.CLIApiDurationMS)
		assertReportOnlyMetric(t, "tool", got.ToolDurationMS, 100)
	})
	t.Run("malformed-transcript", func(t *testing.T) {
		got := AnalyzeReportOnlyReadReentry(call.CallID, []byte(reportOnlyReadFixtureTranscript+"{invalid"), call, events)
		assertReportOnlyUnknown(t, "reads", got.ReadCalls)
		assertReportOnlyUnknown(t, "same-path", got.SamePathSplitReads)
		assertReportOnlyUnknown(t, "tool", got.ToolDurationMS)
	})
	t.Run("duplicate-tool-duration", func(t *testing.T) {
		duplicated := append(append([]state.TaskEventRecord(nil), events...), events[0])
		got := AnalyzeReportOnlyReadReentry(call.CallID, []byte(reportOnlyReadFixtureTranscript), call, duplicated)
		assertReportOnlyUnknown(t, "tool", got.ToolDurationMS)
		assertReportOnlyMetric(t, "reads", got.ReadCalls, 4)
	})
}
