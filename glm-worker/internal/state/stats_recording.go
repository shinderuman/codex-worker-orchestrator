package state

import (
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func (s *StateStore) RecordModelCall(role SessionRole, model string) {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.ModelCalls++
		if stats.ModelCallsByAlias == nil {
			stats.ModelCallsByAlias = make(map[string]int)
		}
		stats.ModelCallsByAlias[model]++
		switch role {
		case ReviewerRole:
			stats.ReviewerCalls++
		case FailurePathReviewerRole:
			stats.FailurePathReviewerCalls++
		default:
			stats.WorkerCalls++
		}
	})
}

func (s *StateStore) RecordTransientRetry() {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.TransientRetries++
	})
}

func (s *StateStore) RecordModelDuration(model string, duration time.Duration) {
	s.UpdateTaskStats(func(stats *TaskStats) {
		if stats.ModelDurationMSByAlias == nil {
			stats.ModelDurationMSByAlias = make(map[string]int64)
		}
		stats.ModelDurationMSByAlias[model] += duration.Milliseconds()
	})
}

func (s *StateStore) RecordDecision() {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.DecisionCommands++
	})
}

func (s *StateStore) RecordFix() {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.FixCommands++
	})
}

func (s *StateStore) RecordResume() {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.ResumeCommands++
	})
}

func (s *StateStore) RecordAutoFix() {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.AutoFixRounds++
	})
}

func (s *StateStore) RecordRateLimit(model string) {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.RateLimits++
		if stats.RateLimitsByAlias == nil {
			stats.RateLimitsByAlias = make(map[string]int)
		}
		stats.RateLimitsByAlias[model]++
	})
}

func (s *StateStore) RecordProviderUnavailable(model string) {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.ProviderUnavailable++
		if stats.ProviderUnavailableByAlias == nil {
			stats.ProviderUnavailableByAlias = make(map[string]int)
		}
		stats.ProviderUnavailableByAlias[model]++
	})
}

func (s *StateStore) RecordResultCorrection() {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.ResultCorrections++
	})
}

func (s *StateStore) RecordStructuredRetryExhausted() {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.StructuredRetryExhausted++
	})
}

func (s *StateStore) RecordRiskFloor(category string) {
	if category == "" {
		return
	}
	s.UpdateTaskStats(func(stats *TaskStats) {
		addInt(&stats.RiskFloorByCategory, category, 1)
	})
}

func (s *StateStore) RecordSnapshotMismatch(axis string) {
	if axis == "" {
		return
	}
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.SnapshotMismatches++
		for _, a := range strings.Split(axis, ",") {
			addInt(&stats.SnapshotMismatchByAxis, a, 1)
		}
	})
}

func (s *StateStore) RecordPacketReject(category string) {
	if category == "" {
		return
	}
	s.UpdateTaskStats(func(stats *TaskStats) {
		addInt(&stats.PacketRejectByCategory, category, 1)
	})
}

func (s *StateStore) RecordProbeOutcome(outcome string) {
	if outcome == "" {
		return
	}
	s.UpdateTaskStats(func(stats *TaskStats) {
		addInt(&stats.ProbeOutcome, outcome, 1)
	})
}

func (s *StateStore) RecordSolResult(value packet.Result, producer ParentReviewProducer) error {
	return s.recordSolResult(value, producer, nil)
}

func (s *StateStore) RecordSolResultWithReviewSnapshot(value packet.Result, producer ParentReviewProducer, snapshot SnapshotDigest) error {
	return s.recordSolResult(value, producer, &snapshot)
}

func (s *StateStore) recordSolResult(value packet.Result, producer ParentReviewProducer, reviewSnapshot *SnapshotDigest) error {
	var err error
	if value.Status == packet.StatusNeedsSolReview && reviewSnapshot != nil {
		err = s.openBoundParentReviewState(value, producer, *reviewSnapshot)
	} else {
		err = s.openParentReviewState(string(value.Status), string(value.Risk), producer, false)
	}
	if err != nil {
		return err
	}
	s.recordSolOutcomeStats(value, producer)
	return nil
}

func (s *StateStore) recordSolOutcomeStats(value packet.Result, producer ParentReviewProducer) {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.SolPacketBytes += value.ByteSize()
		switch value.Status {
		case packet.StatusNeedsSolDecision:
			stats.NeedsSolDecisionPackets++
		case packet.StatusNeedsSolReview:
			stats.NeedsSolReviewPackets++
		case packet.StatusPass:
			stats.PassPackets++
		}
		stats.openParentReview(string(value.Status), string(value.Risk), producer)
	})
}
