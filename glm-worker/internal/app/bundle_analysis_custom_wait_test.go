package app

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestCustomExecWriteStdinWaitsNormalize(t *testing.T) {
	start := time.Date(2026, 9, 19, 2, 0, 0, 0, time.UTC)
	at := start.Add(time.Minute)
	lines := []string{
		analysisCustomWaitRequestLine(t, at, "custom-short", analysisObservedDirectWaitSource(46866, 30000, 16000)),
		analysisCustomWaitRequestLine(t, at.Add(time.Minute), "custom-bounded", analysisObservedDirectWaitSource(46866, 300000, 20000)),
		analysisCustomWaitReturnLine(t, at.Add(2*time.Minute), "custom-bounded"),
		analysisCustomWaitRequestLine(t, at.Add(3*time.Minute), "custom-long", analysisObservedPragmaWaitSource()),
	}
	waits := analysisWaitCallsFromLines(t, start, start.Add(time.Hour), lines)
	if waits.Status != analysisStatusCounted || waits.Count != 3 || len(waits.DuplicateCallIDs) != 0 {
		t.Fatalf("wait calls = %#v", waits)
	}
	byCall := analysisWaitCallsByID(waits.Calls)
	assertAnalysisWaitCall(t, byCall["custom-short"], 30000, analysisWaitYieldClassShort)
	assertAnalysisWaitCall(t, byCall["custom-bounded"], 300000, analysisWaitYieldClassBounded)
	assertAnalysisWaitCall(t, byCall["custom-long"], 21600000, analysisWaitYieldClassLong)
	bounded := byCall["custom-bounded"]
	if len(bounded.ReturnLines) != 1 || bounded.ReturnLines[0] != 3 {
		t.Fatalf("bounded wait return = %#v", bounded)
	}
}

func TestCustomExecWriteStdinWaitRejectsNonCanonicalShapes(t *testing.T) {
	start := time.Date(2026, 9, 19, 2, 0, 0, 0, time.UTC)
	at := start.Add(time.Minute)
	loopWrapper := fmt.Sprintf("// @exec: {%q: 21600000, %q: 20000}\nlet combined = %q;\nwhile (true) {\n  const r = await tools.write_stdin({session_id: 46866, chars: %q, yield_time_ms: 300000, max_output_tokens: 20000});\n  if (r.output) combined += r.output;\n  if (r.exit_code !== undefined) break;\n}",
		"yield-time_ms", "max_output_tokens", "", "")
	lines := []string{
		analysisCustomExecLine(t, at, "exec-command", fmt.Sprintf("const r = await tools.exec_command({cmd:%q,yield_time_ms:300000}); text(r);", "echo tools.write_stdin")),
		analysisCustomExecLine(t, at.Add(time.Minute), "string-only", fmt.Sprintf("text(%q);", "tools.write_stdin({session_id:46866,yield_time_ms:300000})")),
		analysisCustomExecLine(t, at.Add(2*time.Minute), "shape-mismatch", "const r = await tools.write_stdin(session); text(r);"),
		analysisCustomExecLine(t, at.Add(3*time.Minute), "writes-input", fmt.Sprintf("const r = await tools.write_stdin({session_id:46866,chars:%q,yield_time_ms:300000,max_output_tokens:20000}); text(r);", "x")),
		analysisCustomExecLine(t, at.Add(4*time.Minute), "writes-whitespace", fmt.Sprintf("const r = await tools.write_stdin({session_id:46866,chars:%q,yield_time_ms:300000,max_output_tokens:20000}); text(r);", " ")),
		analysisCustomExecLine(t, at.Add(5*time.Minute), "extra-code", analysisObservedDirectWaitSource(46866, 300000, 20000)+fmt.Sprintf(" text(%q);", "extra")),
		analysisCustomExecLine(t, at.Add(6*time.Minute), "dynamic-session", fmt.Sprintf("const r = await tools.write_stdin({session_id:sid,chars:%q,yield_time_ms:300000,max_output_tokens:20000}); text(r);", "")),
		analysisCustomExecLine(t, at.Add(7*time.Minute), "missing-chars", "const r = await tools.write_stdin({session_id:46866,yield_time_ms:300000,max_output_tokens:20000}); text(r);"),
		analysisCustomExecLine(t, at.Add(8*time.Minute), "loop-wrapper", loopWrapper),
		analysisCustomToolLine(t, at.Add(9*time.Minute), "wrong-tool", "other", analysisObservedDirectWaitSource(46866, 300000, 20000)),
	}
	waits := analysisWaitCallsFromLines(t, start, start.Add(time.Hour), lines)
	if waits.Status != analysisStatusCounted || waits.Count != 0 || len(waits.Calls) != 0 || len(waits.DuplicateCallIDs) != 0 {
		t.Fatalf("non-canonical waits = %#v", waits)
	}
}

func TestCustomExecWriteStdinWaitPreservesLegacySemantics(t *testing.T) {
	start := time.Date(2026, 9, 19, 2, 0, 0, 0, time.UTC)
	at := start.Add(time.Minute)
	lines := []string{
		analysisWaitRequestLine(t, at, "legacy", analysisLegacyWaitArguments(300000)),
		analysisWaitReturnLine(t, at.Add(time.Second), "legacy"),
	}
	waits := analysisWaitCallsFromLines(t, start, start.Add(time.Hour), lines)
	if waits.Count != 1 || len(waits.Calls) != 1 || len(waits.DuplicateCallIDs) != 0 {
		t.Fatalf("legacy waits = %#v", waits)
	}
	call := waits.Calls[0]
	assertAnalysisWaitCall(t, call, 300000, analysisWaitYieldClassBounded)
	if len(call.ReturnLines) != 1 || call.ReturnLines[0] != 2 {
		t.Fatalf("legacy return = %#v", call)
	}
}

