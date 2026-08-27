package state

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

type ParentReviewOpenState struct {
	PacketStatus string `json:"packet_status"`
	Role         string `json:"role,omitempty"`
	ModelAlias   string `json:"model_alias,omitempty"`
	Risk         string `json:"risk,omitempty"`
}

type ParentReviewProducer struct {
	Role  string
	Model string
}

type ParentReworkOrigin struct {
	Calls            int
	WorkerCalls      int
	ReviewerCalls    int
	Turns            int
	TreeInputTokens  int64
	TreeOutputTokens int64
	WallDurationMS   int64
}

type ParentReworkSummary struct {
	ByOrigin map[string]ParentReworkOrigin
	Coverage string
}

const (
	ParentOutcomeAccepted = "accepted"
	ParentOutcomeFix      = "fix"
	ParentOutcomeDecision = "decision"
	ParentOutcomeUnknown  = "unknown"
)

const (
	ParentOriginCodexReview    = "codex-review"
	ParentOriginGLMReviewer    = "glm-reviewer"
	ParentOriginUserAmendment  = "user-amendment"
	ParentOriginExternalReview = "external-review"
	ParentOriginMetadataRepair = "metadata-repair"
	ParentOriginUnknown        = "unknown"
)

const (
	ParentPhaseAccept   = "parent-accept"
	ParentPhaseFix      = "parent-fix"
	ParentPhaseDecision = "parent-decision"
	ParentPhaseClose    = "parent-close"
)

const (
	ParentReworkCoverageComplete = "complete"
	ParentReworkCoverageUnknown  = "unknown"
)

var parentOutcomeKinds = map[string]bool{
	ParentOutcomeAccepted: true,
	ParentOutcomeFix:      true,
	ParentOutcomeDecision: true,
	ParentOutcomeUnknown:  true,
}

func ValidParentOrigin(value string) bool {
	switch value {
	case ParentOriginCodexReview, ParentOriginGLMReviewer, ParentOriginUserAmendment, ParentOriginExternalReview, ParentOriginMetadataRepair:
		return true
	}
	return false
}

func (stats *TaskStats) openParentReview(status string, risk string, producer ParentReviewProducer) {
	_, _, _ = stats.resolveParentOutcome(ParentOutcomeUnknown, "")
	stats.ParentReviewOpen = &ParentReviewOpenState{
		PacketStatus: status,
		Role:         producer.Role,
		ModelAlias:   producer.Model,
		Risk:         risk,
	}
}

func (stats *TaskStats) resolveParentOutcome(kind, origin string) (ParentReviewOpenState, bool, error) {
	if !parentOutcomeKinds[kind] {
		return ParentReviewOpenState{}, false, fmt.Errorf("unknown parent outcome kind: %s", kind)
	}
	if kind == ParentOutcomeFix && origin != "" && !ValidParentOrigin(origin) {
		return ParentReviewOpenState{}, false, fmt.Errorf("unknown parent fix origin: %s", origin)
	}
	open := stats.ParentReviewOpen
	if open == nil {
		return ParentReviewOpenState{}, false, nil
	}
	if kind == ParentOutcomeAccepted && open.PacketStatus == string(packet.StatusNeedsSolDecision) {
		return ParentReviewOpenState{}, false, fmt.Errorf("pending Sol decision must be resolved with --decision before --accept")
	}
	resolved := *open
	addInt(&stats.ParentOutcomes, kind, 1)
	if kind == ParentOutcomeFix {
		if origin == "" {
			origin = ParentOriginUnknown
		}
		addInt(&stats.ParentFixOrigins, origin, 1)
	}
	unknownLabel := ParentOriginUnknown
	model := resolved.ModelAlias
	if model == "" {
		model = unknownLabel
	}
	addInt(&stats.ParentOutcomesByModel, model, 1)
	risk := resolved.Risk
	if risk == "" {
		risk = unknownLabel
	}
	addInt(&stats.ParentOutcomesByRisk, risk, 1)
	stats.ParentReviewOpen = nil
	return resolved, true, nil
}

