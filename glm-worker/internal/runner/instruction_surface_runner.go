package runner

import (
	"errors"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

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
	instructionBefore, err := r.base.prepareInstructionSurfaceGuard()
	if err != nil {
		r.invalidateSessions()
		return RunResult{}, err
	}
	gitGuard, err := prepareGitAuthorityGuard(r.base.config.RepoRoot)
	if err != nil {
		r.invalidateSessions()
		return RunResult{}, err
	}
	defer gitGuard.cleanup()

	callBase := r.base
	if gitGuard.before.active {
		wrappedClaude, wrapErr := gitGuard.prepareClaudeWrapper(r.base.config.ClaudeBin)
		if wrapErr != nil {
			r.invalidateSessions()
			return RunResult{}, wrapErr
		}
		copyBase := *r.base
		copyBase.config.ClaudeBin = wrappedClaude
		copyBase.bashSandbox = gitGuard.bashSandboxPolicy()
		callBase = &copyBase
	}
	callBase.instructionSurfaceDigest = instructionBefore.digest

	result, runErr := callBase.Run(role, phase, model, readOnly, effort, prompt, outputPath)
	gitErr := gitGuard.verify()
	instructionErr := r.base.verifyInstructionSurfaceGuard(instructionBefore)
	if gitErr != nil || instructionErr != nil {
		r.invalidateSessions()
		return result, errors.Join(gitErr, instructionErr)
	}
	return result, runErr
}

func (r *InstructionSurfaceGuardRunner) Probe(model string) (ProbeResult, error) {
	return r.base.Probe(model)
}

func (r *InstructionSurfaceGuardRunner) invalidateSessions() {
	_ = r.base.state.InvalidateAllSessions()
}
