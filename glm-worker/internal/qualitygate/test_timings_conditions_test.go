package qualitygate

import (
	"encoding/json"
	"testing"
)

func TestTimingArtifactsRejectMissingIdentityElapsedAndContradictoryCoverage(t *testing.T) {
	base := map[string]any{"schema_version": TestTimingsSchemaVersion, "validation_run_id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "form": "go-test", "coverage": map[string]any{"source": "go-test-json", "captured": true, "terminal_events": 1}, "tests": []any{map[string]any{"package": "example/m", "test": "TestZero", "action": "pass", "elapsed_ms": 0}}}
	wire, _ := json.Marshal(base)
	if _, err := DecodeTestTimings(wire); err != nil {
		t.Fatal(err)
	}
	for _, defect := range []string{"identity", "elapsed", "form", "coverage"} {
		var current map[string]any
		if err := json.Unmarshal(wire, &current); err != nil {
			t.Fatal(err)
		}
		switch defect {
		case "identity":
			delete(current, "validation_run_id")
		case "elapsed":
			delete(current["tests"].([]any)[0].(map[string]any), "elapsed_ms")
		case "form":
			current["form"] = "foreign-run"
		case "coverage":
			current["coverage"] = map[string]any{"source": "plain-log", "terminal_events": 1}
		}
		encoded, _ := json.Marshal(current)
		if _, err := DecodeTestTimings(encoded); err == nil {
			t.Fatalf("accepted %s", defect)
		}
		if defect != "elapsed" {
			var invalid TestTimings
			if err := json.Unmarshal(encoded, &invalid); err != nil {
				t.Fatal(err)
			}
			if _, err := EncodeTestTimings(invalid); err == nil {
				t.Fatalf("producer accepted %s before persistence", defect)
			}
		}
	}
}

func TestCacheObservationDistinguishesExecutionReuseAndMissingEvidence(t *testing.T) {
	for _, tc := range []struct{ output, want string }{{"ok \texample/m\t0.2s\n", "executed"}, {"ok \texample/m\t(cached)\n", "cached"}, {"FAIL\texample/m\t0.2s\n", "executed"}, {"unstructured log", "unknown"}} {
		event, _ := json.Marshal(map[string]any{"Action": "output", "Package": "example/m", "Output": tc.output})
		if got := TestCacheObservation(event); got != tc.want {
			t.Fatalf("cache observation %q: %s", tc.output, got)
		}
	}
}
