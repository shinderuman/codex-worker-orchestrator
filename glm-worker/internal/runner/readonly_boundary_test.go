package runner

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const readonlyBoundaryResultEvent = `{"type":"result","subtype":"success","is_error":false,"structured_output":{"status":"IMPLEMENTED","risk":"LOW","summary":"done","requirement_coverage":"covered","tests":"pass","unverified":"none"},"result":"boundary output\n","duration_ms":100,"duration_api_ms":50,"num_turns":1,"usage":{"input_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":1}}`

var readOnlyBoundaryTools = []string{"Read", "Grep", "Glob", "WebFetch", "WebSearch"}

func writeReadOnlyBoundaryFakeClaude(t *testing.T) string {
	t.Helper()
	argumentsPath := filepath.Join(t.TempDir(), "args")
	commandPath := filepath.Join(t.TempDir(), "fake-claude")
	commandScript := "#!/bin/sh\nprintf '%s\\n' \"$@\" >\"$GLM_ARGS_FILE\"\nprintf '%s\\n' '" + readonlyBoundaryResultEvent + "'\n"
	if err := os.WriteFile(commandPath, []byte(commandScript), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GLM_ARGS_FILE", argumentsPath)
	return commandPath
}

func newReadOnlyBoundaryRunner(t *testing.T, bin string, repository string) *ClaudeRunner {
	t.Helper()
	promptDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(promptDir, "WORKER.md"), []byte("system"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := newTestStateStore(t)
	if err := st.Write("task.id", "12345678-aaaa-bbbb-cccc-dddddddddddd"); err != nil {
		t.Fatal(err)
	}
	return NewClaudeRunner(config.AppConfig{
		RepoRoot:     repository,
		RepoShort:    "abcdef123456",
		PromptDir:    promptDir,
		ClaudeBin:    bin,
		EnvAllowlist: []string{"GLM_ARGS_FILE"},
		WorkerModel:  "worker-model",
	}, st)
}

func TestClaudeRunnerReadOnlyRunExposesNoWriteCapableTool(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	bin := writeReadOnlyBoundaryFakeClaude(t)
	r := newReadOnlyBoundaryRunner(t, bin, t.TempDir())

	output := filepath.Join(t.TempDir(), "out.log")
	if _, err := r.Run(state.WorkerRole, "worker-new", "worker-model", true, "high", "boundary prompt", output); err != nil {
		t.Fatal(err)
	}
	arguments := readLines(t, os.Getenv("GLM_ARGS_FILE"))

	tools := argumentAfter(arguments, "--tools")
	gotTools := strings.Split(tools, ",")
	if !reflect.DeepEqual(gotTools, readOnlyBoundaryTools) {
		t.Fatalf("read-only --tools = %#v want %#v", gotTools, readOnlyBoundaryTools)
	}
	disallowedAt := -1
	for index, argument := range arguments {
		if argument == "--disallowedTools" {
			disallowedAt = index
			break
		}
	}
	if disallowedAt < 0 {
		t.Fatalf("--disallowedToolsがありません: %#v", arguments)
	}
	gotDisallowed := arguments[disallowedAt+1 : disallowedAt+1+len(readOnlyDisallowedTools)]
	if !reflect.DeepEqual(gotDisallowed, readOnlyDisallowedTools) {
		t.Fatalf("read-only --disallowedTools = %#v want %#v", gotDisallowed, readOnlyDisallowedTools)
	}
}

func TestClaudeRunnerWriteRunKeepsFullToolSurface(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	bin := writeReadOnlyBoundaryFakeClaude(t)
	r := newReadOnlyBoundaryRunner(t, bin, t.TempDir())

	output := filepath.Join(t.TempDir(), "out.log")
	if _, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "boundary prompt", output); err != nil {
		t.Fatal(err)
	}
	arguments := readLines(t, os.Getenv("GLM_ARGS_FILE"))
	for _, argument := range arguments {
		if argument == "--tools" || argument == "--disallowedTools" {
			t.Fatalf("write capability runへtool制限が漏れています: %s: %#v", argument, arguments)
		}
	}
}

func TestClaudeRunnerReadOnlyRunRejectsRealBashWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("live検証はUnix系環境向け")
	}
	bin := os.Getenv("GLM_WORKER_CLAUDE_BIN")
	if bin == "" {
		if _, err := exec.LookPath("claude"); err != nil {
			t.Skipf("claude CLIが利用できません: %v", err)
		}
		bin = "claude"
	}
	probeRunner := NewClaudeRunner(config.AppConfig{ClaudeBin: bin}, newTestStateStore(t))
	if _, err := probeRunner.Probe("haiku"); err != nil {
		t.Skipf("live modelが利用できないため実Bash write境界を検証できません: %v", err)
	}

	prompt := "Use the Bash tool to run exactly: echo pwned > bash-write-probe.txt — then reply DONE"
	controlRepository := t.TempDir()
	controlRunner := newReadOnlyBoundaryRunner(t, bin, controlRepository)
	controlOutput := filepath.Join(t.TempDir(), "control.log")
	controlResult, controlErr := controlRunner.Run(state.WorkerRole, "worker-readonly-control", "haiku", false, "low", prompt, controlOutput)
	requireLiveRoundtrip(t, controlResult, controlErr, "write capability control")
	controlTarget := filepath.Join(controlRepository, "bash-write-probe.txt")
	if _, err := os.Stat(controlTarget); err != nil {
		t.Skipf("write capability controlでもBash writeが起きず境界検証の前提が立ちません: %v", err)
	}

	readOnlyRepository := t.TempDir()
	readOnlyRunner := newReadOnlyBoundaryRunner(t, bin, readOnlyRepository)
	readOnlyOutput := filepath.Join(t.TempDir(), "readonly.log")
	readOnlyResult, readOnlyErr := readOnlyRunner.Run(state.WorkerRole, "worker-readonly-boundary", "haiku", true, "low", prompt, readOnlyOutput)
	requireLiveRoundtrip(t, readOnlyResult, readOnlyErr, "read-only boundary")
	readOnlyTarget := filepath.Join(readOnlyRepository, "bash-write-probe.txt")
	if _, err := os.Stat(readOnlyTarget); err == nil {
		t.Fatalf("read-only runで実Bash writeが実行されました: %s", readOnlyTarget)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

func requireLiveRoundtrip(t *testing.T, result RunResult, err error, label string) {
	t.Helper()
	if result.Response == "" {
		t.Fatalf("%s: model応答がありません(err=%v)", label, err)
	}
	var structuredErr *StructuredOutputError
	if err != nil && !errors.As(err, &structuredErr) {
		t.Fatalf("%s: 許容できないerror = %v", label, err)
	}
}
