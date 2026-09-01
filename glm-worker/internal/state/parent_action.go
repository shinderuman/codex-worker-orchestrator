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
	ParentActionNoGo                  ParentAction = "no-go"
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

func (p ParentActionPlan) AdmitsCommand(action ParentAction) bool {
	if p.Allows(action) {
		return true
	}
	if action != ParentActionAccept {
		return false
	}
	switch p.RequiredAction {
	case ParentActionDecision, ParentActionReview, ParentActionAccept:
		return false
	default:
		return true
	}
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
	plan, err := parentActionPlanForStatus(status, pending, openReview, stopKind)
	if err != nil {
		return ParentActionPlan{}, err
	}
	if status == TaskStatusWaitingDecision && s.ObservationNoGoEligible() {
		plan.AllowedActions = append(plan.AllowedActions, ParentActionNoGo)
	}
	return plan, nil
}

func parentActionPlanForStatus(status TaskStatus, pending bool, openReview, stopKind string) (ParentActionPlan, error) {
	switch status {
	case TaskStatusWaitingDecision:
		return waitingDecisionActionPlan(status, pending, openReview, stopKind)
	case TaskStatusWaitingSolReview:
		return waitingReviewActionPlan(status, pending, openReview, stopKind)
	case TaskStatusComplete:
		return completeActionPlan(status, pending, openReview, stopKind)
	case TaskStatusActive, TaskStatusNone:
		return inactiveParentActionPlan(status, pending, openReview, stopKind)
	default:
		return stoppedParentActionPlan(status, pending, openReview, stopKind)
	}
}

func waitingDecisionActionPlan(status TaskStatus, pending bool, openReview, stopKind string) (ParentActionPlan, error) {
	if !pending || stopKind != "" || unexpectedOpenReview(openReview, packet.StatusNeedsSolDecision) {
		return ParentActionPlan{}, lifecycleInconsistency(status, "waiting decision state does not match pending decision, parent review, and resume state")
	}
	return actionPlan(ParentActionDecision, "", ParentActionDecision), nil
}

func waitingReviewActionPlan(status TaskStatus, pending bool, openReview, stopKind string) (ParentActionPlan, error) {
	if pending || stopKind != "" || unexpectedOpenReview(openReview, packet.StatusNeedsSolReview) {
		return ParentActionPlan{}, lifecycleInconsistency(status, "waiting review state does not match parent review and resume state")
	}
	return actionPlan(ParentActionReview, "", ParentActionAccept, ParentActionFix), nil
}

func unexpectedOpenReview(openReview string, expected packet.Status) bool {
	return openReview != roundCommentNone && openReview != string(expected)
}

func stoppedParentActionPlan(status TaskStatus, pending bool, openReview, stopKind string) (ParentActionPlan, error) {
	switch status {
	case TaskStatusRateLimited:
		return stoppedActionPlan(status, pending, openReview, stopKind, "rate-limited", ParentActionResume)
	case TaskStatusProviderUnavailable:
		return stoppedActionPlan(status, pending, openReview, stopKind, "provider-unavailable", ParentActionResume)
	case TaskStatusInterrupted:
		return stoppedActionPlan(status, pending, openReview, stopKind, "interrupted", ParentActionResume)
	case TaskStatusGuardRecoverable:
		return stoppedActionPlan(status, pending, openReview, stopKind, "guard-recoverable", ParentActionRepairGuardThenResume)
	default:
		return ParentActionPlan{}, lifecycleInconsistency(status, "unknown task status")
	}
}

func inactiveParentActionPlan(status TaskStatus, pending bool, openReview, stopKind string) (ParentActionPlan, error) {
	if pending || openReview != roundCommentNone || stopKind != "" {
		return ParentActionPlan{}, lifecycleInconsistency(status, "non-waiting task has unresolved parent or resume state")
	}
	return actionPlan(ParentActionNone, ""), nil
}

func completeActionPlan(status TaskStatus, pending bool, openReview string, stopKind string) (ParentActionPlan, error) {
	if pending || stopKind != "" {
		return ParentActionPlan{}, lifecycleInconsistency(status, "complete task has pending decision or resumable stop state")
	}
	switch openReview {
	case roundCommentNone:
		return actionPlan(ParentActionNone, ""), nil
	case string(packet.StatusPass):
		return actionPlan(ParentActionAccept, "", ParentActionAccept), nil
	default:
		return ParentActionPlan{}, lifecycleInconsistency(status, "complete task has a non-PASS parent review")
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
	if pending || openReview != roundCommentNone || stopKind != expectedKind {
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
