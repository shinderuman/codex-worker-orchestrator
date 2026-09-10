package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

const guardRepairFailureLimit = 4096

type guardRepairRequestRunner struct {
	base  workflow.ModelRunner
	state *state.StateStore
}

func newGuardRepairRequestRunner(base workflow.ModelRunner, st *state.StateStore) workflow.ModelRunner {
	return &guardRepairRequestRunner{base: base, state: st}
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
	sum := sha256.Sum256([]byte(taskID + "\x00" + phase + "\x00" + text))
	_ = r.state.RequestGuardRepair(state.GuardRepairRecord{
		TaskID:      taskID,
		Phase:       phase,
		Fingerprint: hex.EncodeToString(sum[:]),
		Strategy:    state.GuardRepairStrategySourcePatch,
		Status:      state.GuardRepairRequested,
		Failure:     text,
	})
}
