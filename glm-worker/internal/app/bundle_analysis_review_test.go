package app

import (
	"testing"
	"time"
)

func TestAnalysisSubsequentTurnPropagatesCounterReset(t *testing.T) {
	start := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Minute)
	baselineInput, baselineCached := int64(100), int64(50)
	resetInput, resetCached := int64(40), int64(20)
	scan := bundleRolloutScan{
		tokens: []analysisRolloutTokenAnchor{
			{
				At: start, RawAt: start.Format(time.RFC3339Nano), Offset: 10,
				Input: &baselineInput, Cached: &baselineCached,
			},
			{
				At: end, RawAt: end.Format(time.RFC3339Nano), Offset: 20,
				Input: &resetInput, Cached: &resetCached,
			},
		},
	}
	turn := analysisRolloutTurn{
		TurnID:      "turn-review-reset",
		StartedAt:   start,
		HasStart:    true,
		CompletedAt: end,
		HasComplete: true,
	}

	got := analysisSubsequentTurn(scan, &turn, end)
	if got.Status != analysisStatusCounterReset {
		t.Fatalf("status = %q want %q: %#v", got.Status, analysisStatusCounterReset, got)
	}
	if got.InputTokens != 0 || got.CachedInputTokens != 0 {
		t.Fatalf("unavailable delta leaked token values: %#v", got)
	}
	if got.BaselineAt == "" || got.EndAt == "" {
		t.Fatalf("counter reset lost anchor evidence: %#v", got)
	}
}

func TestAnalysisAnchoredTokenDeltaMarksMissingEndpointFieldUnknown(t *testing.T) {
	start := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Minute)
	baselineInput, baselineCached := int64(100), int64(50)
	endInput := int64(200)
	scan := bundleRolloutScan{
		tokens: []analysisRolloutTokenAnchor{
			{
				At: start, RawAt: start.Format(time.RFC3339Nano), Offset: 10,
				Input: &baselineInput, Cached: &baselineCached,
			},
			{
				At: end, RawAt: end.Format(time.RFC3339Nano), Offset: 20,
				Input: &endInput,
			},
		},
	}

	got := analysisAnchoredTokenDelta(scan, start, end)
	if got.Status != analysisStatusUnknown {
		t.Fatalf("status = %q want %q: %#v", got.Status, analysisStatusUnknown, got)
	}
	if got.InputTokens != 0 || got.CachedInputTokens != 0 {
		t.Fatalf("unknown delta leaked token values: %#v", got)
	}
	if got.BaselineAt == "" || got.EndAt == "" {
		t.Fatalf("unknown delta lost anchor evidence: %#v", got)
	}
}
