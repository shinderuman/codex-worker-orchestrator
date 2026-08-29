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
	"strconv"
	"strings"
	"sync"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type StructuredOutputError struct {
	Subtype        string
	TerminalReason string
}

type ClaudeRunner struct {
	config      config.AppConfig
	state       *state.StateStore
	stop        *StopController
	bashSandbox *gitBashSandboxPolicy
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
	InstructionReads   []string
	CallID             string

	ResolvedModelID                   string
	ConfiguredAutoCompactWindowTokens int
	KnownModelContextWindowTokens     int
	DeclaredMaxContextWindowTokens    int
	ContextWindowSource               string

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

type runInputs struct {
	systemFile    string
	isolationArgs string
	settingEnv    map[string]string
	envDeletes    []string
	schema        string
	contextWindow contextWindowConfig
}

const isolationPolicyVersion = "claude-isolation-2"

const subtypeStructuredOutputRetryExhausted = "error_max_structured_output_retries"

const highFloorPhaseMarker = "high-floor"

const readOnlyTools = "Read,Grep,Glob,WebFetch,WebSearch"

var readOnlyDisallowedTools = []string{"Edit", "Write", "NotebookEdit", "Agent", "Bash"}

var (
	workerSchemaOnce            sync.Once
	reviewerSchemaOnce          sync.Once
	riskFloorReviewerSchemaOnce sync.Once
	workerSchemaValue           string
	workerSchemaErr             error
	reviewerSchemaVal           string
	reviewerSchemaFail          error
	riskFloorReviewerSchemaVal  string
	riskFloorReviewerSchemaErr  error
)

var essentialSettingEnvKeys = []string{
	"ANTHROPIC_AUTH_TOKEN",
	"ANTHROPIC_BASE_URL",
	"ANTHROPIC_DEFAULT_OPUS_MODEL",
	"ANTHROPIC_DEFAULT_SONNET_MODEL",
	"ANTHROPIC_DEFAULT_HAIKU_MODEL",
	"API_TIMEOUT_MS",
	"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC",
}

var osEssentialEnvKeys = []string{
	"PATH", "HOME", "TMPDIR", "SHELL", "USER", "LOGNAME",
	"LANG", "LC_ALL", "LC_CTYPE", "TZ", "TERM",
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

func structuredSchema(role state.SessionRole, phase string) (string, error) {
	if role == state.ReviewerRole {
		if strings.Contains(phase, highFloorPhaseMarker) {
			return packet.HighFloorReviewerSchemaJSON()
		}
		if strings.HasSuffix(phase, "risk-floor") {
			riskFloorReviewerSchemaOnce.Do(func() {
				riskFloorReviewerSchemaVal, riskFloorReviewerSchemaErr = packet.RiskFloorReviewerSchemaJSON()
			})
			return riskFloorReviewerSchemaVal, riskFloorReviewerSchemaErr
		}
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
	ingester, stderrPath, runErr, err := r.executeRunCommand(
		role, phase, model, taskID, sessionID, callID, ready, args, inputs, outputPath,
	)
	if err != nil {
		return result, err
	}
	return r.finishRun(role, outputPath, stderrPath, ingester, result, runErr)
}

func (r *ClaudeRunner) prepareRunSession(role state.SessionRole, phase, model string) (RunResult, string, string, bool, error) {
	if model == "" {
		return RunResult{}, "", "", false, fmt.Errorf("modelを指定してください")
	}
	if r.stop != nil && r.stop.StopRequested() {
		return RunResult{}, "", "", false, &InterruptedCallError{Phase: phase}
	}
	taskID, err := r.state.TaskID()
	if err != nil {
		return RunResult{}, "", "", false, err
	}
	if err := r.state.ResetSessionsForPolicy(isolationPolicyVersion); err != nil {
		return RunResult{}, "", "", false, err
	}
	sessionID, ready, err := r.state.SessionID(role)
	if err != nil {
		return RunResult{}, "", "", false, err
	}
	result := RunResult{SessionID: sessionID, Resumed: ready}
	if err := r.state.SetIsolationPolicy(isolationPolicyVersion); err != nil {
		return result, "", "", false, err
	}
	return result, taskID, sessionID, ready, nil
}

func (r *ClaudeRunner) prepareRunInputs(role state.SessionRole, phase, model string, result *RunResult) (runInputs, error) {
	systemFile := filepath.Join(r.config.PromptDir, promptFileName(role))
	systemPrompt, err := os.ReadFile(systemFile)
	if err != nil {
		return runInputs{}, fmt.Errorf("required promptがありません: %s", systemFile)
	}
	result.SystemPromptBytes = len(systemPrompt)
	result.SystemPrompt = string(systemPrompt)
	systemPromptHash := sha256.Sum256(systemPrompt)
	result.SystemPromptSHA256 = hex.EncodeToString(systemPromptHash[:])

	isolationArgs, err := isolationSettings(r.config.ClaudeConfigDir, r.bashSandbox)
	if err != nil {
		return runInputs{}, err
	}
	settingEnv, envDeletes, err := loadSettingEnv(r.config.ClaudeConfigDir, r.config.ClaudeSettingsOverride)
	if err != nil {
		return runInputs{}, err
	}
	contextWindow := contextWindowForModel(model, settingEnv)
	result.ResolvedModelID = contextWindow.resolvedModelID
	result.ConfiguredAutoCompactWindowTokens = configuredAutoCompactWindowTokens
	result.KnownModelContextWindowTokens = contextWindow.knownModelContextWindowTokens
	result.DeclaredMaxContextWindowTokens = contextWindow.declaredMaxContextTokens
	result.ContextWindowSource = contextWindow.source
	schema, err := structuredSchema(role, phase)
	if err != nil {
		return runInputs{}, fmt.Errorf("structured output schemaを構築できません: %w", err)
	}
	return runInputs{
		systemFile: systemFile, isolationArgs: isolationArgs,
		settingEnv: settingEnv, envDeletes: envDeletes, schema: schema, contextWindow: contextWindow,
	}, nil
}

func (r *ClaudeRunner) buildRunArgs(
	role state.SessionRole,
	taskID, sessionID string,
	ready bool,
	model string,
	readOnly bool,
	effort, prompt string,
	inputs runInputs,
) []string {
	args := []string{"-p", "--safe-mode", "--setting-sources", ""}
	if ready {
		args = append(args, "--resume", sessionID)
	} else {
		args = append(args, "--session-id", sessionID, "--name", r.sessionName(role, taskID))
	}
	args = append(args,
		"--model", model,
		"--effort", effort,
		"--autocompact", configuredAutoCompactWindowArgument,
		"--output-format", "stream-json",
		"--verbose",
		"--dangerously-skip-permissions",
		"--strict-mcp-config",
		"--mcp-config", `{"mcpServers":{}}`,
		"--disable-slash-commands",
		"--settings", inputs.isolationArgs,
		"--json-schema", inputs.schema,
	)
	if readOnly {
		args = append(args, "--tools", readOnlyTools, "--disallowedTools")
		args = append(args, readOnlyDisallowedTools...)
	}
	return append(args, "--append-system-prompt-file", inputs.systemFile, prompt)
}

func (r *ClaudeRunner) executeRunCommand(
	role state.SessionRole,
	phase, model, taskID, sessionID, callID string,
	ready bool,
	args []string,
	inputs runInputs,
	outputPath string,
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
	additions := map[string]string{
		"CLAUDE_CONFIG_DIR":                r.config.ClaudeConfigDir,
		"CLAUDE_CODE_AUTO_COMPACT_WINDOW":  strconv.Itoa(configuredAutoCompactWindowTokens),
		"CLAUDE_CODE_ALWAYS_ENABLE_EFFORT": "1",
	}
	if inputs.contextWindow.declaredMaxContextTokens > 0 {
		additions["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] = strconv.Itoa(inputs.contextWindow.declaredMaxContextTokens)
	}
	command.Env = buildChildEnv(r.config.EnvAllowlist, inputs.settingEnv, additions, inputs.envDeletes)

	runErr := r.runCommand(command)
	ingester.flush()
	if closeErr := stderr.Close(); runErr == nil && closeErr != nil {
		runErr = closeErr
	}
	return ingester, stderrPath, runErr, nil
}

func (r *ClaudeRunner) finishRun(
	role state.SessionRole,
	outputPath, stderrPath string,
	ingester *streamEventIngester,
	result RunResult,
	runErr error,
) (RunResult, error) {
	result.InstructionReads = ingester.instructionReadNames()
	parsed, parseErr := parseCapturedStreamResult(ingester.result())
	if parseErr == nil {
		applyParsedRunResult(&result, parsed)
	}
	if result.Response == "" {
		result.PlainFailure = classifyPlainStdoutFailure(ingester.plainSignal())
	}
	if err := writeResultOutput(outputPath, result.Response, streamResultSummary(parsed, parseErr), stderrPath); err != nil {
		return result, err
	}
	if err := terminalRunError(result, parsed, parseErr, runErr); err != nil {
		return result, err
	}
	if err := r.state.MarkReady(role); err != nil {
		return result, err
	}
	return result, nil
}

func applyParsedRunResult(result *RunResult, parsed claudeJSONResult) {
	result.Response = parsed.Result
	result.StructuredOutput = parsed.StructuredOut
	result.TopLevelUsage = parsed.Usage
	result.ModelUsage = parsed.ModelUsage
	result.DurationMS = parsed.DurationMS
	result.DurationAPIMS = parsed.DurationAPIMS
	result.TopLevelTurns = parsed.NumTurns
	result.TotalCostUSD = parsed.TotalCostUSD
}

func terminalRunError(result RunResult, parsed claudeJSONResult, parseErr, runErr error) error {
	if parseErr == nil && parsed.Subtype == subtypeStructuredOutputRetryExhausted {
		return &StructuredOutputError{Subtype: parsed.Subtype, TerminalReason: parsed.TerminalReason}
	}
	if runErr != nil {
		return runErr
	}
	if parseErr != nil {
		return parseErr
	}
	if parsed.IsError {
		return fmt.Errorf("claude CLIがerror結果を返しました: subtype=%s", parsed.Subtype)
	}
	if !structuredOutputPresent(result.StructuredOutput) {
		return &StructuredOutputError{}
	}
	return nil
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
		return claudeJSONResult{}, fmt.Errorf("claude CLIのJSON出力を解析できません: %w", err)
	}
	if result.Type != "result" {
		return claudeJSONResult{}, fmt.Errorf("claude CLIのJSON出力typeが不正です: %q", result.Type)
	}
	return result, nil
}

func parseCapturedStreamResult(line []byte, found bool) (claudeJSONResult, error) {
	if !found {
		return claudeJSONResult{}, fmt.Errorf("claude CLIのJSON出力typeが不正です: result eventがありません")
	}
	var parsed claudeJSONResult
	if err := json.Unmarshal(line, &parsed); err != nil {
		return claudeJSONResult{}, fmt.Errorf("claude CLIのJSON出力を解析できません: %w", err)
	}
	return parsed, nil
}

func structuredOutputPresent(raw json.RawMessage) bool {
	return len(raw) != 0 && string(raw) != "null"
}

func (r *ClaudeRunner) newTaskEventIngester(
	taskID, callID string,
	role state.SessionRole,
	phase string,
	model string,
	sessionID string,
	resumed bool,
) *streamEventIngester {
	if callID == "" {
		return &streamEventIngester{closed: true}
	}
	ingester := newStreamEventIngester(r.state, taskID, callID, role, phase, model, sessionID, resumed)
	ingester.workerInstructionDir = filepath.Join(r.config.CodexConfigDir, "instructions", "worker")
	return ingester
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

func isolationSettings(claudeConfigDir string, sandbox *gitBashSandboxPolicy) (string, error) {
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
	if sandbox != nil {
		settings["sandbox"] = sandbox.settings()
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return "", fmt.Errorf("隔離settingsを構築できません: %w", err)
	}
	return string(encoded), nil
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
			return nil, nil, fmt.Errorf("claude settings.jsonを解析できません: %w", err)
		}
		for _, key := range essentialSettingEnvKeys {
			if value, ok := parsed.Env[key]; ok && value != "" {
				result[key] = value
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("claude settings.jsonを読み込めません: %w", err)
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
