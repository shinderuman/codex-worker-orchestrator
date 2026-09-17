package app

import (
	"errors"
	"io"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type dispositionResetOutput struct {
	Status      string                `json:"status"`
	RepoRoot    *string               `json:"repo_root"`
	Disposition state.TaskDisposition `json:"disposition,omitempty"`
}

const resetDispositionUsage = "usage: glm-worker --reset [--disposition cancel|abandon|recovery]"

func init() {
	commandParsers["--reset"] = resetDispositionCommand
}

func resetDispositionCommand(args []string) (Command, error) {
	if len(args) == 1 {
		return Command{Mode: ModeReset}, nil
	}
	if len(args) != 3 || args[1] != "--disposition" {
		return Command{}, machinecli.UsageErrorf("%s", resetDispositionUsage)
	}
	disposition := state.TaskDisposition(args[2])
	if !disposition.Valid() {
		return Command{}, machinecli.UsageErrorf("%s", resetDispositionUsage)
	}
	return Command{Mode: ModeReset, Payload: string(disposition)}, nil
}

func executeDispositionReset(cmd Command, st *state.StateStore, stdout io.Writer) error {
	if cmd.Payload == "" && safeLegacyNoTaskReset(st) {
		return resetState(st, stdout)
	}
	disposition, err := st.ResetWithDisposition(cmd.Payload)
	if err != nil {
		return err
	}
	return machinecli.WriteJSON(stdout, dispositionResetOutput{
		Status:      "reset",
		RepoRoot:    machinecli.StringPtr(st.ReadOr("repo-root", "")),
		Disposition: disposition,
	})
}

func safeLegacyNoTaskReset(st *state.StateStore) bool {
	if st.ReadOr("task.id", "") != "" || st.TaskStatus() != state.TaskStatusNone {
		return false
	}
	_, err := st.CurrentTaskDisposition()
	return errors.Is(err, os.ErrNotExist)
}
