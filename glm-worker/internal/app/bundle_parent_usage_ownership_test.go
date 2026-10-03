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
