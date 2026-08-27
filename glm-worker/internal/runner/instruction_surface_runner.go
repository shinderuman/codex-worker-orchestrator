package runner

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"

type InstructionSurfaceGuardRunner struct {
	base *ClaudeRunner
}

func NewInstructionSurfaceGuardRunner(base *ClaudeRunner) *InstructionSurfaceGuardRunner {
	return &InstructionSurfaceGuardRunner{base: base}
}

func (r *InstructionSurfaceGuardRunner) Run(
	role state.SessionRole,
	phase string,
	model string,
	readOnly bool,
	effort string,
	prompt string,
	outputPath string,
) (RunResult, error) {
	before, err := r.base.prepareInstructionSurfaceGuard()
	if err != nil {
		r.invalidateSessions()
		return RunResult{}, err
	}
	result, runErr := r.base.Run(role, phase, model, readOnly, effort, prompt, outputPath)
	if guardErr := r.base.verifyInstructionSurfaceGuard(before); guardErr != nil {
		r.invalidateSessions()
		return result, guardErr
	}
	return result, runErr
}

func (r *InstructionSurfaceGuardRunner) Probe(model string) (ProbeResult, error) {
	return r.base.Probe(model)
}

func (r *InstructionSurfaceGuardRunner) invalidateSessions() {
	_ = r.base.state.Remove("worker.id", "worker.ready", "reviewer.id", "reviewer.ready")
}
