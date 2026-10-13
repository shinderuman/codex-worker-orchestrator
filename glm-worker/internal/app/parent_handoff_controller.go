package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func applyCanonicalControllerHandoff(cfg config.AppConfig, output *parentHandoffOutput) {
	report, ok := canonicalHandoffStatus(cfg)
	if !ok {
		return
	}
	output.Controller = &report
	if !output.Consistent {
		return
	}
	output.AllowedActions = canonicalParentActions(output.AllowedActions)
	if output.RequiredAction != nil {
		action := canonicalParentAction(*output.RequiredAction)
		if action != *output.RequiredAction {
			output.RequiredActionParameters = nil
		}
		output.RequiredAction = &action
	}
	if report.Head.PendingTransitionID != "" || report.Head.LiveLeaseID == "" {
		applyCanonicalQuiescentHandoff(cfg, output, report.Head)
	}
	if output.ParentRequest != nil && output.RequiredAction != nil {
		output.ParentRequest.Continuation.RequiredAction = *output.RequiredAction
	}
}

func applyCanonicalQuiescentHandoff(cfg config.AppConfig, output *parentHandoffOutput, head controller.RepositoryControllerHead) {
	action := canonicalQuiescentAction(cfg, head, output.TaskID)
	if action == "start" {
		authority, err := controller.ResolveCommittedTaskAuthority(cfg.RepoRoot)
		if err != nil {
			output.Consistent = false
			message := err.Error()
			output.Inconsistency = &message
			output.AllowedActions = nil
			output.RequiredAction = nil
			return
		}
		if authority.ProjectSnapshotID != head.ProjectSnapshotID {
			action = controllerPublicationSurface
		}
	}
	output.RequiredAction = &action
	output.AllowedActions = []string{action, "controller-evidence"}
	if action == controllerPublicationSurface && output.ParentRequest != nil {
		output.ParentRequest.TaskAttribution.LegalNextAction = action
	}
}

func canonicalParentAction(action string) string {
	switch state.ParentAction(action) {
	case state.ParentActionComplete, state.ParentActionInstall, state.ParentActionReopen:
		return controllerPublicationSurface
	case state.ParentActionPark:
		return controllerExecutionSurface
	case state.ParentActionNoGo, state.ParentActionBindDefectTask, state.ParentActionImprovementDisposition:
		return "controller-semantic"
	default:
		return action
	}
}

func canonicalParentActions(actions []string) []string {
	projected := []string{}
	for _, action := range append(actions, "controller-semantic", controllerExecutionSurface, "controller-evidence") {
		action = canonicalParentAction(action)
		if !containsParentAction(projected, action) {
			projected = append(projected, action)
		}
	}
	return projected
}

func canonicalQuiescentAction(cfg config.AppConfig, head controller.RepositoryControllerHead, taskID *string) string {
	if head.PendingTransitionID != "" {
		return controllerExecutionSurface
	}
	if head.ExecutionTaskRef == nil {
		if head.MetadataLineageRef != nil && taskID == nil {
			return "start"
		}
		return controllerExecutionSurface
	}
	if head.AcceptedCandidateRef == nil {
		return controllerExecutionSurface
	}
	store, err := controller.Open(cfg)
	if err != nil {
		return controllerExecutionSurface
	}
	awaitMaterialization, err := store.QuiescentCandidateAwaitMaterialization(head)
	if err != nil || awaitMaterialization {
		return controllerExecutionSurface
	}
	return controllerPublicationSurface
}

func canonicalHandoffStatus(cfg config.AppConfig) (controller.ControllerStatusReport, bool) {
	exists, err := controller.Exists(cfg)
	if err != nil || !exists {
		return controller.ControllerStatusReport{}, false
	}
	store, err := controller.Open(cfg)
	if err != nil {
		return controller.ControllerStatusReport{}, false
	}
	report, err := store.ProjectStatus()
	return report, err == nil && report.CanonicalActive
}
