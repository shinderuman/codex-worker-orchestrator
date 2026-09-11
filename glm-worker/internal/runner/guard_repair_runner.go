package runner

import (
	"errors"
	"os/exec"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type GuardRepairRunner struct {
	base *ClaudeRunner
}

func NewGuardRepairRunner(base *ClaudeRunner) *GuardRepairRunner {
	return &GuardRepairRunner{base: base}
}

func (r *GuardRepairRunner) Run(
	role state.SessionRole,
	phase string,
	model string,
	readOnly bool,
	effort string,
	prompt string,
	outputPath string,
) (RunResult, error) {
	gitGuard, err := prepareGuardRepairGitEnforcement(r.base.config.RepoRoot)
	if err != nil {
		return RunResult{}, err
	}
	defer gitGuard.cleanup()

	wrappedClaude, err := gitGuard.prepareClaudeWrapper(r.base.config.ClaudeBin)
	if err != nil {
		return RunResult{}, err
	}
	copyBase := *r.base
	copyBase.config.ClaudeBin = wrappedClaude
	copyBase.bashSandbox = guardRepairSandboxPolicy(gitGuard, r.base.config.RepoRoot)
	result, runErr := copyBase.Run(role, phase, model, readOnly, effort, prompt, outputPath)
	artifactErr := validateSensitiveResultArtifacts(r.base, result)
	attempts, attemptErr := readGitAuthorityAttempts(gitGuard.attemptLog)
	if attemptErr != nil || len(attempts) != 0 {
		_ = r.base.state.InvalidateAllSessions()
		return result, errors.Join(runErr, artifactErr, &GitAuthorityGuardError{
			Stage:     "repair-boundary",
			Mutations: attempts,
			Cause:     attemptErr,
		})
	}
	return result, errors.Join(runErr, artifactErr)
}

func prepareGuardRepairGitEnforcement(repoRoot string) (*gitAuthorityGuard, error) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		return nil, &GitAuthorityGuardError{Stage: "repair-resolve-git", Cause: err}
	}
	realGit, err = filepath.Abs(realGit)
	if err != nil {
		return nil, &GitAuthorityGuardError{Stage: "repair-resolve-git", Cause: err}
	}
	metadataPaths, err := resolveGitAuthorityMetadataPaths(realGit, repoRoot)
	if err != nil {
		return nil, &GitAuthorityGuardError{Stage: "repair-resolve-metadata", Cause: err}
	}
	guard := &gitAuthorityGuard{
		repoRoot:      repoRoot,
		realGit:       realGit,
		before:        gitAuthoritySnapshot{active: true},
		metadataPaths: metadataPaths,
	}
	if err := guard.prepareProxy(); err != nil {
		guard.cleanup()
		return nil, err
	}
	return guard, nil
}

func guardRepairSandboxPolicy(guard *gitAuthorityGuard, repoRoot string) *gitBashSandboxPolicy {
	policy := guard.bashSandboxPolicy()
	policy.denyWrite = append(policy.denyWrite,
		filepath.Join(repoRoot, state.ParentRulesFile),
		filepath.Join(repoRoot, state.ParentPlanFile),
		filepath.Join(repoRoot, state.ParentTasksDir),
		filepath.Join(repoRoot, state.ParentHistoryFile),
	)
	return policy
}
