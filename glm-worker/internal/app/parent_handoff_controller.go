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
		action := canonicalQuiescentAction(report.Head, output.TaskID)
		output.RequiredAction = &action
		output.AllowedActions = []string{action, "controller-evidence"}
	}
	if output.ParentRequest != nil && output.RequiredAction != nil {
		output.ParentRequest.Continuation.RequiredAction = *output.RequiredAction
	}
}

func canonicalParentAction(action string) string {
	switch state.ParentAction(action) {
	case state.ParentActionComplete, state.ParentActionInstall, state.ParentActionReopen:
		return "controller-publication"
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

func canonicalQuiescentAction(head controller.RepositoryControllerHead, taskID *string) string {
	if head.PendingTransitionID == "" && head.ExecutionTaskRef == nil && head.MetadataLineageRef != nil && taskID == nil {
		return "start"
	}
	return controllerExecutionSurface
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
