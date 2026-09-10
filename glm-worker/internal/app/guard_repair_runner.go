package app

import (
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/guardrepair"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

const guardRepairFailureLimit = 4096

type guardRepairRequestRunner struct {
	base     workflow.ModelRunner
	state    *state.StateStore
	repoRoot string
}

func newGuardRepairRequestRunner(base workflow.ModelRunner, st *state.StateStore, repoRoot string) workflow.ModelRunner {
	return &guardRepairRequestRunner{base: base, state: st, repoRoot: repoRoot}
}

func (r *guardRepairRequestRunner) Run(
	role state.SessionRole,
	phase string,
	model string,
	readOnly bool,
	effort string,
	prompt string,
	outputPath string,
) (runner.RunResult, error) {
	result, err := r.base.Run(role, phase, model, readOnly, effort, prompt, outputPath)
	if err != nil && os.Getenv(state.GuardRepairParentActionEnv) == state.GuardRepairParentActionResume && runner.IsPreCallGuardFailure(err) {
		r.recordRequest(phase, err)
	}
	return result, err
}

func (r *guardRepairRequestRunner) Probe(model string) (runner.ProbeResult, error) {
	return r.base.Probe(model)
}

func (r *guardRepairRequestRunner) recordRequest(phase string, failure error) {
	taskID, err := r.state.TaskID()
	if err != nil {
		return
	}
	text := failure.Error()
	if len(text) > guardRepairFailureLimit {
		text = text[:guardRepairFailureLimit]
	}
	record, err := guardrepair.NewRecord(r.repoRoot, taskID, phase, text)
	if err != nil {
		return
	}
	_ = r.state.RequestGuardRepair(record)
}
