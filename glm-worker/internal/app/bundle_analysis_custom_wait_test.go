package app

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

const analysisBoundedCustomWaitSource = `// @exec: {"yield-time_ms":300000,"max_output_tokens":4096}
const result = await tools.write_stdin({session_id: 101, chars: '', yield_time_ms: 300000, max_output_tokens: 4096}); text(result.output);`

func TestCustomExecWriteStdinWaitsNormalize(t *testing.T) {
	start := time.Date(2026, 9, 17, 7, 0, 0, 0, time.UTC)
	at := start.Add(time.Minute)
	lines := []string{
		analysisCustomWaitRequestLine(t, at, "custom-short", `await tools.write_stdin({session_id: 101, chars: "", yield_time_ms: 30000});`),
		analysisCustomWaitRequestLine(t, at.Add(time.Minute), "custom-bounded", analysisBoundedCustomWaitSource),
		analysisCustomWaitReturnLine(t, at.Add(2*time.Minute), "custom-bounded"),
	}
	waits := analysisWaitCallsFromLines(t, start, start.Add(time.Hour), lines)
	if waits.Status != analysisStatusCounted || waits.Count != 2 || len(waits.DuplicateCallIDs) != 0 {
		t.Fatalf("wait calls = %#v", waits)
	}
	byCall := analysisWaitCallsByID(waits.Calls)
	short := byCall["custom-short"]
	if short.RequestedYieldMS == nil || *short.RequestedYieldMS != 30000 || short.YieldClass != analysisWaitYieldClassShort {
		t.Fatalf("short wait = %#v", short)
	}
	bounded := byCall["custom-bounded"]
	if bounded.RequestedYieldMS == nil || *bounded.RequestedYieldMS != 300000 || bounded.YieldClass != analysisWaitYieldClassBounded ||
		len(bounded.ReturnLines) != 1 || bounded.ReturnLines[0] != 3 {
		t.Fatalf("bounded wait = %#v", bounded)
	}
}

func TestCustomExecWriteStdinWaitRejectsFalsePositives(t *testing.T) {
	start := time.Date(2026, 9, 17, 7, 0, 0, 0, time.UTC)
	at := start.Add(time.Minute)
	lines := []string{
		analysisCustomExecLine(t, at, "exec-command", `await tools.exec_command({cmd: "echo tools.write_stdin", yield_time_ms: 300000});`),
		analysisCustomExecLine(t, at.Add(time.Minute), "string-only", `text("tools.write_stdin({session_id: 101, yield_time_ms: 300000})");`),
		analysisCustomExecLine(t, at.Add(2*time.Minute), "shape-mismatch", `await tools.write_stdin(session);`),
		analysisCustomExecLine(t, at.Add(3*time.Minute), "writes-input", `await tools.write_stdin({session_id: 101, chars: "x", yield_time_ms: 300000});`),
		analysisCustomExecLine(t, at.Add(4*time.Minute), "extra-code", `await tools.write_stdin({session_id: 101, chars: "", yield_time_ms: 300000}); text("extra");`),
		analysisCustomExecLine(t, at.Add(5*time.Minute), "dynamic-session", `await tools.write_stdin({session_id: session.id, chars: "", yield_time_ms: 300000});`),
		analysisCustomExecLine(t, at.Add(6*time.Minute), "missing-chars", `await tools.write_stdin({session_id: 101, yield_time_ms: 300000});`),
		analysisCustomExecLine(t, at.Add(7*time.Minute), "wrong-tool", `await tools.write_stdin({session_id: 101, chars: "", yield_time_ms: 300000});`),
	}
	lines[len(lines)-1] = analysisCustomToolLine(t, at.Add(7*time.Minute), "wrong-tool", "other", `await tools.write_stdin({session_id: 101, chars: "", yield_time_ms: 300000});`)

	waits := analysisWaitCallsFromLines(t, start, start.Add(time.Hour), lines)
	if waits.Status != analysisStatusCounted || waits.Count != 0 || len(waits.Calls) != 0 || len(waits.DuplicateCallIDs) != 0 {
		t.Fatalf("false positives = %#v", waits)
	}
}

func TestCustomExecWriteStdinWaitMixedTransportDeduplicatesByCallIdentity(t *testing.T) {
	start := time.Date(2026, 9, 17, 7, 0, 0, 0, time.UTC)
	at := start.Add(time.Minute)
	lines := []string{
		analysisWaitRequestLine(t, at, "shared", `{"yield-time_ms":300000}`),
		analysisCustomWaitRequestLine(t, at.Add(time.Second), "shared", `await tools.write_stdin({session_id: 101, chars: "", yield_time_ms: 300000});`),
		analysisCustomWaitReturnLine(t, at.Add(2*time.Second), "shared"),
	}
	waits := analysisWaitCallsFromLines(t, start, start.Add(time.Hour), lines)
	if waits.Count != 1 || len(waits.Calls) != 1 || len(waits.DuplicateCallIDs) != 0 {
		t.Fatalf("mixed wait calls = %#v", waits)
	}
	call := waits.Calls[0]
	if call.CallID != "shared" || len(call.RequestLines) != 2 || call.RequestLines[0] != 1 || call.RequestLines[1] != 2 ||
		len(call.ReturnLines) != 1 || call.ReturnLines[0] != 3 || call.RequestedYieldMS == nil || *call.RequestedYieldMS != 300000 {
		t.Fatalf("mixed wait = %#v", call)
	}
}

