package app

import (
	"testing"
	"time"
)

func TestAnalysisExecutionUsageComparability(t *testing.T) {
	start := time.Now().UTC().Truncate(time.Second)
	end := start.Add(30 * time.Minute)
	ownedInitial := analysisRolloutTurn{
		TurnID:      "owned-initial",
		StartedAt:   start.Add(-time.Minute),
		HasStart:    true,
		CompletedAt: start.Add(10 * time.Minute),
		HasComplete: true,
	}
	ownedResume := analysisRolloutTurn{
		TurnID:      "owned-resume",
		StartedAt:   start.Add(12 * time.Minute),
		HasStart:    true,
		CompletedAt: start.Add(20 * time.Minute),
		HasComplete: true,
	}
	unattributed := analysisRolloutTurn{
		TurnID:      "unattributed",
		StartedAt:   start.Add(22 * time.Minute),
		HasStart:    true,
		CompletedAt: start.Add(25 * time.Minute),
		HasComplete: true,
	}

	t.Run("single-owned-turn", func(t *testing.T) {
		ownership := analysisTaskOwnership{
			status:  analysisStatusAvailable,
			initial: &ownedInitial,
			final:   &ownedInitial,
			owned:   map[string]struct{}{ownedInitial.TurnID: {}},
		}
		got := analysisExecutionUsageComparability(bundleRolloutScan{turns: []analysisRolloutTurn{ownedInitial}}, ownership, start, end)
		if got.status != analysisStatusAvailable || got.reason != "" {
			t.Fatalf("comparability = %#v", got)
		}
	})

	t.Run("exact-owned-resume-remains-comparable", func(t *testing.T) {
		ownership := analysisTaskOwnership{
			status:  analysisStatusAvailable,
			initial: &ownedInitial,
			final:   &ownedResume,
			owned: map[string]struct{}{
				ownedInitial.TurnID: {},
				ownedResume.TurnID:  {},
			},
		}
		got := analysisExecutionUsageComparability(bundleRolloutScan{turns: []analysisRolloutTurn{ownedInitial, ownedResume}}, ownership, start, end)
		if got.status != analysisStatusAvailable || got.reason != "" {
			t.Fatalf("comparability = %#v", got)
		}
	})

	t.Run("unattributed-turn-fails-closed", func(t *testing.T) {
		ownership := analysisTaskOwnership{
			status:  analysisStatusAvailable,
			initial: &ownedInitial,
			final:   &ownedResume,
			owned: map[string]struct{}{
				ownedInitial.TurnID: {},
				ownedResume.TurnID:  {},
			},
		}
		got := analysisExecutionUsageComparability(bundleRolloutScan{turns: []analysisRolloutTurn{ownedInitial, ownedResume, unattributed}}, ownership, start, end)
		if got.status != codexStatusAmbiguous || got.reason != parentUsageReasonInterleavedUnattributed {
			t.Fatalf("comparability = %#v", got)
		}
	})

	t.Run("unknown-ownership-stays-unknown", func(t *testing.T) {
		got := analysisExecutionUsageComparability(bundleRolloutScan{turns: []analysisRolloutTurn{ownedInitial}}, analysisTaskOwnership{status: analysisStatusUnknown}, start, end)
		if got.status != analysisStatusUnknown || got.reason != parentUsageReasonOwnershipUnknown {
			t.Fatalf("comparability = %#v", got)
		}
	})
}

