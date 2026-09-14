package runner

import (
	"fmt"
	"os"
	"strconv"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (r *ClaudeRunner) runWithAdmission(
	role state.SessionRole,
	phase string,
	model string,
	readOnly bool,
	effort string,
	prompt string,
	outputPath string,
	admit func() error,
) (RunResult, error) {
	result, taskID, sessionID, ready, err := r.prepareRunSession(role, phase, model)
	if err != nil {
		return result, err
	}
	inputs, err := r.prepareRunInputs(role, phase, model, &result)
	if err != nil {
		return result, err
	}
	args := r.buildRunArgs(role, taskID, sessionID, ready, model, readOnly, effort, prompt, inputs)
	callID, callIDErr := state.NewUUID()
	if callIDErr != nil {
		state.WarnTaskEventSkip("call ID生成", callIDErr)
	} else {
		result.CallID = callID
	}
	versionScope := r.beginClaudeVersionScope(sessionID)
	ingester, stderrPath, runErr, err := r.executeRunCommandWithAdmission(
		role, phase, model, taskID, sessionID, callID, ready, args, inputs, outputPath, admit,
	)
	if err != nil {
		return result, err
	}
	return r.finishRun(role, outputPath, stderrPath, ingester, result, runErr, versionScope)
}

func (r *ClaudeRunner) executeRunCommandWithAdmission(
	role state.SessionRole,
	phase, model, taskID, sessionID, callID string,
	ready bool,
	args []string,
	inputs runInputs,
	outputPath string,
	admit func() error,
) (*streamEventIngester, string, error, error) {
	stderrPath := outputPath + ".stderr"
	stderr, err := createPrivateFile(stderrPath)
	if err != nil {
		return nil, stderrPath, nil, err
	}
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		_ = stderr.Close()
		return nil, stderrPath, nil, fmt.Errorf("/dev/nullを開けません: %w", err)
	}
	defer func() { _ = devNull.Close() }()

	ingester := r.newTaskEventIngester(taskID, callID, role, phase, model, sessionID, ready)
	command := newProcessGroupCmd(r.config.ClaudeBin, args...)
	command.Dir = r.config.RepoRoot
	command.Stdin = devNull
	command.Stdout = ingester
	command.Stderr = stderr
	additions := claudeInvocationEnvDefaults()
	additions["CLAUDE_CONFIG_DIR"] = r.config.ClaudeConfigDir
	if inputs.contextWindow.declaredMaxContextTokens > 0 {
		additions["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] = strconv.Itoa(inputs.contextWindow.declaredMaxContextTokens)
	}
	command.Env = buildChildEnv(r.config.EnvAllowlist, inputs.settingEnv, additions, inputs.envDeletes)

	if admit != nil {
		if err := admit(); err != nil {
			_ = stderr.Close()
			return nil, stderrPath, nil, err
		}
	}
	runErr := r.runCommand(command)
	ingester.flush()
	if closeErr := stderr.Close(); runErr == nil && closeErr != nil {
		runErr = closeErr
	}
	return ingester, stderrPath, runErr, nil
}
