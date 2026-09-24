package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

type DecisionCallResult struct {
	StructuredOutput json.RawMessage
	Response         string
	IsError          bool
	DurationMS       int64
	DurationAPIMS    int64
	TotalCostUSD     float64
	Usage            TokenUsage
	SettingEnvKeys   []string
}

type DecisionCallError struct {
	Model  string
	Reason string
}

const defaultDecisionTimeout = 120 * time.Second

func (e *DecisionCallError) Error() string {
	return fmt.Sprintf("decision呼出に失敗しました(%s): %s", e.Model, e.Reason)
}

func (r *ClaudeRunner) Decide(model, effort, schema, prompt string) (DecisionCallResult, error) {
	if model == "" {
		return DecisionCallResult{}, fmt.Errorf("decision modelを指定してください")
	}
	if schema == "" {
		return DecisionCallResult{}, fmt.Errorf("decision schemaを指定してください")
	}
	if prompt == "" {
		return DecisionCallResult{}, fmt.Errorf("decision promptを指定してください")
	}

	isolationArgs, err := isolationSettings(r.config.ClaudeConfigDir, nil)
	if err != nil {
		return DecisionCallResult{}, err
	}
	settingEnv, envDeletes, err := loadConfiguredSettingEnv(r.config)
	if err != nil {
		return DecisionCallResult{}, err
	}

	decisionDir, err := os.MkdirTemp("", "glm-worker-decision-*")
	if err != nil {
		return DecisionCallResult{}, fmt.Errorf("decision dirを作成できません: %w", err)
	}
	defer func() { _ = os.RemoveAll(decisionDir) }()

	output, stderr, devNull, rawOutputPath, _, err := openProbeFiles(decisionDir)
	if err != nil {
		return DecisionCallResult{}, err
	}
	defer func() { _ = devNull.Close() }()

	command := newProcessGroupCmd(r.config.ClaudeBin, decisionArgs(model, effort, schema, isolationArgs, prompt)...)
	command.Dir = decisionDir
	command.Stdin = devNull
	command.Stdout = output
	command.Stderr = stderr
	additions := claudeInvocationEnvDefaults()
	additions["CLAUDE_CONFIG_DIR"] = r.config.ClaudeConfigDir
	command.Env = buildChildEnv(r.config.EnvAllowlist, settingEnv, additions, envDeletes)

	runErr := closeProbeOutputs(r.runProbeCommand(command, time.Now().Add(r.decisionTimeout)), output, stderr)
	return finishDecision(model, rawOutputPath, settingEnv, runErr)
}

func finishDecision(model, rawOutputPath string, settingEnv map[string]string, runErr error) (DecisionCallResult, error) {
	parsed, parseErr := parseClaudeJSONResult(rawOutputPath)
	result := DecisionCallResult{
		StructuredOutput: parsed.StructuredOut,
		Response:         parsed.Result,
		IsError:          parsed.IsError,
		DurationMS:       parsed.DurationMS,
		DurationAPIMS:    parsed.DurationAPIMS,
		TotalCostUSD:     parsed.TotalCostUSD,
		Usage:            parsed.Usage,
		SettingEnvKeys:   sortedEnvKeys(settingEnv),
	}
	if runErr != nil {
		return result, &DecisionCallError{Model: model, Reason: decisionRunFailureReason(runErr)}
	}
	if parseErr != nil {
		return result, &DecisionCallError{Model: model, Reason: "result-parse-failure"}
	}
	if result.IsError {
		return result, &DecisionCallError{Model: model, Reason: "provider-is-error"}
	}
	if !structuredOutputPresent(result.StructuredOutput) {
		return result, &StructuredOutputError{}
	}
	return result, nil
}

func decisionRunFailureReason(runErr error) string {
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return "exit-status"
	}
	var interrupted *InterruptedCallError
	if errors.As(runErr, &interrupted) {
		return "interrupted"
	}
	if errors.Is(runErr, ErrProbeDeadlineExceeded) {
		return "deadline-exceeded"
	}
	return "command-failure"
}

func decisionArgs(model, effort, schema, isolationArgs, prompt string) []string {
	return []string{
		"-p", "--safe-mode", "--setting-sources", "",
		"--no-session-persistence",
		"--model", model,
		"--effort", effort,
		"--output-format", "json",
		"--dangerously-skip-permissions",
		"--strict-mcp-config",
		"--mcp-config", `{"mcpServers":{}}`,
		"--disable-slash-commands",
		"--tools", "",
		"--settings", isolationArgs,
		"--json-schema", schema,
		prompt,
	}
}