func TestExecutionUsageOwnershipWrappers(t *testing.T) {
	start := time.Date(2026, time.September, 18, 7, 1, 27, 0, time.UTC)
	end := start.Add(90 * time.Minute)
	ownedInitial := analysisRolloutTurn{
		TurnID:      "owned-initial",
		StartedAt:   start.Add(-time.Minute),
		HasStart:    true,
		CompletedAt: start.Add(20 * time.Minute),
		HasComplete: true,
	}
	unattributed := analysisRolloutTurn{
		TurnID:      "unattributed",
		StartedAt:   start.Add(30 * time.Minute),
		HasStart:    true,
		CompletedAt: start.Add(40 * time.Minute),
		HasComplete: true,
	}
	baselineInput, endInput := int64(100), int64(8869351)
	baselineCached, endCached := int64(50), int64(8756914)
	baselineOutput, endOutput := int64(10), int64(20)
	baselineReasoning, endReasoning := int64(5), int64(8)
	baselineTotal, endTotal := int64(165), int64(17626293)
	scan := bundleRolloutScan{
		turns:         []analysisRolloutTurn{ownedInitial, unattributed},
		windowRecords: []time.Time{start.Add(time.Minute)},
		tokens: []analysisRolloutTokenAnchor{
			{
				At: start, RawAt: start.Format(time.RFC3339Nano), Offset: 10, File: 0, Source: "rollout.jsonl",
				Input: &baselineInput, Cached: &baselineCached, Output: &baselineOutput, Reasoning: &baselineReasoning, Total: &baselineTotal,
			},
			{
				At: end, RawAt: end.Format(time.RFC3339Nano), Offset: 20, File: 0, Source: "rollout.jsonl",
				Input: &endInput, Cached: &endCached, Output: &endOutput, Reasoning: &endReasoning, Total: &endTotal,
			},
		},
	}
	execution := analysisExecutionBoundary{status: analysisStatusAvailable, end: end, endBasis: analysisExecutionEndBasisLifecycleComplete}
	ownership := analysisTaskOwnership{
		status:  analysisStatusAvailable,
		initial: &ownedInitial,
		final:   &ownedInitial,
		owned:   map[string]struct{}{ownedInitial.TurnID: {}},
	}
	association := codexAssociation{ParentStatus: codexStatusIncluded}

	t.Run("analysis-index-does-not-promote-mixed-lifecycle-delta", func(t *testing.T) {
		delta := analysisExecutionTokenDeltaForOwnership(association, scan, nil, start, execution, end, ownership)
		if delta.Status != codexStatusAmbiguous || delta.InputTokens != 0 || delta.CachedInputTokens != 0 {
			t.Fatalf("delta = %#v", delta)
		}
	})

	t.Run("parent-usage-discards-mixed-lifecycle-values", func(t *testing.T) {
		interval := parentUsageExecutionIntervalForOwnership(association, scan, nil, start, execution, end, ownership)
		if interval.Tokens.Status != codexStatusAmbiguous || interval.Tokens.Reason != parentUsageReasonInterleavedUnattributed ||
			interval.Tokens.InputTokens != 0 || interval.Tokens.CachedInputTokens != 0 {
			t.Fatalf("tokens = %#v", interval.Tokens)
		}
		if interval.Activity.Status != codexStatusAmbiguous || interval.Activity.Reason != parentUsageReasonInterleavedUnattributed ||
			interval.Activity.ModelTurns != 0 || interval.Activity.ToolCalls != 0 {
			t.Fatalf("activity = %#v", interval.Activity)
		}
	})

	t.Run("existing-missing-anchor-semantics-win", func(t *testing.T) {
		missingBaseline := scan
		missingBaseline.tokens = missingBaseline.tokens[1:]
		delta := analysisExecutionTokenDeltaForOwnership(association, missingBaseline, nil, start, execution, end, ownership)
		if delta.Status != analysisStatusMissing {
			t.Fatalf("delta status = %q", delta.Status)
		}
		interval := parentUsageExecutionIntervalForOwnership(association, missingBaseline, nil, start, execution, end, ownership)
		if interval.Tokens.Status != analysisStatusMissing || interval.Tokens.Reason != parentUsageReasonBaselineAnchor {
			t.Fatalf("tokens = %#v", interval.Tokens)
		}
		if interval.Activity.Status != codexStatusAmbiguous {
			t.Fatalf("activity status = %q", interval.Activity.Status)
		}
	})
}
