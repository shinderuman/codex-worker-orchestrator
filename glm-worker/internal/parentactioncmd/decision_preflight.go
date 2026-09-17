package parentactioncmd

import (
	"bytes"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

func executePreflightedDecision(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: glm-parent-action decision <token>")
	}
	descriptor, ok := parentaction.LookupPayloadAction(args[0])
	if !ok || descriptor.Action != parentaction.ActionDecision {
		return fmt.Errorf("unsupported parent payload action %q", args[0])
	}
	if err := persistParentCodexIdentity(cfg); err != nil {
		return err
	}
	return withParentWaitLease(cfg, func() error {
		worker, err := resolveGLMWorker()
		if err != nil {
			return err
		}
		payload, err := parentaction.Peek(cfg.RepoRoot, string(descriptor.Action), args[1])
		if err != nil {
			return err
		}
		if err := workflow.ValidateDecisionExecutionUnitPayload(cfg, state.AttachStateStore(cfg), string(payload)); err != nil {
			return err
		}
		payload, err = parentaction.ConsumeExpected(cfg.RepoRoot, string(descriptor.Action), args[1], payload)
		if err != nil {
			return err
		}
		workerArgs := payloadWorkerArgsForDescriptor(descriptor, payload, nil)
		return runResolvedWorker(worker, cfg.RepoRoot, workerArgs, bytes.NewReader(payload), stdout, stderr, nil)
	})
}
