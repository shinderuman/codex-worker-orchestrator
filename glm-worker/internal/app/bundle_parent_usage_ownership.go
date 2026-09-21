package app

import "time"

type analysisUsageComparability struct {
	status string
	reason string
}

const parentUsageReasonInterleavedUnattributed = "interleaved-unattributed-turn"

const parentUsageReasonSameTurnInterleaved = "same-turn-interleaved-user-message"

const parentUsageReasonOwnershipUnknown = "task-ownership-unknown"

func analysisExecutionUsageComparability(scan bundleRolloutScan, ownership analysisTaskOwnership, start, end time.Time) analysisUsageComparability {
	if ownership.status != analysisStatusAvailable || ownership.initial == nil || len(ownership.owned) == 0 {
		return analysisUsageComparability{status: analysisStatusUnknown, reason: parentUsageReasonOwnershipUnknown}
	}
	if ownership.sameTurnInterleaved {
		return analysisUsageComparability{status: codexStatusAmbiguous, reason: parentUsageReasonSameTurnInterleaved}
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

func analysisExecutionTokenDeltaForOwnership(association codexAssociation, scan bundleRolloutScan, scanErr error, start time.Time, execution analysisExecutionBoundary, collectionEnd time.Time, ownership analysisTaskOwnership) bundleAnalysisTokenDelta {
	delta := analysisExecutionTokenDelta(association, scan, scanErr, start, execution, collectionEnd)
	if delta.Status != analysisStatusAvailable && delta.Status != analysisStatusOpen {
		return delta
	}
	endBound := collectionEnd
	if execution.status == analysisStatusAvailable {
		endBound = execution.end
	}
	comparability := analysisExecutionUsageComparability(scan, ownership, start, endBound)
	if comparability.status != analysisStatusAvailable {
		return bundleAnalysisTokenDelta{Status: comparability.status}
	}
	return delta
}

func parentUsageExecutionIntervalForOwnership(association codexAssociation, scan bundleRolloutScan, scanErr error, start time.Time, execution analysisExecutionBoundary, collectionEnd time.Time, ownership analysisTaskOwnership) parentUsageInterval {
	interval := parentUsageExecutionInterval(association, scan, scanErr, start, execution, collectionEnd)
	if association.ParentStatus != codexStatusIncluded || scanErr != nil || execution.status == analysisStatusUnknown {
		return interval
	}
	endBound := collectionEnd
	if execution.status == analysisStatusAvailable {
		endBound = execution.end
	}
	comparability := analysisExecutionUsageComparability(scan, ownership, start, endBound)
	if comparability.status == analysisStatusAvailable {
		return interval
	}
	if interval.Tokens.Status == analysisStatusAvailable || interval.Tokens.Status == analysisStatusOpen {
		interval.Tokens = parentUsageTokens{Status: comparability.status, Reason: comparability.reason}
	}
	if interval.Activity.Status == analysisStatusCounted || interval.Activity.Status == analysisStatusOpen {
		interval.Activity = parentUsageActivity{Status: comparability.status, Reason: comparability.reason}
	}
	return interval
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
