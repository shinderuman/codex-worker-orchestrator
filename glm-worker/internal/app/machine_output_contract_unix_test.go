//go:build unix

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const machineContractProbeArg = "--machine-contract-probe"

const machineContractCommandTimeout = 60 * time.Second

func TestDispatchCommandMachineOutputContract(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go commandがないため実binary testをskipします: %v", err)
	}
	binary, err := buildMultiRepoWorkerBinary(t)
	if err != nil {
		t.Fatal(err)
	}
	flags := dispatchCommandFlags(t)
	for _, required := range []string{"--accept", "--status", "--watch", "--install-smoke", "--decision-stdin"} {
		if !flags[required] {
			t.Fatalf("ParseCommand dispatchの展開に%sが含まれていません: %v", required, flags)
		}
	}

	for flag := range flags {
		for _, args := range [][]string{{flag}, {flag, machineContractProbeArg}} {
			name := strings.Join(args, " ")
			t.Run(name, func(t *testing.T) {
				outcome := runBinaryForMachineContract(t, binary, args, false)
				requireMachineProcessContract(t, args, outcome)
			})
		}
	}

	t.Run("引数なし", func(t *testing.T) {
		outcome := runBinaryForMachineContract(t, binary, nil, false)
		requireMachineProcessContract(t, nil, outcome)
	})

	t.Run("default payload mode", func(t *testing.T) {
		outcome := runBinaryForMachineContract(t, binary, []string{machineContractProbeArg}, true)
		requireMachineProcessContract(t, []string{machineContractProbeArg}, outcome)
	})
}

type machineProcessOutcome struct {
	runErr error
	stdout string
	stderr string
}

func runBinaryForMachineContract(t *testing.T, binary string, args []string, breakStateHome bool) machineProcessOutcome {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "glm-home")
	if breakStateHome {
		home = filepath.Join(root, "glm-home-file")
		if err := os.WriteFile(home, []byte("not a directory\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(home, 0o700); err != nil && !breakStateHome {
		t.Fatal(err)
	}
	cwd := filepath.Join(root, "cwd")
	if err := os.Mkdir(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	codexConfig := filepath.Join(root, "codex-config")
	claudeConfig := filepath.Join(root, "claude-config")
	for _, dir := range []string{codexConfig, claudeConfig} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), machineContractCommandTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = cwd
	command.Env = append(os.Environ(),
		"GLM_WORKER_HOME="+home,
		"CODEX_CONFIG_DIR="+codexConfig,
		"CLAUDE_CONFIG_DIR="+claudeConfig,
		"GLM_WORKER_CLAUDE_BIN="+filepath.Join(root, "missing-claude"),
		"GLM_WORKER_CODEX_BIN="+filepath.Join(root, "missing-codex"),
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	return machineProcessOutcome{runErr: runErr, stdout: stdout.String(), stderr: stderr.String()}
}

func requireMachineProcessContract(t *testing.T, args []string, outcome machineProcessOutcome) {
	t.Helper()
	rendered := strings.Join(args, " ")
	if outcome.runErr == nil {
		command, parseErr := ParseCommand(args)
		if parseErr != nil {
			t.Fatalf("成功した実行の引数をParseCommandできません: %v", parseErr)
		}
		if streamOutputMode(command.Mode) {
			requireStreamJSONLStdout(t, rendered, outcome.stdout)
			return
		}
		requireExactlyOneJSONStdout(t, rendered, outcome.stdout)
		return
	}
	exitErr := &exec.ExitError{}
	ok := errors.As(outcome.runErr, &exitErr)
	if !ok || exitErr.ExitCode() <= 0 {
		t.Fatalf("%s: 失敗時のexitがnon-zeroではありません: %v", rendered, outcome.runErr)
	}
	if strings.TrimSpace(outcome.stdout) != "" {
		t.Fatalf("%s: 失敗時のstdoutは空でなければなりません: %q", rendered, outcome.stdout)
	}
	trimmed := strings.TrimSpace(outcome.stderr)
	if trimmed == "" {
		t.Fatalf("%s: 失敗時のstderrにstructured process error JSONがありません", rendered)
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	var envelope struct {
		Error struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("%s: stderrがprocess error JSONとして解析できません: %v: %q", rendered, err, outcome.stderr)
	}
	if envelope.Error.Kind == "" || envelope.Error.Message == "" {
		t.Fatalf("%s: process errorのkind/messageが空です: %q", rendered, outcome.stderr)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("%s: stderrに2つ目のJSON valueがあります: %q", rendered, outcome.stderr)
	}
}

func requireExactlyOneJSONStdout(t *testing.T, name string, stdout string) {
	t.Helper()
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		t.Fatalf("%s: 成功時のstdoutが空です", name)
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		t.Fatalf("%s: 成功時のstdoutが単一JSON objectとして解析できません: %v: %q", name, err, stdout)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("%s: 成功時のstdoutに2つ目のJSON valueまたはtrailing textがあります: %q", name, stdout)
	}
}

func requireStreamJSONLStdout(t *testing.T, name string, stdout string) {
	t.Helper()
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		t.Fatalf("%s: stream成功時のstdoutが空です", name)
	}
	for _, line := range strings.Split(trimmed, "\n") {
		var object map[string]any
		if err := json.Unmarshal([]byte(line), &object); err != nil {
			t.Fatalf("%s: stream stdoutの行がJSON objectではありません: %v: %q", name, err, line)
		}
	}
}

func dispatchCommandFlags(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "app.go", nil, 0)
	if err != nil {
		t.Fatalf("ParseCommand sourceを解析できません: %v", err)
	}
	var declaration *ast.FuncDecl
	for _, candidate := range file.Decls {
		function, ok := candidate.(*ast.FuncDecl)
		if ok && function.Name.Name == "ParseCommand" {
			declaration = function
			break
		}
	}
	if declaration == nil {
		t.Fatal("app.goにParseCommandがありません")
	}
	flags := map[string]bool{}
	ast.Inspect(declaration.Body, func(node ast.Node) bool {
		clause, ok := node.(*ast.CaseClause)
		if !ok {
			return true
		}
		for _, expression := range clause.List {
			literal, ok := expression.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				continue
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatalf("dispatch case labelをunquoteできません: %v", err)
			}
			if strings.HasPrefix(value, "--") {
				flags[value] = true
			}
		}
		return true
	})
	if len(flags) == 0 {
		t.Fatal("ParseCommand dispatchからflagを列挙できませんでした")
	}
	return flags
}
