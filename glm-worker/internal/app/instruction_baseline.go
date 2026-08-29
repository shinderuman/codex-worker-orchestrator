package app

import (
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type instructionBaselineRotationOutput struct {
	Rotated        bool   `json:"rotated"`
	PreviousDigest string `json:"previous_digest"`
	CurrentDigest  string `json:"current_digest"`
}

const modeRotateInstructionBaseline CommandMode = 100

func init() {
	commandParsers["--rotate-instruction-baseline"] = func(args []string) (Command, error) {
		return singleArgCommand(args, modeRotateInstructionBaseline, "usage: glm-worker --rotate-instruction-baseline")
	}
}

func rotateInstructionBaseline(cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	if st.TaskStatus() != state.TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		return fmt.Errorf("instruction baseline rotation is only available while the active task is waiting for a Sol decision")
	}
	rotation, err := runner.RotateInstructionSurfaceBaseline(cfg, st)
	if err != nil {
		return err
	}
	return writeJSON(stdout, instructionBaselineRotationOutput{
		Rotated:        true,
		PreviousDigest: rotation.PreviousDigest,
		CurrentDigest:  rotation.CurrentDigest,
	})
}
