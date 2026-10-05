package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type acceptOutput struct {
	Accepted bool `json:"accepted"`
}

func parentAccept(st *state.StateStore, stdout io.Writer) error {
	resolved, err := st.AcceptParentReview()
	if err != nil {
		return err
	}
	return machinecli.WriteJSON(stdout, acceptOutput{Accepted: resolved})
}