func (s *StateStore) RecordParentOutcome(kind, origin string) (bool, error) {
	stats, err := s.loadTaskStats()
	if err != nil {
		stats, err = s.recoverTaskStats(err)
		if err != nil {
			return false, nil
		}
	}
	resolved, ok, resolveErr := stats.resolveParentOutcome(kind, origin)
	if !ok || resolveErr != nil {
		return ok, resolveErr
	}
	if err := s.writeTaskStats(stats); err != nil {
		warnStatsFailure("parent outcome更新", err)
		return false, nil
	}
	s.appendParentOutcomeEvent(stats.TaskID, parentPhaseOfKind(kind), kind, origin, resolved)
	return true, nil
}

func (s *StateStore) OpenParentReviewLabel() string {
	stats, err := s.loadTaskStats()
	if err != nil || stats.ParentReviewOpen == nil {
		return "none"
	}
	return stats.ParentReviewOpen.PacketStatus
}

func parentPhaseOfKind(kind string) string {
	switch kind {
	case ParentOutcomeAccepted:
		return ParentPhaseAccept
	case ParentOutcomeFix:
		return ParentPhaseFix
	case ParentOutcomeDecision:
		return ParentPhaseDecision
	default:
		return ParentPhaseClose
	}
}

func (s *StateStore) appendParentOutcomeEvent(taskID string, phase string, kind string, origin string, resolved ParentReviewOpenState) {
	now := time.Now().UTC()
	s.RecordModelCallLog(ModelCallLog{
		TaskID:             taskID,
		CallType:           CallTypeEvent,
		StartedAt:          now,
		CompletedAt:        now,
		Phase:              phase,
		Role:               SessionRole(resolved.Role),
		Outcome:            kind,
		PacketStatus:       resolved.PacketStatus,
		ModelAlias:         resolved.ModelAlias,
		WorkerReportedRisk: resolved.Risk,
		ParentOrigin:       origin,
	})
}

func isParentOutcomePhase(phase string) bool {
	switch phase {
	case ParentPhaseAccept, ParentPhaseFix, ParentPhaseDecision, ParentPhaseClose:
		return true
	}
	return false
}

func (s *StateStore) ComputeParentRework(tasks []TaskStats) ParentReworkSummary {
	summary := ParentReworkSummary{ByOrigin: make(map[string]ParentReworkOrigin), Coverage: ParentReworkCoverageComplete}
	for _, task := range tasks {
		if !s.accumulateParentReworkTask(&summary, task) {
			summary.Coverage = ParentReworkCoverageUnknown
		}
	}
	return summary
}

func (s *StateStore) accumulateParentReworkTask(summary *ParentReworkSummary, task TaskStats) bool {
	logs, err := s.ReadModelCallLogs(task.TaskID)
	if err != nil {
		return errors.Is(err, os.ErrNotExist) && task.ModelCalls == 0
	}
	origin := ""
	taskCalls := 0
	for _, record := range logs {
		if nextOrigin, handled := parentReworkOriginTransition(origin, record); handled {
			origin = nextOrigin
			continue
		}
		if record.CallType != CallTypeTask {
			continue
		}
		taskCalls++
		if origin != "" {
			addParentReworkCall(summary.ByOrigin, origin, record)
		}
	}
	return taskCalls == task.ModelCalls
}

func parentReworkOriginTransition(current string, record ModelCallLog) (string, bool) {
	if record.CallType != CallTypeEvent || !isParentOutcomePhase(record.Phase) {
		return current, false
	}
	if record.Outcome != ParentOutcomeFix {
		return "", true
	}
	origin := record.ParentOrigin
	if origin == "" {
		origin = ParentOriginUnknown
	}
	return origin, true
}

func addParentReworkCall(byOrigin map[string]ParentReworkOrigin, origin string, record ModelCallLog) {
	entry := byOrigin[origin]
	entry.Calls++
	switch record.Role {
	case WorkerRole:
		entry.WorkerCalls++
	case ReviewerRole:
		entry.ReviewerCalls++
	}
	entry.Turns += record.TopLevelTurns
	usage := modelCallTreeUsage(record)
	entry.TreeInputTokens += usage.InputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens
	entry.TreeOutputTokens += usage.OutputTokens
	entry.WallDurationMS += record.WallDurationMS
	byOrigin[origin] = entry
}
