package report

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/abeval"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func PrintEvalAB(st *state.StateStore, dir string, stdout io.Writer) error {
	spec, direct, orchestrated, err := abeval.LoadPair(dir)
	if err != nil {
		return err
	}
	if orchestrated.GLMUsage.Source == abeval.GLMUsageSourceTaskStats {
		all, err := st.AllTaskStats()
		if err != nil {
			return err
		}
		orchestrated, err = abeval.ResolveFromTaskStats(orchestrated, all)
		if err != nil {
			return err
		}
	}
	if err := abeval.ValidatePair(spec, direct, orchestrated); err != nil {
		return err
	}
	return machinecli.WriteJSON(stdout, abeval.BuildReport(abeval.Compare(spec, direct, orchestrated)))
}
