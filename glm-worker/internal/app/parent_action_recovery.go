package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type parentActionRecoveryOutput struct {
	Status     string `json:"status"`
	TaskID     string `json:"task_id"`
	TaskStatus string `json:"task_status"`
}

func recoverInterruptedParentAction(st *state.StateStore, stdout io.Writer) error {
	taskID, err := st.TaskID()
	if err != nil {
		return err
	}
	target, err := st.RecoverParentActionBeginFromState()
	if err != nil {
		return err
	}
	return machinecli.WriteJSON(stdout, parentActionRecoveryOutput{
		Status:     "recovered",
		TaskID:     taskID,
		TaskStatus: string(target),
	})
}
