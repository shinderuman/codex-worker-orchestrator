package app

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestCustomWaitExtensionPreservesLegacyWaitDecode(t *testing.T) {
	arguments := analysisLegacyWaitArguments(300000)
	if !json.Valid([]byte(arguments)) {
		t.Fatalf("legacy arguments are not JSON: %q", arguments)
	}
	var direct struct {
		YieldTimeMS *float64 `json:"yield-time_ms"`
	}
	if err := json.Unmarshal([]byte(arguments), &direct); err != nil {
		t.Fatalf("direct unmarshal failed for %q: %v", arguments, err)
	}
	assertAnalysisWaitYield(t, "manual-direct", direct.YieldTimeMS, 300000)
	if yield := analysisWaitRequestedYield(arguments); yield == nil || *yield != 300000 {
		t.Fatalf("helper direct yield = %#v arguments=%q", yield, arguments)
	}

	at := time.Date(2026, 9, 17, 7, 1, 0, 0, time.UTC)
	line := analysisWaitRequestLine(t, at, "legacy-decode", arguments)
	var envelope codexRolloutScanLine
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		t.Fatal(err)
	}
	var item codexRolloutItemPayload
	if err := json.Unmarshal(envelope.Payload, &item); err != nil {
		t.Fatal(err)
	}
	if item.Arguments != arguments {
		t.Fatalf("decoded arguments = %q want %q", item.Arguments, arguments)
	}
	assertAnalysisWaitYield(t, "decoded", analysisWaitRequestedYield(item.Arguments), 300000)

	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeBundleFile(t, path, line)
	scan, err := scanCodexRolloutWindow(path, at.Add(-time.Minute), at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.waits) != 1 {
		t.Fatalf("scan waits = %#v", scan.waits)
	}
	assertAnalysisWaitYield(t, "scan", scan.waits[0].YieldMS, 300000)
}

func assertAnalysisWaitYield(t *testing.T, stage string, yield *float64, want float64) {
	t.Helper()
	if yield == nil || *yield != want {
		t.Fatalf("%s yield = %#v want %v", stage, yield, want)
	}
}
