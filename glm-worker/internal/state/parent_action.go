package state

import (
	"errors"
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

type ParentAction string

type ParentActionPlan struct {
	RequiredAction           ParentAction      `json:"required_action"`
	AllowedActions           []ParentAction    `json:"allowed_actions"`
	ResumeKind               string            `json:"resume_kind,omitempty"`
	RequiredActionParameters map[string]string `json:"required_action_parameters,omitempty"`
}

type LifecycleInconsistencyError struct {
	Status TaskStatus
	Detail string
}

const (
	ParentActionNone                        ParentAction = "none"
	ParentActionDecision                    ParentAction = "decision"
	ParentActionNoGo                        ParentAction = "no-go"
	ParentActionReview                      ParentAction = "parent-review"
	ParentActionApproveSurface              ParentAction = "approve-surface"
	ParentActionAccept                      ParentAction = "accept"
	ParentActionComplete                    ParentAction = "complete"
	ParentActionInstall                     ParentAction = "install"
	ParentActionFix                         ParentAction = "fix"
	ParentActionResume                      ParentAction = "resume"
	ParentActionPark                        ParentAction = "park"
	ParentActionUnpark                      ParentAction = "unpark"
	ParentActionRepairGuardThenResume       ParentAction = "repair-guard-then-resume"
	ParentActionRepairQualityGateThenResume ParentAction = "repair-quality-gate-then-resume"
)

const approveSurfaceScopeParameter = "accepted-scope"

var approveSurfaceParameters = map[string]string{approveSurfaceScopeParameter: "current-diff"}

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
	case ParentActionDecision, ParentActionReview, ParentActionApproveSurface, ParentActionAccept, ParentActionComplete:
		return false
	default:
		return true
	}
}

func (s *StateStore) AdmitParentAction(action ParentAction) (ParentActionPlan, bool, error) {
	plan, err := s.ParentActionPlan()
	if err != nil {
		return ParentActionPlan{}, false, err
	}
	return plan, plan.AdmitsCommand(action), nil
}

func (s *StateStore) AdmitNewTask() (ParentActionPlan, bool, error) {
	plan, err := s.ParentActionPlan()
	if err != nil {
		return ParentActionPlan{}, false, err
	}
	return plan, plan.RequiredAction == ParentActionNone, nil
}

func (kind ResumeStopKind) ParentAction() ParentAction {
	switch kind {
	case ResumeStopRateLimited, ResumeStopProviderUnavailable, ResumeStopInterrupted:
		return ParentActionResume
	case ResumeStopGuardRecoverable:
		return ParentActionRepairGuardThenResume
	case ResumeStopQualityGate:
		return ParentActionRepairQualityGateThenResume
	default:
		return ParentActionNone
	}
}

func (s *StateStore) ParentActionPlan() (ParentActionPlan, error) {
	status := s.TaskStatus()
	pending := s.Exists("pending-decision")
	openReview, reviewErr := s.CurrentParentReviewLabel()
	if reviewErr != nil {
		return ParentActionPlan{}, lifecycleInconsistency(status, "parent review state is unreadable: "+reviewErr.Error())
	}
	checkpoint, checkpointErr := s.LoadResumeCheckpoint()
	if checkpointErr != nil && !errors.Is(checkpointErr, ErrNoResumeCheckpoint) {
		return ParentActionPlan{}, lifecycleInconsistency(status, "resume checkpoint is unreadable")
	}
	stopKind := ResumeStopNone
	pendingDecisionResume := false
	qualitySurfaceApproval := false
	if checkpointErr == nil {
		stopKind = checkpoint.StopKind
		pendingDecisionResume = pendingDecisionContinuesCheckpoint(pending, checkpoint, s.readExact("last-decision"))
		qualitySurfaceApproval = checkpoint.QualitySurfaceApprovalPending && !checkpoint.IsStopped()
	}
	plan, err := s.parentActionPlanForStatus(status, pending, pendingDecisionResume, openReview, stopKind, qualitySurfaceApproval)
	if err != nil {
		return ParentActionPlan{}, err
	}
	if status == TaskStatusWaitingDecision && s.ObservationNoGoEligible() {
		plan.AllowedActions = append(plan.AllowedActions, ParentActionNoGo)
	}
	return plan, nil
}

func (s *StateStore) parentActionPlanForStatus(status TaskStatus, pending bool, pendingDecisionResume bool, openReview string, stopKind ResumeStopKind, qualitySurfaceApproval bool) (ParentActionPlan, error) {
	switch status {
	case TaskStatusWaitingDecision:
		return waitingDecisionActionPlan(status, pending, openReview, stopKind)
	case TaskStatusWaitingSolReview:
		return waitingReviewActionPlan(status, pending, openReview, stopKind, qualitySurfaceApproval)
	case TaskStatusAwaitingParentCompletion:
		return awaitingParentCompletionActionPlan(status, pending, openReview, stopKind)
	case TaskStatusParked:
		return s.parkedActionPlan(status, pending, openReview, stopKind)
	case TaskStatusComplete:
		return completeActionPlan(status, pending, openReview, stopKind)
	case TaskStatusActive, TaskStatusNone:
		return inactiveParentActionPlan(status, pending, openReview, stopKind)
	default:
		return stoppedParentActionPlan(status, pending, pendingDecisionResume, openReview, stopKind)
	}
}

func waitingDecisionActionPlan(status TaskStatus, pending bool, openReview string, stopKind ResumeStopKind) (ParentActionPlan, error) {
	if !pending || stopKind != ResumeStopNone || unexpectedOpenReview(openReview, packet.StatusNeedsSolDecision) {
		return ParentActionPlan{}, lifecycleInconsistency(status, "waiting decision state does not match pending decision, parent review, and resume state")
	}
	return actionPlan(ParentActionDecision, "", ParentActionDecision, ParentActionPark), nil
}

