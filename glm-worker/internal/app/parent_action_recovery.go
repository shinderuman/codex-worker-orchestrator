package app

import (
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type parentActionRecoveryOutput struct {
	Status     string `json:"status"`
	TaskID     string `json:"task_id"`
	TaskStatus string `json:"task_status"`
}

const modeRecoverParentAction CommandMode = 101

const modelCallOutcomeError = "error"

func init() {
	commandParsers["--recover-parent-action"] = func(args []string) (Command, error) {
		return singleArgCommand(args, modeRecoverParentAction, "usage: glm-worker --recover-parent-action")
	}
}

func recoverInterruptedParentAction(st *state.StateStore, stdout io.Writer) error {
	taskID, err := st.TaskID()
	if err != nil {
		return err
	}
	target, err := parentActionRecoveryTarget(st, taskID)
	if err != nil {
		return err
	}
	if err := st.RecoverParentActionBegin(target); err != nil {
		return err
	}
	return writeJSON(stdout, parentActionRecoveryOutput{
		Status:     "recovered",
		TaskID:     taskID,
		TaskStatus: string(target),
	})
}

func parentActionRecoveryTarget(st *state.StateStore, taskID string) (state.TaskStatus, error) {
	logs, err := readStatusTelemetry(st, taskID)
	if err != nil {
		return state.TaskStatusNone, err
	}
	material := lastParentActionMaterial(logs)
	if material == nil {
		return state.TaskStatusNone, fmt.Errorf("parent action recovery requires the failed call material")
	}
	if material.Outcome != modelCallOutcomeError || !runner.IsPreCallGuardFailureText(material.Error) {
		return state.TaskStatusNone, fmt.Errorf("parent action recovery requires the last material to be a pre-call guard error, got outcome %s", material.Outcome)
	}
	switch material.Phase {
	case state.WorkerPhaseCategoryDecision:
		return state.TaskStatusWaitingDecision, nil
	case state.WorkerPhaseCategoryExplicitFix:
		return state.TaskStatusWaitingSolReview, nil
	default:
		return state.TaskStatusNone, fmt.Errorf("parent action recovery does not cover phase %s", material.Phase)
	}
}

func lastParentActionMaterial(logs []state.ModelCallLog) *state.ModelCallLog {
	for index := len(logs) - 1; index >= 0; index-- {
		if logs[index].CallType == state.CallTypeProbe {
			continue
		}
		return &logs[index]
	}
	return nil
}
