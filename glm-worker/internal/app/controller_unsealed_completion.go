package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

func executeUnsealedCompletionReentry(cfg config.AppConfig, store *controller.Store, command controllerPublicationCommand) (any, error) {
	bound, err := controller.ParentContinuationConfig(cfg)
	if err != nil {
		return nil, err
	}
	bound, err = controller.WorkflowConfig(bound)
	if err != nil {
		return nil, err
	}
	wf := workflow.NewWorkflow(bound, state.AttachStateStore(bound), nil, io.Discard)
	ref, err := wf.ReopenUnsealedParentCompletion(command.ExpectedGeneration, command.Message)
	if err != nil {
		return nil, err
	}
	head, err := store.LoadHead()
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "reopened-for-fix", "head": head, "source_evidence": ref}, nil
}
