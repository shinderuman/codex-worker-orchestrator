package app

import (
	"encoding/json"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const canonicalStartAction = "start"

func applyCanonicalControllerHandoff(cfg config.AppConfig, output *parentHandoffOutput) {
	report, ok := canonicalHandoffStatus(cfg)
	if !ok {
		return
	}
	output.Controller = &report
	if applyPendingCompletionRecoveryHandoff(cfg, output, report) {
		return
	}
	if !output.Consistent {
		if applyExternalAdvancementSyncHandoff(cfg, output, report) {
			return
		}
		applyExecutionTerminationHandoff(cfg, output)
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
	if !applyTaskAuthoritySyncHandoff(cfg, output, report) &&
		(report.Head.PendingTransitionID != "" || report.Head.LiveLeaseID == "") {
		applyCanonicalQuiescentHandoff(cfg, output, report.Head)
	}
	if output.ParentRequest != nil && output.RequiredAction != nil {
		output.ParentRequest.Continuation.RequiredAction = *output.RequiredAction
	}
}

func applyPendingCompletionRecoveryHandoff(cfg config.AppConfig, output *parentHandoffOutput, report controller.ControllerStatusReport) bool {
	if report.Head.PendingTransitionID == "" {
		return false
	}
	store, err := controller.Open(cfg)
	if err != nil {
		return false
	}
	pending, err := store.PendingCompletionRecovery(report.Head)
	if !pending {
		return false
	}
	*output = parentHandoffOutput{Version: parentHandoffVersion, Controller: &report, Consistent: false, AllowedActions: []string{}, Validations: []parentHandoffValidation{}}
	if err != nil {
		message := err.Error()
		output.Inconsistency = &message
		return true
	}
	command, err := json.Marshal(controllerExecutionCommand{Action: "recover", ExpectedGeneration: report.Head.ControllerGeneration, TransitionID: report.Head.PendingTransitionID})
	if err != nil {
		return true
	}
	applyControllerExecutionCompletionHandoff(output, string(command))
	return true
}

func applyControllerExecutionCompletionHandoff(output *parentHandoffOutput, command string) {
	action := controllerExecutionSurface
	output.Consistent = true
	output.Inconsistency = nil
	output.ParentRequest = nil
	output.RequiredAction = &action
	output.AllowedActions = []string{action}
	output.RequiredActionParameters = map[string]string{"controller_command_json": command}
}

func applyExternalAdvancementSyncHandoff(cfg config.AppConfig, output *parentHandoffOutput, report controller.ControllerStatusReport) bool {
	attribution := observedTaskAttribution(output)
	if attribution == nil {
		return false
	}
	authority, explained := externalAdvancementAuthority(cfg, report.Head)
	if !explained || !externalAdvancementExplainsAttribution(*attribution, authority) {
		return false
	}
	restored := *output
	applyCanonicalQuiescentHandoff(cfg, output, report.Head)
	if output.RequiredAction == nil || *output.RequiredAction != controllerPublicationSurface {
		*output = restored
		return false
	}
	output.Consistent = true
	output.Inconsistency = nil
	output.ParentRequest.TaskAttribution.LegalNextAction = controllerPublicationSurface
	output.ParentRequest.Continuation.RequiredAction = controllerPublicationSurface
	return true
}

func observedTaskAttribution(output *parentHandoffOutput) *repositoryproject.TaskAttribution {
	if output.ParentRequest == nil {
		return nil
	}
	return &output.ParentRequest.TaskAttribution
}

func externalAdvancementExplainsAttribution(attribution repositoryproject.TaskAttribution, authority controller.CommittedTaskAuthority) bool {
	if attribution.Matches || attribution.Reason != repositoryproject.ReasonActiveTaskMismatch || attribution.Handover {
		return false
	}
	return attribution.ActiveTask == authority.Task.TaskPath && attribution.ActiveTask != attribution.LifecycleTask && authority.Task.TaskPath != ""
}

func externalAdvancementAuthority(cfg config.AppConfig, head controller.RepositoryControllerHead) (controller.CommittedTaskAuthority, bool) {
	if head.MetadataLineageRef == nil || head.PublicationPolicy == nil {
		return controller.CommittedTaskAuthority{}, false
	}
	authority, err := controller.ResolveCommittedTaskAuthority(cfg.RepoRoot)
	if err != nil || authority.Snapshot.HeadOID == head.IntegrationTip {
		return controller.CommittedTaskAuthority{}, false
	}
	store, err := controller.Open(cfg)
	if err != nil {
		return controller.CommittedTaskAuthority{}, false
	}
	if _, err := store.RetiredSourceAdvancementRequired(head, authority.Snapshot.HeadOID); err != nil {
		return controller.CommittedTaskAuthority{}, false
	}
	return authority, true
}

func applyTaskAuthoritySyncHandoff(cfg config.AppConfig, output *parentHandoffOutput, report controller.ControllerStatusReport) bool {
	store, err := controller.Open(cfg)
	if err != nil {
		return false
	}
	required, err := store.RequiredTaskAuthoritySync(report.Head)
	if err != nil {
		output.Consistent = false
		message := err.Error()
		output.Inconsistency = &message
		output.RequiredAction = nil
		output.AllowedActions = []string{"controller-evidence"}
		output.RequiredActionParameters = nil
		return true
	}
	if required == nil {
		return false
	}
	if !required.Admittable {
		if applyReentrySourceHandoff(cfg, output, report.Head, required.Plan.RevisedTaskRef, store) {
			return true
		}
		output.Consistent = false
		message := "active Task contract is stale but the regular sync owner cannot admit it: " + required.Reason
		output.Inconsistency = &message
		output.RequiredAction = nil
		output.AllowedActions = []string{"controller-evidence"}
		output.RequiredActionParameters = nil
		return true
	}
	plan := required.Plan
	command, err := json.Marshal(controllerExecutionCommand{
		Action:             "sync-task-authority",
		ExpectedGeneration: report.Head.ControllerGeneration,
		SourceSnapshotID:   plan.SourceSnapshotID,
		RevisedTask:        &plan.RevisedTaskRef,
	})
	if err != nil {
		return false
	}
	action := controllerExecutionSurface
	output.RequiredAction = &action
	output.AllowedActions = []string{action, "controller-evidence"}
	output.RequiredActionParameters = map[string]string{
		"controller_command_json":       string(command),
		"previous_task_contract_sha256": plan.PreviousTaskRef.ContractDigest,
		"revised_task_contract_sha256":  plan.RevisedTaskRef.ContractDigest,
	}
	return true
}

func applyExecutionTerminationHandoff(cfg config.AppConfig, output *parentHandoffOutput) {
	store, err := controller.Open(cfg)
	if err != nil {
		return
	}
	input, err := store.PreviewExecutionTermination()
	if err != nil {
		return
	}
	data, err := json.Marshal(controllerExecutionCommand{Action: "user-terminate-execution", ExpectedGeneration: input.ExpectedGeneration, ExecutionTermination: &input})
	if err != nil {
		return
	}
	action := controllerExecutionSurface
	output.Consistent = true
	output.Inconsistency = nil
	output.ParentRequest = nil
	output.RequiredAction = &action
	output.AllowedActions = []string{action}
	output.RequiredActionParameters = map[string]string{"controller_command_json": string(data), "semantic_slot": "execution_termination.reason"}
}

func applyCanonicalQuiescentHandoff(cfg config.AppConfig, output *parentHandoffOutput, head controller.RepositoryControllerHead) {
	action := canonicalQuiescentAction(cfg, head, output.TaskID)
	if action == canonicalStartAction {
		authority, err := controller.ResolveCommittedTaskAuthority(cfg.RepoRoot)
		if err != nil {
			output.Consistent = false
			message := err.Error()
			output.Inconsistency = &message
			output.AllowedActions = nil
			output.RequiredAction = nil
			return
		}
		if authority.ProjectSnapshotID != head.ProjectSnapshotID || authority.Snapshot.HeadOID != head.IntegrationTip {
			action = controllerPublicationSurface
		}
	}
	output.RequiredAction = &action
	output.AllowedActions = []string{action, "controller-evidence"}
	applyRetiredSourceSyncHandoff(cfg, output, head)
	if head.PendingTransitionID == "" && head.AcceptedCandidateRef != nil {
		if action != controllerExecutionSurface {
			output.AllowedActions = append(output.AllowedActions, controllerExecutionSurface)
		}
		if action != controllerPublicationSurface {
			output.AllowedActions = append(output.AllowedActions, controllerPublicationSurface)
		}
	}
	if action == controllerPublicationSurface && output.ParentRequest != nil {
		output.ParentRequest.TaskAttribution.LegalNextAction = action
	}
}

func canonicalParentAction(action string) string {
	switch state.ParentAction(action) {
	case state.ParentActionComplete, state.ParentActionInstall, state.ParentActionReopen:
		return controllerPublicationSurface
	case state.ParentActionPark, state.ParentActionRepairReentryReview:
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
		return quiescentSuccessorAction(cfg, head, taskID)
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

func quiescentSuccessorAction(cfg config.AppConfig, head controller.RepositoryControllerHead, _ *string) string {
	if head.ObservedRemoteTip != "" && head.ObservedRemoteTip != head.IntegrationTip {
		return controllerPublicationSurface
	}
	if head.ActiveEpisodeID != "" || (head.MetadataLineageRef == nil && head.UserTerminationRef == nil) {
		return controllerExecutionSurface
	}
	store, err := controller.Open(cfg)
	if err != nil {
		return controllerExecutionSurface
	}
	if _, err := store.QuiescentRuntimeAdmission(head); err == nil {
		return canonicalStartAction
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