func TestCustomExecWriteStdinWaitMalformedAndConflictFailClosed(t *testing.T) {
	start := time.Date(2026, 9, 17, 7, 0, 0, 0, time.UTC)
	at := start.Add(time.Minute)
	t.Run("malformed-yield-is-unknown", func(t *testing.T) {
		waits := analysisWaitCallsFromLines(t, start, start.Add(time.Hour), []string{
			analysisCustomWaitRequestLine(t, at, "malformed", `await tools.write_stdin({session_id: 101, chars: "", yield_time_ms: "300000"});`),
		})
		if waits.Count != 1 || len(waits.Calls) != 1 {
			t.Fatalf("waits = %#v", waits)
		}
		call := waits.Calls[0]
		if call.RequestedYieldMS != nil || call.YieldClass != analysisStatusUnknown {
			t.Fatalf("malformed yield = %#v", call)
		}
	})

	t.Run("pragma-yield-conflict-is-unknown", func(t *testing.T) {
		input := `// @exec: {"yield-time_ms":30000,"max_output_tokens":4096}
await tools.write_stdin({session_id: 101, chars: "", yield_time_ms: 300000});`
		waits := analysisWaitCallsFromLines(t, start, start.Add(time.Hour), []string{
			analysisCustomWaitRequestLine(t, at, "pragma-conflict", input),
		})
		if waits.Count != 1 || len(waits.Calls) != 1 {
			t.Fatalf("waits = %#v", waits)
		}
		call := waits.Calls[0]
		if call.RequestedYieldMS != nil || call.YieldClass != analysisStatusUnknown {
			t.Fatalf("pragma conflict = %#v", call)
		}
	})

	t.Run("conflicting-yields-stay-conflicted", func(t *testing.T) {
		waits := analysisWaitCallsFromLines(t, start, start.Add(time.Hour), []string{
			analysisWaitRequestLine(t, at, "conflict", `{"yield-time_ms":30000}`),
			analysisCustomWaitRequestLine(t, at.Add(time.Second), "conflict", `await tools.write_stdin({session_id: 101, chars: "", yield_time_ms: 300000});`),
		})
		if waits.Count != 0 || len(waits.Calls) != 0 || len(waits.DuplicateCallIDs) != 1 {
			t.Fatalf("conflict waits = %#v", waits)
		}
		conflict := waits.DuplicateCallIDs[0]
		if conflict.CallID != "conflict" || len(conflict.RequestLines) != 2 || conflict.RequestLines[0] != 1 || conflict.RequestLines[1] != 2 {
			t.Fatalf("conflict = %#v", conflict)
		}
	})
}

func TestCustomExecWriteStdinWaitBundleLikeUndercountRegression(t *testing.T) {
	start := time.Date(2026, 9, 17, 7, 0, 0, 0, time.UTC)
	at := start.Add(time.Minute)
	lines := []string{analysisWaitRequestLine(t, at, "legacy", `{"yield-time_ms":300000}`)}
	for index := 0; index < 2; index++ {
		lines = append(lines, analysisCustomWaitRequestLine(t, at.Add(time.Duration(index+1)*time.Minute), fmt.Sprintf("startup-%d", index),
			`await tools.write_stdin({session_id: 101, chars: "", yield_time_ms: 30000});`))
	}
	for index := 0; index < 14; index++ {
		lines = append(lines, analysisCustomWaitRequestLine(t, at.Add(time.Duration(index+3)*time.Minute), fmt.Sprintf("long-%d", index), analysisBoundedCustomWaitSource))
	}
	waits := analysisWaitCallsFromLines(t, start, start.Add(time.Hour), lines)
	if waits.Count != 17 || len(waits.Calls) != 17 || len(waits.DuplicateCallIDs) != 0 {
		t.Fatalf("bundle-like wait count = %#v", waits)
	}
	short, bounded := 0, 0
	for _, call := range waits.Calls {
		switch call.YieldClass {
		case analysisWaitYieldClassShort:
			short++
		case analysisWaitYieldClassBounded:
			bounded++
		}
	}
	if short != 2 || bounded != 15 {
		t.Fatalf("wait classes short=%d bounded=%d calls=%#v", short, bounded, waits.Calls)
	}
}

func analysisWaitCallsFromLines(t *testing.T, start, end time.Time, lines []string) bundleAnalysisWaitCalls {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeBundleFile(t, path, joinAnalysisLines(lines))
	scan, err := scanCodexRolloutWindow(path, start, end)
	if err != nil {
		t.Fatal(err)
	}
	return analysisWaitCalls(codexAssociation{ParentStatus: codexStatusIncluded}, scan, nil, start,
		analysisExecutionBoundary{status: analysisStatusAvailable, end: end}, end)
}

func analysisWaitCallsByID(calls []bundleAnalysisWaitCall) map[string]bundleAnalysisWaitCall {
	result := make(map[string]bundleAnalysisWaitCall, len(calls))
	for _, call := range calls {
		result[call.CallID] = call
	}
	return result
}

func analysisCustomWaitRequestLine(t *testing.T, timestamp time.Time, callID, input string) string {
	t.Helper()
	return analysisCustomExecLine(t, timestamp, callID, input)
}

func analysisCustomExecLine(t *testing.T, timestamp time.Time, callID, input string) string {
	t.Helper()
	return analysisCustomToolLine(t, timestamp, callID, "exec", input)
}

func analysisCustomToolLine(t *testing.T, timestamp time.Time, callID, name, input string) string {
	t.Helper()
	return analysisRolloutLine(t, timestamp, "response_item", map[string]any{
		"type":    codexRolloutCustomToolCallType,
		"name":    name,
		"call_id": callID,
		"input":   input,
	})
}

func analysisCustomWaitReturnLine(t *testing.T, timestamp time.Time, callID string) string {
	t.Helper()
	return analysisRolloutLine(t, timestamp, "response_item", map[string]any{
		"type":    codexRolloutCustomToolCallOutputType,
		"call_id": callID,
		"output":  "done",
	})
}
