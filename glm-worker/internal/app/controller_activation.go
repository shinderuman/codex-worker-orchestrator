package app

import (
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
)

type controllerActivationOutput struct {
	ControllerGeneration uint64                     `json:"controller_generation"`
	ProjectSnapshotID    string                     `json:"project_snapshot_id"`
	Task                 controller.SemanticTaskRef `json:"task"`
	AttemptID            string                     `json:"attempt_id"`
	LeaseID              string                     `json:"lease_id"`
	WorkspaceID          string                     `json:"workspace_id"`
	WorkspaceSnapshotID  string                     `json:"workspace_snapshot_id"`
}

func runControllerActivation(
	args []string,
	loadConfig func() (config.AppConfig, error),
	stdout io.Writer,
) (bool, error) {
	if len(args) != 2 || args[0] != controllerAuthorityFlag || args[1] != "controller-activate" {
		return false, nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return true, err
	}
	decision, err := repositoryharness.Evaluate(cfg.RepoRoot)
	if err != nil {
		return true, fmt.Errorf("evaluate repository controller activation: %w", err)
	}
	if !decision.Active {
		return true, fmt.Errorf("repository controller activation requires active repository harness")
	}
	lock, err := acquireWorkflowLock(cfg)
	if err != nil {
		return true, err
	}
	defer func() { _ = lock.Close() }()
	admission, err := controller.Activate(cfg)
	if err != nil {
		return true, err
	}
	return true, writeValidatedMachineJSON(stdout, controllerActivationOutput{
		ControllerGeneration: admission.Head.ControllerGeneration,
		ProjectSnapshotID:    admission.Head.ProjectSnapshotID,
		Task:                 admission.Lease.SemanticTaskRef,
		AttemptID:            admission.Attempt.AttemptID,
		LeaseID:              admission.Lease.LeaseID,
		WorkspaceID:          admission.Workspace.ID,
		WorkspaceSnapshotID:  admission.Snapshot.ID,
	})
}
