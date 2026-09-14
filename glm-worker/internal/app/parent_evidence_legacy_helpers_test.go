package app

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"

func finishParentRead(st *state.StateStore, surface, digest string, render func() (int, error)) error {
	scope, err := captureParentEvidenceReadScope(st)
	if err != nil {
		return err
	}
	return finishParentReadInScope(st, scope, surface, digest, render)
}