func waitingReviewActionPlan(status TaskStatus, pending bool, openReview string, stopKind ResumeStopKind, qualitySurfaceApproval bool) (ParentActionPlan, error) {
	if pending || stopKind != ResumeStopNone || unexpectedOpenReview(openReview, packet.StatusNeedsSolReview) {
		return ParentActionPlan{}, lifecycleInconsistency(status, "waiting review state does not match parent review and resume state")
	}
	if qualitySurfaceApproval {
		return ParentActionPlan{
			RequiredAction:           ParentActionApproveSurface,
			AllowedActions:           []ParentAction{ParentActionApproveSurface, ParentActionFix, ParentActionPark},
			RequiredActionParameters: approveSurfaceParameters,
		}, nil
	}
	return actionPlan(ParentActionReview, "", ParentActionAccept, ParentActionFix, ParentActionPark), nil
}

func awaitingParentCompletionActionPlan(status TaskStatus, pending bool, openReview string, stopKind ResumeStopKind) (ParentActionPlan, error) {
	if pending || stopKind != ResumeStopNone || openReview != roundCommentNone {
		return ParentActionPlan{}, lifecycleInconsistency(status, "awaiting parent completion task has unresolved parent or resume state")
	}
	return actionPlan(ParentActionComplete, "", ParentActionComplete, ParentActionInstall), nil
}

func (s *StateStore) parkedActionPlan(status TaskStatus, pending bool, openReview string, stopKind ResumeStopKind) (ParentActionPlan, error) {
	if stopKind != ResumeStopNone {
		return ParentActionPlan{}, lifecycleInconsistency(status, "parked task must not carry a resumable stop checkpoint")
	}
	if openReview != roundCommentNone && openReview != string(packet.StatusNeedsSolReview) && openReview != string(packet.StatusNeedsSolDecision) {
		return ParentActionPlan{}, lifecycleInconsistency(status, "parked task has an unexpected parent review label")
	}
	record, err := s.LoadParkRecord()
	if err != nil {
		return ParentActionPlan{}, lifecycleInconsistency(status, "parked task has no readable park record")
	}
	if err := validateParkedOriginState(status, record.FromStatus, pending, openReview); err != nil {
		return ParentActionPlan{}, err
	}
	return actionPlan(ParentActionUnpark, "", ParentActionUnpark), nil
}

func validateParkedOriginState(status TaskStatus, from TaskStatus, pending bool, openReview string) error {
	switch from {
	case TaskStatusWaitingDecision:
		if !pending {
			return lifecycleInconsistency(status, "parked decision-origin task lost its pending decision")
		}
		if unexpectedOpenReview(openReview, packet.StatusNeedsSolDecision) {
			return lifecycleInconsistency(status, "parked decision-origin task has a review label outside the pending decision lease")
		}
	case TaskStatusWaitingSolReview:
		if pending {
			return lifecycleInconsistency(status, "parked review-origin task carries a pending decision")
		}
		if unexpectedOpenReview(openReview, packet.StatusNeedsSolReview) {
			return lifecycleInconsistency(status, "parked review-origin task has a review label outside the review lease")
		}
	default:
		return lifecycleInconsistency(status, fmt.Sprintf("park record from-status %s is not unparkable", from))
	}
	return nil
}

func unexpectedOpenReview(openReview string, expected packet.Status) bool {
	return openReview != roundCommentNone && openReview != string(expected)
}

func stoppedParentActionPlan(status TaskStatus, pending bool, pendingDecisionResume bool, openReview string, stopKind ResumeStopKind) (ParentActionPlan, error) {
	switch status {
	case TaskStatusRateLimited,
		TaskStatusProviderUnavailable,
		TaskStatusInterrupted,
		TaskStatusGuardRecoverable,
		TaskStatusQualityGateRecoverable:
	default:
		return ParentActionPlan{}, lifecycleInconsistency(status, "unknown task status")
	}
	if !stopKind.IsStopped() || stopKind.TaskStatus() != status {
		return ParentActionPlan{}, lifecycleInconsistency(status, "stopped task status does not match pending decision, parent review, and resume checkpoint")
	}
	return stoppedActionPlan(status, pending, pendingDecisionResume, openReview, stopKind, stopKind.ParentAction())
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
	pendingDecisionResume bool,
	openReview string,
	stopKind ResumeStopKind,
	required ParentAction,
) (ParentActionPlan, error) {
	if pending && !pendingDecisionResume || openReview != roundCommentNone {
		return ParentActionPlan{}, lifecycleInconsistency(status, "stopped task status does not match pending decision, parent review, and resume checkpoint")
	}
	return actionPlan(required, string(stopKind), ParentActionResume), nil
}

func pendingDecisionContinuesCheckpoint(pending bool, checkpoint ResumeCheckpoint, lastDecision string) bool {
	if !pending {
		return false
	}
	switch checkpoint.StopKind {
	case ResumeStopRateLimited, ResumeStopProviderUnavailable, ResumeStopInterrupted:
	default:
		return false
	}
	if checkpoint.Stage != ResumeStageWorker || WorkerPhaseCategory(checkpoint.Phase) != WorkerPhaseCategoryDecision {
		return false
	}
	return strings.TrimSpace(checkpoint.Decision) != "" && checkpoint.Decision == lastDecision
}

func lifecycleInconsistency(status TaskStatus, detail string) error {
	return &LifecycleInconsistencyError{Status: status, Detail: detail}
}
