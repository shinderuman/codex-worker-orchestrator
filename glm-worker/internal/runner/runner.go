package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const isolationPolicyVersion = "claude-isolation-1"

const subtypeStructuredOutputRetryExhausted = "error_max_structured_output_retries"

type StructuredOutputError struct {
	Subtype        string
	TerminalReason string
}

func (e *StructuredOutputError) Error() string {
	if e.Subtype != "" {
		return fmt.Sprintf("structured outputが得られませんでした: subtype=%s", e.Subtype)
	}
	return "structured outputが得られませんでした: result eventにstructured_outputがありません"
}

func IsStructuredOutputError(err error) bool {
	var target *StructuredOutputError
	return errors.As(err, &target)
}

func (e *StructuredOutputError) RetryExhausted() bool {
	return e.Subtype == subtypeStructuredOutputRetryExhausted
}

var (
	workerSchemaOnce   sync.Once
	reviewerSchemaOnce sync.Once
	workerSchemaValue  string
	workerSchemaErr    error
	reviewerSchemaVal  string
	reviewerSchemaFail error
)

func structuredSchema(role state.SessionRole) (string, error) {
	if role == state.ReviewerRole {
		reviewerSchemaOnce.Do(func() {
			reviewerSchemaVal, reviewerSchemaFail = packet.ReviewerSchemaJSON()
		})
		return reviewerSchemaVal, reviewerSchemaFail
	}
	workerSchemaOnce.Do(func() {
		workerSchemaValue, workerSchemaErr = packet.WorkerSchemaJSON()
	})
	return workerSchemaValue, workerSchemaErr
}

type ClaudeRunner struct {
	config config.AppConfig
	state  *state.StateStore
	stop   *StopController
}

type TokenUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
}

type ModelUsage struct {
	InputTokens              int64   `json:"inputTokens"`
	CacheCreationInputTokens int64   `json:"cacheCreationInputTokens"`
	CacheReadInputTokens     int64   `json:"cacheReadInputTokens"`
	OutputTokens             int64   `json:"outputTokens"`
	CostUSD                  float64 `json:"costUSD,omitempty"`
}

type RunResult struct {
	SessionID          string
	Resumed            bool
	Response           string
	TopLevelUsage      TokenUsage
	ModelUsage         map[string]ModelUsage
	DurationMS         int64
	DurationAPIMS      int64
	TopLevelTurns      int
	TotalCostUSD       float64
	SystemPrompt       string
	SystemPromptBytes  int
	SystemPromptSHA256 string

	PlainFailure ProviderFailureClass

	StructuredOutput json.RawMessage
}

type claudeJSONResult struct {
	Type           string                `json:"type"`
	Subtype        string                `json:"subtype"`
	IsError        bool                  `json:"is_error"`
	Result         string                `json:"result"`
	StructuredOut  json.RawMessage       `json:"structured_output"`
	TerminalReason string                `json:"terminal_reason"`
	DurationMS     int64                 `json:"duration_ms"`
	DurationAPIMS  int64                 `json:"duration_api_ms"`
	NumTurns       int                   `json:"num_turns"`
	TotalCostUSD   float64               `json:"total_cost_usd"`
	Usage          TokenUsage            `json:"usage"`
	ModelUsage     map[string]ModelUsage `json:"modelUsage"`
}

func NewClaudeRunner(cfg config.AppConfig, st *state.StateStore) *ClaudeRunner {
	return &ClaudeRunner{config: cfg, state: st}
}

func (r *ClaudeRunner) AttachStopController(stop *StopController) {
	r.stop = stop
}

