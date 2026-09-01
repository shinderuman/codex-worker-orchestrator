package app

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestBundleAnalysisTypesDoNotCarryExplanationFields(t *testing.T) {
	for name, value := range map[string]any{
		"interval":    bundleAnalysisInterval{},
		"subsequents": bundleAnalysisSubsequents{},
		"rollout":     bundleAnalysisRollout{},
		"count":       bundleAnalysisCount{},
		"token-delta": bundleAnalysisTokenDelta{},
		"validations": bundleAnalysisValidations{},
		"retries":     bundleAnalysisRetries{},
	} {
		typeOf := reflect.TypeOf(value)
		for _, field := range []string{"Basis", "Note", "Rule"} {
			if _, found := typeOf.FieldByName(field); found {
				t.Fatalf("%s retains explanation-only field %s", name, field)
			}
		}
	}
}

func TestBundleAnalysisJSONRemainsStructural(t *testing.T) {
	start := "2026-09-01T00:00:00Z"
	end := "2026-09-01T00:01:00Z"
	index := bundleAnalysisIndex{
		Version:    bundleAnalysisIndexVersion,
		TaskID:     "task-id",
		TaskStatus: "complete",
		Intervals: bundleAnalysisIntervals{
			TaskExecution: bundleAnalysisInterval{
				Status: analysisStatusAvailable, Start: &start, End: &end,
				EndBasis: analysisExecutionEndBasisLifecycleComplete,
			},
			ParentFinalization: bundleAnalysisInterval{Status: analysisStatusUnknown},
			SubsequentRequests: bundleAnalysisSubsequents{
				Status: analysisStatusAvailable, Attribution: analysisAttributionSubsequent,
			},
			Collection: bundleAnalysisInterval{Status: analysisStatusAvailable, Start: &start, End: &end},
		},
		RolloutWindow: bundleAnalysisRollout{Status: analysisStatusAvailable, TotalBytes: 100},
		WaitCalls:     bundleAnalysisCount{Status: analysisStatusCounted, Count: 2},
		TokenDelta:    bundleAnalysisTokenDelta{Status: analysisStatusAvailable, InputTokens: 10},
		Finalization:  bundleAnalysisTokenDelta{Status: analysisStatusAvailable, InputTokens: 5},
		ValidationRuns: bundleAnalysisValidations{
			Status: analysisStatusAvailable,
		},
		Retries: bundleAnalysisRetries{
			ResumedModelCalls: bundleAnalysisCount{Status: analysisStatusCounted, Count: 1},
		},
	}

	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{`"basis":`, `"note":`, `"attribution_rule":`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("serialized analysis index retained explanation-only contract %q: %s", forbidden, text)
		}
	}
	for _, required := range []string{
		`"end_basis":"lifecycle-complete"`,
		`"attribution":"unattributed-subsequent-request"`,
		`"input_tokens":10`,
		`"count":2`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("serialized analysis index lost structural fact %q: %s", required, text)
		}
	}
	if len(data) >= 2048 {
		t.Fatalf("serialized analysis index overhead = %d bytes", len(data))
	}
}
