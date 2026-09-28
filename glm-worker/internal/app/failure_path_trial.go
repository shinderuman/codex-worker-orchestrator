package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/failurepathtrial"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func executeFailurePathTrial(cmd Command, st *state.StateStore, stdout io.Writer) error {
	registryPath := st.Path(failurepathtrial.RegistryFile)
	registry, err := failurepathtrial.LoadRegistry(registryPath)
	if err != nil {
		return err
	}
	if cmd.ReferencePath != "" {
		labels, err := failurepathtrial.LoadLabels(cmd.ReferencePath)
		if err != nil {
			return err
		}
		registry, err = failurepathtrial.UpdateRegistry(registryPath, func(current failurepathtrial.Registry) (failurepathtrial.Registry, error) {
			return failurepathtrial.ApplyLabels(current, labels)
		})
		if err != nil {
			return err
		}
	}
	return machinecli.WriteJSON(stdout, failurepathtrial.BuildSummary(registry))
}