func (r *ClaudeRunner) Run(
	role state.SessionRole,
	phase string,
	model string,
	readOnly bool,
	effort string,
	prompt string,
	outputPath string,
) (RunResult, error) {
	if model == "" {
		return RunResult{}, fmt.Errorf("modelを指定してください")
	}

	if r.stop != nil && r.stop.StopRequested() {
		return RunResult{}, &InterruptedCallError{Phase: phase}
	}
	taskID, err := r.state.TaskID()
	if err != nil {
		return RunResult{}, err
	}
	if err := r.state.ResetSessionsForPolicy(isolationPolicyVersion); err != nil {
		return RunResult{}, err
	}
	sessionID, ready, err := r.state.SessionID(role)
	if err != nil {
		return RunResult{}, err
	}
	result := RunResult{SessionID: sessionID, Resumed: ready}
	if err := r.state.SetIsolationPolicy(isolationPolicyVersion); err != nil {
		return result, err
	}

	systemFile := filepath.Join(r.config.PromptDir, promptFileName(role))
	systemPrompt, err := os.ReadFile(systemFile)
	if err != nil {
		return result, fmt.Errorf("required promptがありません: %s", systemFile)
	}
	result.SystemPromptBytes = len(systemPrompt)
	result.SystemPrompt = string(systemPrompt)
	systemPromptHash := sha256.Sum256(systemPrompt)
	result.SystemPromptSHA256 = hex.EncodeToString(systemPromptHash[:])

	isolationArgs, err := isolationSettings(r.config.ClaudeConfigDir)
	if err != nil {
		return result, err
	}
	settingEnv, envDeletes, err := loadSettingEnv(r.config.ClaudeConfigDir, r.config.ClaudeSettingsOverride)
	if err != nil {
		return result, err
	}
	schema, err := structuredSchema(role)
	if err != nil {
		return result, fmt.Errorf("structured output schemaを構築できません: %w", err)
	}

	args := []string{"-p", "--safe-mode", "--setting-sources", ""}
	if ready {
		args = append(args, "--resume", sessionID)
	} else {
		args = append(
			args,
			"--session-id", sessionID,
			"--name", r.sessionName(role, taskID),
		)
	}

	args = append(
		args,
		"--model", model,
		"--effort", effort,
		"--autocompact", "500k",
		"--output-format", "stream-json",
		"--verbose",
		"--dangerously-skip-permissions",
		"--strict-mcp-config",
		"--mcp-config", `{"mcpServers":{}}`,
		"--disable-slash-commands",
		"--settings", isolationArgs,
		"--json-schema", schema,
	)

	if readOnly {
		args = append(args, "--disallowedTools", "Edit", "Write", "NotebookEdit", "Agent")
	}

	args = append(args, "--append-system-prompt-file", systemFile, prompt)

	stderrPath := outputPath + ".stderr"
	stderr, err := createPrivateFile(stderrPath)
	if err != nil {
		return result, err
	}
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		stderr.Close()
		return result, fmt.Errorf("/dev/nullを開けません: %w", err)
	}
	defer devNull.Close()

	ingester := r.newTaskEventIngester(taskID, role, phase, model, sessionID, ready)

	command := newProcessGroupCmd(r.config.ClaudeBin, args...)
	command.Dir = r.config.RepoRoot
	command.Stdin = devNull

	command.Stdout = ingester
	command.Stderr = stderr
	command.Env = buildChildEnv(r.config.EnvAllowlist, settingEnv, map[string]string{
		"CLAUDE_CONFIG_DIR":                r.config.ClaudeConfigDir,
		"CLAUDE_CODE_AUTO_COMPACT_WINDOW":  "500000",
		"CLAUDE_CODE_ALWAYS_ENABLE_EFFORT": "1",
	}, envDeletes)

	runErr := r.runCommand(command)
	ingester.flush()
	stderrCloseErr := stderr.Close()
	if runErr == nil && stderrCloseErr != nil {
		runErr = stderrCloseErr
	}

	parsed, parseErr := parseCapturedStreamResult(ingester.result())
	if parseErr == nil {
		result.Response = parsed.Result
		result.StructuredOutput = parsed.StructuredOut
		result.TopLevelUsage = parsed.Usage
		result.ModelUsage = parsed.ModelUsage
		result.DurationMS = parsed.DurationMS
		result.DurationAPIMS = parsed.DurationAPIMS
		result.TopLevelTurns = parsed.NumTurns
		result.TotalCostUSD = parsed.TotalCostUSD
	}

	if result.Response == "" {
		result.PlainFailure = classifyPlainStdoutFailure(ingester.plainSignal())
	}

	if err := writeResultOutput(outputPath, result.Response, streamResultSummary(parsed, parseErr), stderrPath); err != nil {
		return result, err
	}

	if parseErr == nil && parsed.Subtype == subtypeStructuredOutputRetryExhausted {
		return result, &StructuredOutputError{Subtype: parsed.Subtype, TerminalReason: parsed.TerminalReason}
	}
	if runErr != nil {
		return result, runErr
	}
	if parseErr != nil {
		return result, parseErr
	}
	if parsed.IsError {
		return result, fmt.Errorf("Claude CLIがerror結果を返しました: subtype=%s", parsed.Subtype)
	}

	if !structuredOutputPresent(result.StructuredOutput) {
		return result, &StructuredOutputError{}
	}

	if err := r.state.MarkReady(role); err != nil {
		return result, err
	}
	return result, nil
}

func createPrivateFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
}

func (r *ClaudeRunner) runCommand(command *exec.Cmd) error {
	if r.stop == nil {
		return command.Run()
	}
	if err := command.Start(); err != nil {
		return err
	}
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- command.Wait()
	}()
	select {
	case err := <-waitDone:
		return err
	case <-r.stop.Requested():

		select {
		case err := <-waitDone:
			return err
		default:
		}
		warning := terminateProcessGroup(command.Process.Pid, stopTermGrace)
		<-waitDone
		return &InterruptedCallError{CleanupWarning: warning}
	}
}

func parseClaudeJSONResult(path string) (claudeJSONResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return claudeJSONResult{}, err
	}
	var result claudeJSONResult
	if err := json.Unmarshal(data, &result); err != nil {
		return claudeJSONResult{}, fmt.Errorf("Claude CLIのJSON出力を解析できません: %w", err)
	}
	if result.Type != "result" {
		return claudeJSONResult{}, fmt.Errorf("Claude CLIのJSON出力typeが不正です: %q", result.Type)
	}
	return result, nil
}

func parseCapturedStreamResult(line []byte, found bool) (claudeJSONResult, error) {
	if !found {
		return claudeJSONResult{}, fmt.Errorf("Claude CLIのJSON出力typeが不正です: result eventがありません")
	}
	var parsed claudeJSONResult
	if err := json.Unmarshal(line, &parsed); err != nil {
		return claudeJSONResult{}, fmt.Errorf("Claude CLIのJSON出力を解析できません: %w", err)
	}
	return parsed, nil
}

func structuredOutputPresent(raw json.RawMessage) bool {
	return len(raw) != 0 && string(raw) != "null"
}

func (r *ClaudeRunner) newTaskEventIngester(
	taskID string,
	role state.SessionRole,
	phase string,
	model string,
	sessionID string,
	resumed bool,
) *streamEventIngester {
	callID, err := state.NewUUID()
	if err != nil {
		state.WarnTaskEventSkip("call ID生成", err)
		return &streamEventIngester{closed: true}
	}
	return newStreamEventIngester(r.state, taskID, callID, role, phase, model, sessionID, resumed)
}

func streamResultSummary(parsed claudeJSONResult, parseErr error) string {
	if parseErr != nil {
		return fmt.Sprintf("stream-json result unavailable: %v\n", parseErr)
	}
	if parsed.Result == "" {
		return fmt.Sprintf("stream result event: subtype=%s is_error=%v\n", parsed.Subtype, parsed.IsError)
	}
	return ""
}

func classifyPlainStdoutFailure(plain string) ProviderFailureClass {
	class := ClassifyProviderFailureText(plain)
	if class.Kind == ProviderFailureZaiFiveHour || class.Kind == ProviderFailureTransient {
		return class
	}
	return ProviderFailureClass{}
}

func writeResultOutput(outputPath string, response string, summary string, stderrPath string) error {
	var data []byte
	if response != "" {
		data = []byte(response)
		if data[len(data)-1] != '\n' {
			data = append(data, '\n')
		}
	}
	if summary != "" {
		data = append(data, summary...)
	}
	if stderr, err := os.ReadFile(stderrPath); err == nil && len(stderr) > 0 {
		data = append(data, stderr...)
	}
	return os.WriteFile(outputPath, data, 0o600)
}

