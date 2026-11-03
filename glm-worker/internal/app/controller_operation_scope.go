package app

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
)

func openControllerOperationStore(cfg config.AppConfig) (*controller.Store, error) {
	decision, err := repositoryharness.Evaluate(cfg.RepoRoot)
	if err != nil {
		return nil, err
	}
	if decision.Active {
		return openControllerSemanticStore(cfg)
	}
	if err := controller.RequireRequestRepository(cfg.RepoRoot); err != nil {
		return nil, err
	}
	exists, err := controller.Exists(cfg)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("repository controller operation requires active repository harness or existing request authority")
	}
	store, err := controller.Open(cfg)
	if err != nil {
		return nil, err
	}
	head, err := store.LoadHead()
	if err != nil {
		return nil, err
	}
	if head.ProjectSnapshotID == "" {
		return nil, fmt.Errorf("repository controller operation has no request authority")
	}
	project, err := store.LoadProjectSnapshot(head.ProjectSnapshotID)
	if err != nil {
		return nil, err
	}
	if project.UserRequest == nil && project.CompletedRequest == nil {
		return nil, fmt.Errorf("repository controller operation requires active repository harness or existing request authority")
	}
	return store, nil
}
