package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/failurepathadvisory"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func executeFailurePathAdvisory(cmd Command, st *state.StateStore, stdout io.Writer) error {
	registryPath := st.Path(failurepathadvisory.RegistryFile)
	registry, err := failurepathadvisory.LoadRegistry(registryPath)
	if err != nil {
		return err
	}
	if cmd.ReferencePath != "" {
		labels, err := failurepathadvisory.LoadLabels(cmd.ReferencePath)
		if err != nil {
			return err
		}
		registry, err = failurepathadvisory.UpdateRegistry(registryPath, func(current failurepathadvisory.Registry) (failurepathadvisory.Registry, error) {
			return failurepathadvisory.ApplyLabels(current, labels)
		})
		if err != nil {
			return err
		}
	}
	return machinecli.WriteJSON(stdout, failurepathadvisory.BuildSummary(registry))
}
