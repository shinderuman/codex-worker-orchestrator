package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func writeQualityGateGoShim(t *testing.T, failFlagPath string) (string, string) {
	t.Helper()
	shimDir := t.TempDir()
	invocationLog := filepath.Join(t.TempDir(), "go-invocations.log")
	shim := filepath.Join(shimDir, "go")
	body := "#!/bin/sh\n" +
		"log_file='" + invocationLog + "'\n" +
		"printf 'argv:%s\\n' \"$*\" >>\"$log_file\"\n" +
		"printf 'goflags:%s\\n' \"$GOFLAGS\" >>\"$log_file\"\n" +
		"if [ -f '" + failFlagPath + "' ]; then\n" +
		"  printf '%s\\n' 'go shim: forced failure'\n" +
		"  exit 3\n" +
		"fi\n" +
		"printf '%s\\n' 'go shim: ok'\n"
	if err := os.WriteFile(shim, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return shimDir, invocationLog
}

func newQualityGateEnv(t *testing.T) (config.AppConfig, *state.StateStore) {
	t.Helper()
	repoRoot := t.TempDir()
	cfg := config.AppConfig{
		RepoRoot:  repoRoot,
		RepoHash:  config.RepoHashFor(repoRoot),
		RepoShort: config.RepoHashFor(repoRoot)[:12],
		StateBase: filepath.Join(t.TempDir(), "state"),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, st
}

func useInlineQualityGateRunner(t *testing.T) {
	t.Helper()
	previous := launchQualityGateRunner
	launchQualityGateRunner = func(st *state.StateStore, record qualityGateRunRecord) (qualityGateRunnerWait, error) {
		return func() error { return executeQualityGateRun(st, record.ValidationRunID) }, nil
	}
	t.Cleanup(func() { launchQualityGateRunner = previous })
}

func qualityGateInvocationLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func TestQualityGateParseAcceptsOnlyExactForms(t *testing.T) {
	cases := []struct {
		args     []string
		wantForm string
		wantErr  bool
	}{
		{args: []string{"--quality-gate", "go-test"}, wantForm: "go-test"},
		{args: []string{"--quality-gate", "go-test-race"}, wantForm: "go-test-race"},
		{args: []string{"--quality-gate"}, wantErr: true},
		{args: []string{"--quality-gate", "bogus"}, wantErr: true},
		{args: []string{"--quality-gate", "go-test", "go-test"}, wantErr: true},
		{args: []string{"--quality-gate", "go-test", "-exec", "/bin/sh"}, wantErr: true},
		{args: []string{"--quality-gate", "go-test-race", "-count=1"}, wantErr: true},
		{args: []string{"--quality-gate", "go-test", "--", "./internal/app"}, wantErr: true},
		{args: []string{"--quality-gate", "", "go-test"}, wantErr: true},
	}
	for _, tc := range cases {
		cmd, err := ParseCommand(tc.args)
		if tc.wantErr {
			var usage *UsageError
			if err == nil {
				t.Fatalf("ParseCommand(%v)がerrorを返しませんでした", tc.args)
			}
			if !errors.As(err, &usage) {
				t.Fatalf("ParseCommand(%v)がUsageError以外を返しました: %v", tc.args, err)
			}
			if !strings.Contains(usage.Message, "usage: glm-worker --quality-gate") {
				t.Fatalf("usage文が想定と異なります: %s", usage.Message)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseCommand(%v)がerrorを返しました: %v", tc.args, err)
		}
		if cmd.Mode != ModeQualityGate || cmd.Payload != tc.wantForm {
			t.Fatalf("ParseCommand(%v)の結果が想定と異なります: %+v", tc.args, cmd)
		}
	}
}

func TestQualityGateExtraArgvFailsClosedBeforeProcess(t *testing.T) {
	shimDir, invocationLog := writeQualityGateGoShim(t, filepath.Join(t.TempDir(), "absent-flag"))
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	loadConfigCalled := false
	var stderr bytes.Buffer
	err := run([]string{"--quality-gate", "go-test", "-exec", "/bin/sh"},
		func() (config.AppConfig, error) {
			loadConfigCalled = true
			return config.AppConfig{}, nil
		},
		defaultRunnerFactory,
		nil,
		&bytes.Buffer{},
		&stderr,
	)
	var usage *UsageError
	if err == nil || !errors.As(err, &usage) {
		t.Fatalf("余分なargvがusage errorになりませんでした: %v", err)
	}
	if loadConfigCalled {
		t.Fatal("config読込より前にfail closedしていません")
	}
	if lines := qualityGateInvocationLines(t, invocationLog); len(lines) != 0 {
		t.Fatalf("go processが起動されています: %v", lines)
	}
}

func TestQualityGateRunsFixedArgv(t *testing.T) {
	cases := []struct {
		form       string
		wantArgv   string
		wantRecord int
	}{
		{form: "go-test", wantArgv: "test ./...", wantRecord: 2},
		{form: "go-test-race", wantArgv: "test -race ./...", wantRecord: 2},
	}
	for _, tc := range cases {
		t.Run(tc.form, func(t *testing.T) {
			useInlineQualityGateRunner(t)
			shimDir, invocationLog := writeQualityGateGoShim(t, filepath.Join(t.TempDir(), "absent-flag"))
			t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("GOFLAGS", "-exec=/bin/sh")
			_, st := newQualityGateEnv(t)

			workingDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			var stdout bytes.Buffer
			if err := runQualityGate(tc.form, st, &stdout); err != nil {
				t.Fatalf("runQualityGateがerrorを返しました: %v", err)
			}

			var out qualityGateOutput
			if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
				t.Fatalf("stdoutが単一JSON objectではありません: %v: %s", err, stdout.String())
			}
			if out.Status != qualityGateStatusPass || out.Form != tc.form {
				t.Fatalf("結果JSONが想定と異なります: %+v", out)
			}
			if out.Command != "go "+tc.wantArgv {
				t.Fatalf("command表記が想定と異なります: %s", out.Command)
			}
			if out.WorkingDir != workingDir {
				t.Fatalf("working_dirが現在dirと一致しません: %s", out.WorkingDir)
			}
			logData, err := os.ReadFile(out.Log)
			if err != nil {
				t.Fatalf("log fileが読めません: %v", err)
			}
			if !strings.Contains(string(logData), "go shim: ok") {
				t.Fatalf("logへsubprocess出力が保存されていません: %s", logData)
			}

			lines := qualityGateInvocationLines(t, invocationLog)
			if len(lines) != tc.wantRecord {
				t.Fatalf("go呼出記録が想定と異なります: %v", lines)
			}
			if lines[0] != "argv:"+tc.wantArgv {
				t.Fatalf("固定argvが起動されていません: %s", lines[0])
			}
			if lines[1] != "goflags:" {
				t.Fatalf("GOFLAGSが排除されていません: %s", lines[1])
			}
		})
	}
}

func TestQualityGateFailureIsStructuredProcessError(t *testing.T) {
	useInlineQualityGateRunner(t)
	failFlagPath := filepath.Join(t.TempDir(), "fail-flag")
	if err := os.WriteFile(failFlagPath, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	shimDir, invocationLog := writeQualityGateGoShim(t, failFlagPath)
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, st := newQualityGateEnv(t)

	var stdout bytes.Buffer
	err := runQualityGate("go-test", st, &stdout)
	if err == nil {
		t.Fatal("失敗がerrorとして返りませんでした")
	}
	if stdout.Len() != 0 {
		t.Fatalf("失敗時にstdoutへ出力されています: %s", stdout.String())
	}
	var gateFail *QualityGateError
	if !errors.As(err, &gateFail) {
		t.Fatalf("QualityGateError以外が返りました: %v", err)
	}
	if gateFail.ExitCode != 3 || gateFail.Form != "go-test" || !validValidationRunID(gateFail.ValidationRunID) {
		t.Fatalf("失敗detailが想定と異なります: %+v", gateFail)
	}
	if _, statErr := os.Stat(gateFail.LogPath); statErr != nil {
		t.Fatalf("失敗logが保存されていません: %v", statErr)
	}
	failLog, readErr := os.ReadFile(gateFail.LogPath)
	if readErr != nil || !strings.Contains(string(failLog), "go shim: forced failure") {
		t.Fatalf("失敗logへsubprocess出力が保存されていません: %s", failLog)
	}

	var stderr bytes.Buffer
	if writeErr := WriteProcessError(&stderr, err); writeErr != nil {
		t.Fatal(writeErr)
	}
	var envelope struct {
		Error struct {
			Kind    string         `json:"kind"`
			Message string         `json:"message"`
			Detail  map[string]any `json:"detail"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
		t.Fatalf("structured error JSONではありません: %v: %s", err, stderr.String())
	}
	if envelope.Error.Kind != "quality_gate_failed" {
		t.Fatalf("error kindが想定と異なります: %s", envelope.Error.Kind)
	}
	if envelope.Error.Detail["exit_code"] != float64(3) || envelope.Error.Detail["command"] != "go test ./..." {
		t.Fatalf("error detailが想定と異なります: %+v", envelope.Error.Detail)
	}
	if lines := qualityGateInvocationLines(t, invocationLog); len(lines) == 0 {
		t.Fatal("go processが起動されていません")
	}
}
