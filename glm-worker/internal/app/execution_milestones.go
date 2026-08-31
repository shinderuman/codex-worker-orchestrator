package app

import (
	"encoding/json"
	"io"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

const executionMilestoneTaskUsage = "usage: glm-worker --execution-milestones-stdin <payload-bytes> [--sha256 <hex>]"
const executionMilestoneRevisionUsage = "usage: glm-worker --execution-milestones-revise-stdin <payload-bytes> [--sha256 <hex>]"

func executionMilestoneTaskCommand(args []string) (Command, error) {
	command, err := stdinPayloadCommand(ModeNewTask, args, executionMilestoneTaskUsage, false)
	if err != nil {
		return Command{}, err
	}
	command.ExecutionMilestones = true
	return command, nil
}

func executionMilestoneRevisionCommand(args []string) (Command, error) {
	return stdinPayloadCommand(ModeExecutionMilestonesRevise, args, executionMilestoneRevisionUsage, false)
}

func executeExecutionMilestoneRevision(
	cmd Command,
	cfg config.AppConfig,
	st *state.StateStore,
	stdout io.Writer,
) error {
	definitions, err := workflow.ParseExecutionMilestonePayload(cmd.Payload)
	if err != nil {
		return err
	}
	result, err := workflow.ReviseExecutionMilestones(cfg, st, definitions, time.Now().UTC())
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

func executeNewTaskCommand(wf *workflow.Workflow, cmd Command) error {
	if !cmd.ExecutionMilestones {
		return wf.ExecuteNewTask(cmd.Payload)
	}
	request, definitions, err := workflow.ParseExecutionTaskPlanPayload(cmd.Payload)
	if err != nil {
		return err
	}
	return wf.ExecuteNewTaskWithMilestones(request, definitions)
}
