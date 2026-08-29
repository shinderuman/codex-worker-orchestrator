package state

import (
	"errors"
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

type ParentAction string

type ParentActionPlan struct {
	RequiredAction ParentAction   `json:"required_action"`
	AllowedActions []ParentAction `json:"allowed_actions"`
	ResumeKind     string         `json:"resume_kind,omitempty"`
}

type LifecycleInconsistencyError struct {
	Status TaskStatus
	Detail string
}

const (
	ParentActionNone                  ParentAction = "none"
	ParentActionDecision              ParentAction = "decision"
	ParentActionReview                ParentAction = "parent-review"
	ParentActionAccept                ParentAction = "accept"
	ParentActionFix                   ParentAction = "fix"
	ParentActionResume                ParentAction = "resume"
	ParentActionRepairGuardThenResume ParentAction = "repair-guard-then-resume"
)

func (e *LifecycleInconsistencyError) Error() string {
	return fmt.Sprintf("lifecycle inconsistency for task status %s: %s", e.Status, e.Detail)
}

func (p ParentActionPlan) Allows(action ParentAction) bool {
	for _, allowed := range p.AllowedActions {
		if allowed == action {
			return true
		}
	}
	return false
}

func (s *StateStore) ParentActionPlan() (ParentActionPlan, error) {
	status := s.TaskStatus()
	pending := s.Exists("pending-decision")
	openReview := s.OpenParentReviewLabel()
	checkpoint, checkpointErr := s.LoadResumeCheckpoint()
	if checkpointErr != nil && !errors.Is(checkpointErr, ErrNoResumeCheckpoint) {
		return ParentActionPlan{}, lifecycleInconsistency(status, "resume checkpoint is unreadable")
	}
	stopKind, stopErr := checkpointStopKind(checkpoint, checkpointErr == nil)
	if stopErr != nil {
		return ParentActionPlan{}, lifecycleInconsistency(status, stopErr.Error())
	}

	switch status {
	case TaskStatusWaitingDecision:
		if !pending || openReview != string(packet.StatusNeedsSolDecision) || stopKind != "" {
			return ParentActionPlan{}, lifecycleInconsistency(status, "waiting decision state does not match pending decision, parent review, and resume state")
		}
		return actionPlan(ParentActionDecision, "", ParentActionDecision), nil
	case TaskStatusWaitingSolReview:
		if pending || openReview != string(packet.StatusNeedsSolReview) || stopKind != "" {
			return ParentActionPlan{}, lifecycleInconsistency(status, "waiting review state does not match parent review and resume state")
		}
		return actionPlan(ParentActionReview, "", ParentActionAccept, ParentActionFix), nil
	case TaskStatusRateLimited:
		return stoppedActionPlan(status, pending, openReview, stopKind, "rate-limited", ParentActionResume)
	case TaskStatusProviderUnavailable:
		return stoppedActionPlan(status, pending, openReview, stopKind, "provider-unavailable", ParentActionResume)
	case TaskStatusInterrupted:
		return stoppedActionPlan(status, pending, openReview, stopKind, "interrupted", ParentActionResume)
	case TaskStatusGuardRecoverable:
		return stoppedActionPlan(status, pending, openReview, stopKind, "guard-recoverable", ParentActionRepairGuardThenResume)
	case TaskStatusActive, TaskStatusComplete, TaskStatusNone:
		if pending || openReview != "none" || stopKind != "" {
			return ParentActionPlan{}, lifecycleInconsistency(status, "non-waiting task has unresolved parent or resume state")
		}
		return actionPlan(ParentActionNone, ""), nil
	default:
		return ParentActionPlan{}, lifecycleInconsistency(status, "unknown task status")
	}
}

func actionPlan(required ParentAction, resumeKind string, allowed ...ParentAction) ParentActionPlan {
	return ParentActionPlan{RequiredAction: required, AllowedActions: allowed, ResumeKind: resumeKind}
}

func stoppedActionPlan(
	status TaskStatus,
	pending bool,
	openReview string,
	stopKind string,
	expectedKind string,
	required ParentAction,
) (ParentActionPlan, error) {
	if pending || openReview != "none" || stopKind != expectedKind {
		return ParentActionPlan{}, lifecycleInconsistency(status, "stopped task status does not match pending decision, parent review, and resume checkpoint")
	}
	return actionPlan(required, stopKind, ParentActionResume), nil
}

func checkpointStopKind(checkpoint ResumeCheckpoint, available bool) (string, error) {
	if !available {
		return "", nil
	}
	kinds := make([]string, 0, 4)
	if checkpoint.RateLimited {
		kinds = append(kinds, "rate-limited")
	}
	if checkpoint.ProviderUnavailable {
		kinds = append(kinds, "provider-unavailable")
	}
	if checkpoint.UserInterrupted {
		kinds = append(kinds, "interrupted")
	}
	if checkpoint.GuardRecoverable {
		kinds = append(kinds, "guard-recoverable")
	}
	if len(kinds) > 1 {
		return "", fmt.Errorf("resume checkpoint contains multiple stop reasons")
	}
	if len(kinds) == 0 {
		return "", nil
	}
	return kinds[0], nil
}

func lifecycleInconsistency(status TaskStatus, detail string) error {
	return &LifecycleInconsistencyError{Status: status, Detail: detail}
}
