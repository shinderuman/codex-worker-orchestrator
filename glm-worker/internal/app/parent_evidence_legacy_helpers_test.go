package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func finishParentRead(st *state.StateStore, surface, digest string, render func() (int, error)) error {
	scope, err := captureParentEvidenceReadScope(st)
	if err != nil {
		return err
	}
	return finishParentReadInScope(st, scope, surface, digest, render)
}

func printParentHandoffLeased(st *state.StateStore, stdout io.Writer) error {
	return printParentHandoffLeasedWithConfig(config.AppConfig{}, st, stdout)
}

func printParentHandoffRecoveryLeased(st *state.StateStore, stdout io.Writer) error {
	return printParentHandoffRecoveryLeasedWithConfig(config.AppConfig{}, st, stdout)
}