func TestCustomExecWriteStdinWaitMixedTransportDeduplicatesByCallIdentity(t *testing.T) {
	start := time.Date(2026, 9, 19, 2, 0, 0, 0, time.UTC)
	at := start.Add(time.Minute)
	lines := []string{
		analysisWaitRequestLine(t, at, "shared", analysisLegacyWaitArguments(300000)),
		analysisCustomWaitRequestLine(t, at.Add(time.Second), "shared", analysisObservedDirectWaitSource(46866, 300000, 20000)),
		analysisCustomWaitReturnLine(t, at.Add(2*time.Second), "shared"),
	}
	waits := analysisWaitCallsFromLines(t, start, start.Add(time.Hour), lines)
	if waits.Count != 1 || len(waits.Calls) != 1 || len(waits.DuplicateCallIDs) != 0 {
		t.Fatalf("mixed wait calls = %#v", waits)
	}
	call := waits.Calls[0]
	if call.CallID != "shared" || len(call.RequestLines) != 2 || call.RequestLines[0] != 1 || call.RequestLines[1] != 2 ||
		len(call.ReturnLines) != 1 || call.ReturnLines[0] != 3 {
		t.Fatalf("mixed wait = %#v", call)
	}
	assertAnalysisWaitCall(t, call, 300000, analysisWaitYieldClassBounded)
}

func TestCustomExecWriteStdinWaitMalformedAndConflictFailClosed(t *testing.T) {
	start := time.Date(2026, 9, 19, 2, 0, 0, 0, time.UTC)
	at := start.Add(time.Minute)
	t.Run("malformed-yield-is-unknown", func(t *testing.T) {
		input := fmt.Sprintf("const r = await tools.write_stdin({session_id:46866,chars:%q,yield_time_ms:%q,max_output_tokens:20000}); text(r);", "", "300000")
		waits := analysisWaitCallsFromLines(t, start, start.Add(time.Hour), []string{
			analysisCustomWaitRequestLine(t, at, "malformed", input),
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
		input := fmt.Sprintf("// @exec: {%q:30000,%q:20000}\nconst r = await tools.write_stdin({session_id:46866,chars:%q,yield_time_ms:300000,max_output_tokens:20000});\ntext(r);",
			"yield-time_ms", "max_output_tokens", "")
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

	t.Run("conflicting-transports-stay-conflicted", func(t *testing.T) {
		waits := analysisWaitCallsFromLines(t, start, start.Add(time.Hour), []string{
			analysisWaitRequestLine(t, at, "conflict", analysisLegacyWaitArguments(30000)),
			analysisCustomWaitRequestLine(t, at.Add(time.Second), "conflict", analysisObservedDirectWaitSource(46866, 300000, 20000)),
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
	start := time.Date(2026, 9, 19, 2, 0, 0, 0, time.UTC)
	at := start.Add(time.Minute)
	lines := []string{analysisWaitRequestLine(t, at, "legacy", analysisLegacyWaitArguments(300000))}
	for index := 0; index < 2; index++ {
		lines = append(lines, analysisCustomWaitRequestLine(t, at.Add(time.Duration(index+1)*time.Minute), fmt.Sprintf("startup-%d", index),
			analysisObservedDirectWaitSource(46866, 30000, 16000)))
	}
	for index := 0; index < 14; index++ {
		lines = append(lines, analysisCustomWaitRequestLine(t, at.Add(time.Duration(index+3)*time.Minute), fmt.Sprintf("long-%d", index),
			analysisObservedDirectWaitSource(46866, 300000, 20000)))
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

func analysisObservedDirectWaitSource(sessionID, yieldMS, maxOutputTokens int) string {
	return fmt.Sprintf("const r = await tools.write_stdin({session_id:%d, chars:%q, yield_time_ms:%d, max_output_tokens:%d});\ntext(r);",
		sessionID, "", yieldMS, maxOutputTokens)
}

func analysisObservedPragmaWaitSource() string {
	return fmt.Sprintf("// @exec: {%q: 21600000, %q: 1600}\nconst r = await tools.write_stdin({\n  session_id: 24719,\n  chars: %q,\n  yield_time_ms: 21600000,\n  max_output_tokens: 1600\n});\ntext(JSON.stringify(r));",
		"yield-time_ms", "max_output_tokens", "")
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

func analysisLegacyWaitArguments(yieldMS int) string {
	return fmt.Sprintf("{%q:%d}", "yield"+"_"+"time_ms", yieldMS)
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

func assertAnalysisWaitCall(t *testing.T, call bundleAnalysisWaitCall, wantYield float64, wantClass string) {
	t.Helper()
	if call.RequestedYieldMS == nil || *call.RequestedYieldMS != wantYield || call.YieldClass != wantClass {
		t.Fatalf("wait = %#v want yield=%v class=%s", call, wantYield, wantClass)
	}
}
