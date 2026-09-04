package app

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestBundleAnalysisTypesDoNotCarryExplanationFields(t *testing.T) {
	for name, value := range map[string]any{
		"interval":           bundleAnalysisInterval{},
		"subsequents":        bundleAnalysisSubsequents{},
		"rollout":            bundleAnalysisRollout{},
		"count":              bundleAnalysisCount{},
		"token-delta":        bundleAnalysisTokenDelta{},
		"validations":        bundleAnalysisValidations{},
		"retries":            bundleAnalysisRetries{},
		"model-relations":    bundleAnalysisModelRelations{},
		"retry-edge":         bundleAnalysisRetryEdge{},
		"ambiguous-relation": bundleAnalysisAmbiguousRelation{},
		"unlinked-call":      bundleAnalysisUnlinkedCall{},
		"duplicate-calls":    bundleAnalysisDuplicateCalls{},
		"record-trace":       bundleAnalysisRecordTrace{},
		"wait-calls":         bundleAnalysisWaitCalls{},
		"wait-call":          bundleAnalysisWaitCall{},
		"wait-duplicate":     bundleAnalysisWaitDuplicate{},
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
	yield := 1000.0
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
		WaitCalls: bundleAnalysisWaitCalls{
			Status: analysisStatusCounted, Count: 2,
			Calls: []bundleAnalysisWaitCall{{
				CallID:           "call-wait",
				RequestedYieldMS: &yield,
				YieldClass:       analysisWaitYieldClassShort,
				RequestLines:     []int{47},
				ReturnLines:      []int{48},
			}},
		},
		TokenDelta:   bundleAnalysisTokenDelta{Status: analysisStatusAvailable, InputTokens: 10},
		Finalization: bundleAnalysisTokenDelta{Status: analysisStatusAvailable, InputTokens: 5},
		ValidationRuns: bundleAnalysisValidations{
			Status: analysisStatusAvailable,
		},
		Retries: bundleAnalysisRetries{
			ResumedModelCalls: bundleAnalysisCount{Status: analysisStatusCounted, Count: 1},
			ModelCallRelations: bundleAnalysisModelRelations{
				Status: analysisStatusAvailable,
				Resolved: []bundleAnalysisRetryEdge{{
					CallID:      "call-retry",
					RetryOf:     "call-original",
					RetryReason: "invalid-packet-result-correction",
					Phase:       "worker-new-result-correct",
					Outcome:     "success",
					Resumed:     true,
					Source:      bundleAnalysisRecordTrace{ArchivePath: "task/telemetry/task-id.jsonl", Lines: []int{2}},
				}},
				Ambiguous: []bundleAnalysisAmbiguousRelation{
					{
						CallID:      "call-ambiguous",
						RetryOf:     "call-conflicted",
						RetryReason: "invalid-packet-result-correction",
						Phase:       "worker-new-result-correct",
						Outcome:     "success",
						Resumed:     true,
						Ambiguity:   []string{analysisAmbiguityTargetConflicted},
						Source:      bundleAnalysisRecordTrace{ArchivePath: "task/telemetry/task-id.jsonl", Lines: []int{5}},
					},
					{
						CallID:    "call-ambiguous-resumed",
						Phase:     "worker-new",
						Outcome:   "transient_error",
						Resumed:   true,
						Ambiguity: []string{analysisAmbiguitySourceConflicted, analysisAmbiguityTargetConflicted},
						Source:    bundleAnalysisRecordTrace{ArchivePath: "task/telemetry/task-id.jsonl", Lines: []int{6}},
					},
				},
			},
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
		`"version":4`,
		`"requested_yield_ms":1000`,
		`"yield_class":"short"`,
		`"request_lines":[47]`,
		`"return_lines":[48]`,
		`"retry_of":"call-original"`,
		`"retry_reason":"invalid-packet-result-correction"`,
		`"resumed":true`,
		`"ambiguity":["target_call_id_conflicted"]`,
		`"ambiguity":["source_call_id_conflicted","target_call_id_conflicted"]`,
		`"ambiguous":[`,
		`"archive_path":"task/telemetry/task-id.jsonl"`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("serialized analysis index lost structural fact %q: %s", required, text)
		}
	}
	if len(data) >= 2048 {
		t.Fatalf("serialized analysis index overhead = %d bytes", len(data))
	}
}
