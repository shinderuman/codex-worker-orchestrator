package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ProbeResult struct {
	Response      string
	IsError       bool
	DurationMS    int64
	DurationAPIMS int64
	TotalCostUSD  float64
	Usage         TokenUsage
	ModelUsage    map[string]ModelUsage
}

const ProbeSentinel = "GLM_WORKER_PROBE_OK"

const ProbePrompt = "Reply with exactly GLM_WORKER_PROBE_OK and nothing else."

func (r *ClaudeRunner) Probe(model string) (ProbeResult, error) {
	if model == "" {
		return ProbeResult{}, fmt.Errorf("probe modelを指定してください")
	}

	isolationArgs, err := isolationSettings(r.config.ClaudeConfigDir)
	if err != nil {
		return ProbeResult{}, err
	}
	settingEnv, envDeletes, err := loadSettingEnv(r.config.ClaudeConfigDir, r.config.ClaudeSettingsOverride)
	if err != nil {
		return ProbeResult{}, err
	}

	args := []string{
		"-p", "--safe-mode", "--setting-sources", "",
		"--no-session-persistence",
		"--model", model,
		"--effort", "low",
		"--output-format", "json",
		"--dangerously-skip-permissions",
		"--strict-mcp-config",
		"--mcp-config", `{"mcpServers":{}}`,
		"--disable-slash-commands",
		"--tools", "",
		"--settings", isolationArgs, ProbePrompt,
	}

	probeDir, err := os.MkdirTemp("", "glm-worker-probe-*")
	if err != nil {
		return ProbeResult{}, fmt.Errorf("probe dirを作成できません: %w", err)
	}
	defer os.RemoveAll(probeDir)

	rawOutputPath := filepath.Join(probeDir, "probe.json")
	stderrPath := filepath.Join(probeDir, "probe.stderr")
	output, err := createPrivateFile(rawOutputPath)
	if err != nil {
		return ProbeResult{}, err
	}
	stderr, err := createPrivateFile(stderrPath)
	if err != nil {
		output.Close()
		return ProbeResult{}, err
	}
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		output.Close()
		stderr.Close()
		return ProbeResult{}, fmt.Errorf("/dev/nullを開けません: %w", err)
	}
	defer devNull.Close()

	command := exec.Command(r.config.ClaudeBin, args...)
	command.Dir = probeDir
	command.Stdin = devNull
	command.Stdout = output
	command.Stderr = stderr
	command.Env = buildChildEnv(r.config.EnvAllowlist, settingEnv, map[string]string{
		"CLAUDE_CONFIG_DIR":                r.config.ClaudeConfigDir,
		"CLAUDE_CODE_AUTO_COMPACT_WINDOW":  "500000",
		"CLAUDE_CODE_ALWAYS_ENABLE_EFFORT": "1",
	}, envDeletes)

	runErr := command.Run()
	outputCloseErr := output.Close()
	stderrCloseErr := stderr.Close()
	if runErr == nil {
		if outputCloseErr != nil {
			runErr = outputCloseErr
		} else if stderrCloseErr != nil {
			runErr = stderrCloseErr
		}
	}

	result := ProbeResult{}
	parsed, parseErr := parseClaudeJSONResult(rawOutputPath)
	if parseErr == nil {
		result.Response = parsed.Result
		result.IsError = parsed.IsError
		result.Usage = parsed.Usage
		result.ModelUsage = parsed.ModelUsage
		result.DurationMS = parsed.DurationMS
		result.DurationAPIMS = parsed.DurationAPIMS
		result.TotalCostUSD = parsed.TotalCostUSD
	}
	if runErr != nil {
		stderrText, _ := os.ReadFile(stderrPath)
		return result, fmt.Errorf("probe失敗(%s): %w%s", model, runErr, probeDiagnostic(rawOutputPath, stderrPath, stderrText))
	}
	if parseErr != nil {
		return result, &ProbeInvalidResponseError{Model: model, Reason: parseErr}
	}
	if err := ValidateProbeResult(result); err != nil {
		return result, &ProbeInvalidResponseError{Model: model, Reason: err}
	}
	return result, nil
}

type ProbeInvalidResponseError struct {
	Model  string
	Reason error
}

func (e *ProbeInvalidResponseError) Error() string {
	return fmt.Sprintf("probe不正応答(%s): %v", e.Model, e.Reason)
}

func (e *ProbeInvalidResponseError) Unwrap() error {
	return e.Reason
}

func ValidateProbeResult(result ProbeResult) error {
	if result.IsError {
		return fmt.Errorf("is_error=trueの応答を受け取りました: %q", strings.TrimSpace(result.Response))
	}
	if strings.TrimSpace(result.Response) != ProbeSentinel {
		return fmt.Errorf("応答がsentinel %qと一致しません: %q", ProbeSentinel, strings.TrimSpace(result.Response))
	}
	if result.Usage.OutputTokens <= 0 {
		return fmt.Errorf("model usageが出力tokenを含みません(output_tokens=%d)", result.Usage.OutputTokens)
	}
	return nil
}

func probeDiagnostic(rawOutputPath, stderrPath string, stderrText []byte) string {
	detail := string(stderrText)
	if raw, err := os.ReadFile(rawOutputPath); err == nil {
		detail += string(raw)
	}
	if detail == "" {
		return ""
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return ""
	}
	return " " + string(encoded)
}
