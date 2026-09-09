package app

import (
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type qualitySurfaceRecoveryOutput struct {
	Status     string `json:"status"`
	TaskID     string `json:"task_id"`
	TaskStatus string `json:"task_status"`
	Repair     string `json:"repair"`
}

const modeRecoverQualitySurface CommandMode = 102

const (
	qualitySurfaceRepairDecisionWait   = "quality-surface-decision-wait"
	qualitySurfaceRepairApprovedReview = "approved-quality-surface-review"
)

func init() {
	commandParsers["--recover-quality-surface"] = func(args []string) (Command, error) {
		return requiredPayloadCommand(args, modeRecoverQualitySurface, "usage: glm-worker --recover-quality-surface <task-id>")
	}
}

func recoverQualitySurfaceLifecycle(st *state.StateStore, expectedTaskID string, stdout io.Writer) error {
	taskID, err := st.TaskID()
	if err != nil {
		return err
	}
	status := st.TaskStatus()
	var repair string
	switch {
	case status == state.TaskStatusWaitingSolReview:
		if err := st.RecoverQualitySurfaceDecisionWait(expectedTaskID); err != nil {
			return err
		}
		repair = qualitySurfaceRepairDecisionWait
	case state.StoppedTaskStatus(status):
		if err := st.RecoverApprovedQualitySurfaceReview(expectedTaskID); err != nil {
			return err
		}
		repair = qualitySurfaceRepairApprovedReview
	default:
		return fmt.Errorf("quality-surface recovery does not cover task status %s", status)
	}
	return writeJSON(stdout, qualitySurfaceRecoveryOutput{
		Status:     "recovered",
		TaskID:     taskID,
		TaskStatus: string(status),
		Repair:     repair,
	})
}
