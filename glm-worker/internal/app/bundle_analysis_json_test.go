package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBundleAnalysisJSONOmitsExplanatoryProse(t *testing.T) {
	longText := strings.Repeat("explanatory-prose-", 80)
	start := "2026-09-01T00:00:00Z"
	end := "2026-09-01T00:01:00Z"
	index := bundleAnalysisIndex{
		Version:    bundleAnalysisIndexVersion,
		TaskID:     "task-id",
		TaskStatus: "complete",
		Intervals: bundleAnalysisIntervals{
			TaskExecution: bundleAnalysisInterval{
				Status: analysisStatusAvailable, Start: &start, End: &end,
				EndBasis: analysisExecutionEndBasisLifecycleComplete, Basis: longText,
			},
			ParentFinalization: bundleAnalysisInterval{Status: analysisStatusUnknown, Basis: longText},
			SubsequentRequests: bundleAnalysisSubsequents{
				Status: analysisStatusAvailable, Attribution: analysisAttributionSubsequent, Basis: longText,
			},
			Collection: bundleAnalysisInterval{Status: analysisStatusAvailable, Start: &start, End: &end, Basis: longText},
		},
		RolloutWindow: bundleAnalysisRollout{Status: analysisStatusAvailable, TotalBytes: 100, Note: longText},
		WaitCalls:     bundleAnalysisCount{Status: analysisStatusCounted, Count: 2, Basis: longText},
		TokenDelta:    bundleAnalysisTokenDelta{Status: analysisStatusAvailable, InputTokens: 10, Basis: longText},
		Finalization:  bundleAnalysisTokenDelta{Status: analysisStatusAvailable, InputTokens: 5, Basis: longText},
		ValidationRuns: bundleAnalysisValidations{
			Status: analysisStatusAvailable, Rule: longText,
		},
		Retries: bundleAnalysisRetries{
			ResumedModelCalls: bundleAnalysisCount{Status: analysisStatusCounted, Count: 1, Basis: longText},
			Basis:             longText,
		},
	}

	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{longText, `"basis":`, `"note":`, `"attribution_rule":`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("serialized analysis index retained explanatory contract %q: %s", forbidden, text)
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
