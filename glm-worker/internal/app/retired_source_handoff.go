package app

import (
	"encoding/json"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

func applyRetiredSourceSyncHandoff(cfg config.AppConfig, output *parentHandoffOutput, head controller.RepositoryControllerHead) {
	if output.RequiredAction == nil || *output.RequiredAction != controllerPublicationSurface || head.MetadataLineageRef == nil || head.PublicationPolicy == nil {
		return
	}
	authority, err := controller.ResolveCommittedTaskAuthority(cfg.RepoRoot)
	if err != nil || authority.Snapshot.HeadOID == head.IntegrationTip {
		return
	}
	action := "adopt"
	store, err := controller.Open(cfg)
	if err != nil {
		return
	}
	requiresSource, err := store.RetiredSourceAdvancementRequired(head, authority.Snapshot.HeadOID)
	if err != nil {
		return
	}
	if requiresSource {
		action = "adopt-source"
	}
	command, err := json.Marshal(controllerPublicationCommand{Action: action, ExpectedGeneration: head.ControllerGeneration, Policy: *head.PublicationPolicy, SourceOID: authority.Snapshot.HeadOID})
	if err == nil {
		output.RequiredActionParameters = map[string]string{"controller_command_json": string(command), "active_task_path": authority.Task.TaskPath, "source_oid": authority.Snapshot.HeadOID, "integration_tip": head.IntegrationTip, "sync_reason": "committed primary source or project authority advanced beyond the retired controller"}
		if head.RootTaskRef != nil {
			output.RequiredActionParameters["previous_task_path"] = head.RootTaskRef.TaskPath
		}
	}
}
