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

func (kind ResumeStopKind) ParentAction() ParentAction {
	switch kind {
	case ResumeStopRateLimited, ResumeStopProviderUnavailable, ResumeStopInterrupted:
		return ParentActionResume
	case ResumeStopGuardRecoverable:
		return ParentActionRepairGuardThenResume
	default:
		return ParentActionNone
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
	stopKind := ResumeStopNone
	if checkpointErr == nil {
		stopKind = checkpoint.StopKind
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

func parentActionPlanForStatus(status TaskStatus, pending bool, openReview string, stopKind ResumeStopKind) (ParentActionPlan, error) {
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

func waitingDecisionActionPlan(status TaskStatus, pending bool, openReview string, stopKind ResumeStopKind) (ParentActionPlan, error) {
	if !pending || stopKind != ResumeStopNone || unexpectedOpenReview(openReview, packet.StatusNeedsSolDecision) {
		return ParentActionPlan{}, lifecycleInconsistency(status, "waiting decision state does not match pending decision, parent review, and resume state")
	}
	return actionPlan(ParentActionDecision, "", ParentActionDecision), nil
}

func waitingReviewActionPlan(status TaskStatus, pending bool, openReview string, stopKind ResumeStopKind) (ParentActionPlan, error) {
	if pending || stopKind != ResumeStopNone || unexpectedOpenReview(openReview, packet.StatusNeedsSolReview) {
		return ParentActionPlan{}, lifecycleInconsistency(status, "waiting review state does not match parent review and resume state")
	}
	return actionPlan(ParentActionReview, "", ParentActionAccept, ParentActionFix), nil
}

func unexpectedOpenReview(openReview string, expected packet.Status) bool {
	return openReview != roundCommentNone && openReview != string(expected)
}

func stoppedParentActionPlan(status TaskStatus, pending bool, openReview string, stopKind ResumeStopKind) (ParentActionPlan, error) {
	switch status {
	case TaskStatusRateLimited,
		TaskStatusProviderUnavailable,
		TaskStatusInterrupted,
		TaskStatusGuardRecoverable:
	default:
		return ParentActionPlan{}, lifecycleInconsistency(status, "unknown task status")
	}
	if !stopKind.IsStopped() || stopKind.TaskStatus() != status {
		return ParentActionPlan{}, lifecycleInconsistency(status, "stopped task status does not match pending decision, parent review, and resume checkpoint")
	}
	return stoppedActionPlan(status, pending, openReview, stopKind, stopKind.ParentAction())
}

func inactiveParentActionPlan(status TaskStatus, pending bool, openReview string, stopKind ResumeStopKind) (ParentActionPlan, error) {
	if pending || openReview != roundCommentNone || stopKind != ResumeStopNone {
		return ParentActionPlan{}, lifecycleInconsistency(status, "non-waiting task has unresolved parent or resume state")
	}
	return actionPlan(ParentActionNone, ""), nil
}

func completeActionPlan(status TaskStatus, pending bool, openReview string, stopKind ResumeStopKind) (ParentActionPlan, error) {
	if pending || stopKind != ResumeStopNone {
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
	stopKind ResumeStopKind,
	required ParentAction,
) (ParentActionPlan, error) {
	if pending || openReview != roundCommentNone {
		return ParentActionPlan{}, lifecycleInconsistency(status, "stopped task status does not match pending decision, parent review, and resume checkpoint")
	}
	return actionPlan(required, string(stopKind), ParentActionResume), nil
}

func lifecycleInconsistency(status TaskStatus, detail string) error {
	return &LifecycleInconsistencyError{Status: status, Detail: detail}
}