func isolationSettings(claudeConfigDir string) (string, error) {
	configDir, err := resolveClaudeConfigDir(claudeConfigDir)
	if err != nil {
		return "", err
	}
	settings := map[string]any{
		"claudeMdExcludes": []string{
			"**/CLAUDE.md",
			"**/CLAUDE.local.md",
			filepath.Join(configDir, "CLAUDE.md"),
			filepath.Join(configDir, "rules", "**"),
		},
		"autoMemoryEnabled":    false,
		"disableAllHooks":      true,
		"disableBundledSkills": true,
		"disableWorkflows":     true,
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return "", fmt.Errorf("隔離settingsを構築できません: %w", err)
	}
	return string(encoded), nil
}

var essentialSettingEnvKeys = []string{
	"ANTHROPIC_AUTH_TOKEN",
	"ANTHROPIC_BASE_URL",
	"ANTHROPIC_DEFAULT_OPUS_MODEL",
	"ANTHROPIC_DEFAULT_SONNET_MODEL",
	"ANTHROPIC_DEFAULT_HAIKU_MODEL",
	"API_TIMEOUT_MS",
	"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC",
}

func loadSettingEnv(claudeConfigDir string, overridePath string) (map[string]string, []string, error) {
	configDir, err := resolveClaudeConfigDir(claudeConfigDir)
	if err != nil {
		return nil, nil, err
	}
	result := make(map[string]string)
	data, err := os.ReadFile(filepath.Join(configDir, "settings.json"))
	if err == nil {
		var parsed struct {
			Env map[string]string `json:"env"`
		}
		if err := json.Unmarshal(data, &parsed); err != nil {
			return nil, nil, fmt.Errorf("Claude settings.jsonを解析できません: %w", err)
		}
		for _, key := range essentialSettingEnvKeys {
			if value, ok := parsed.Env[key]; ok && value != "" {
				result[key] = value
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("Claude settings.jsonを読み込めません: %w", err)
	}

	override, err := parseClaudeEnvOverride(overridePath)
	if err != nil {
		return nil, nil, fmt.Errorf("env override: %w", err)
	}
	for _, key := range override.deletes {
		delete(result, key)
	}
	for key, value := range override.sets {
		result[key] = value
	}
	return result, override.deletes, nil
}

var osEssentialEnvKeys = []string{
	"PATH", "HOME", "TMPDIR", "SHELL", "USER", "LOGNAME",
	"LANG", "LC_ALL", "LC_CTYPE", "TZ", "TERM",
}

func buildChildEnv(extraAllow []string, settingEnv, additions map[string]string, deletes []string) []string {
	allowed := make(map[string]struct{}, len(osEssentialEnvKeys)+len(extraAllow))
	for _, key := range osEssentialEnvKeys {
		allowed[key] = struct{}{}
	}
	for _, key := range extraAllow {
		allowed[key] = struct{}{}
	}
	denied := make(map[string]struct{}, len(deletes))
	for _, key := range deletes {
		denied[key] = struct{}{}
	}

	child := make(map[string]string)
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if _, ok := allowed[key]; !ok {
			continue
		}
		if _, deny := denied[key]; deny {
			continue
		}
		child[key] = value
	}
	for key, value := range settingEnv {
		child[key] = value
	}
	for key, value := range additions {
		child[key] = value
	}

	keys := make([]string, 0, len(child))
	for key := range child {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+child[key])
	}
	return result
}

func resolveClaudeConfigDir(claudeConfigDir string) (string, error) {
	if claudeConfigDir != "" {
		return claudeConfigDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("ホームディレクトリを取得できません: %w", err)
	}
	return filepath.Join(home, ".claude"), nil
}

func promptFileName(role state.SessionRole) string {
	if role == state.ReviewerRole {
		return "REVIEWER.md"
	}
	return "WORKER.md"
}

func (r *ClaudeRunner) sessionName(role state.SessionRole, taskID string) string {
	if len(taskID) > 8 {
		taskID = taskID[:8]
	}
	return fmt.Sprintf("glm-%s-%s-%s", role, r.config.RepoShort, taskID)
}
