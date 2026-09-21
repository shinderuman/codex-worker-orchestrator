package app

import (
	"testing"
	"time"
)

func TestCustomExecWriteStdinWaitAcceptsSessionReference(t *testing.T) {
	start := time.Date(2026, 9, 17, 7, 0, 0, 0, time.UTC)
	at := start.Add(time.Minute)
	waits := analysisWaitCallsFromLines(t, start, start.Add(time.Hour), []string{
		analysisCustomWaitRequestLine(t, at, "custom-reference", `await tools.write_stdin({session_id: result.session_id, chars: "", yield_time_ms: 300000, max_output_tokens: 4096});`),
	})
	if waits.Status != analysisStatusCounted || waits.Count != 1 || len(waits.Calls) != 1 {
		t.Fatalf("waits = %#v", waits)
	}
	call := waits.Calls[0]
	if call.CallID != "custom-reference" || call.RequestedYieldMS == nil || *call.RequestedYieldMS != 300000 ||
		call.YieldClass != analysisWaitYieldClassBounded {
		t.Fatalf("wait = %#v", call)
	}
}
