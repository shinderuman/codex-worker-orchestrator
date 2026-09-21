package app

import "time"

type analysisUsageComparability struct {
	status string
	reason string
}

const parentUsageReasonInterleavedUnattributed = "interleaved-unattributed-turn"

const parentUsageReasonOwnershipUnknown = "task-ownership-unknown"

func analysisExecutionUsageComparability(scan bundleRolloutScan, ownership analysisTaskOwnership, start, end time.Time) analysisUsageComparability {
	if ownership.status != analysisStatusAvailable || ownership.initial == nil || len(ownership.owned) == 0 {
		return analysisUsageComparability{status: analysisStatusUnknown, reason: parentUsageReasonOwnershipUnknown}
	}
	for index := range scan.turns {
		turn := &scan.turns[index]
		if !analysisUsageTurnOverlaps(turn, start, end) {
			continue
		}
		if analysisTaskOwnsTurn(ownership, turn) {
			continue
		}
		return analysisUsageComparability{status: codexStatusAmbiguous, reason: parentUsageReasonInterleavedUnattributed}
	}
	return analysisUsageComparability{status: analysisStatusAvailable}
}

func analysisUsageTurnOverlaps(turn *analysisRolloutTurn, start, end time.Time) bool {
	if !turn.HasStart || turn.StartedAt.After(end) {
		return false
	}
	if turn.HasComplete && turn.CompletedAt.Before(start) {
		return false
	}
	return true
}
