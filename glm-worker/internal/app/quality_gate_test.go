package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionunit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

var qualityGateShimPassEvents = strings.Join([]string{
	`{"Action":"start","Package":"example.com/m"}`,
	`{"Action":"run","Package":"example.com/m","Test":"TestShimGate"}`,
	`{"Action":"output","Package":"example.com/m","Test":"TestShimGate","Output":"--- PASS: TestShimGate (0.20s)\n"}`,
	`{"Action":"pass","Package":"example.com/m","Test":"TestShimGate","Elapsed":0.2}`,
	`{"Action":"output","Package":"example.com/m","Output":"ok  example.com/m 0.30s\n"}`,
	`{"Action":"pass","Package":"example.com/m","Elapsed":0.3}`,
}, "\n")

var qualityGateShimFailEvents = strings.Join([]string{
	`{"Action":"start","Package":"example.com/m"}`,
	`{"Action":"run","Package":"example.com/m","Test":"TestShimGate"}`,
	`{"Action":"output","Package":"example.com/m","Test":"TestShimGate","Output":"--- FAIL: TestShimGate (0.10s)\n"}`,
	`{"Action":"fail","Package":"example.com/m","Test":"TestShimGate","Elapsed":0.1}`,
	`{"Action":"output","Package":"example.com/m","Output":"FAIL example.com/m\n"}`,
	`{"Action":"fail","Package":"example.com/m","Elapsed":0.1}`,
}, "\n")

func writeQualityGateGoShim(t *testing.T, failFlagPath string) (string, string) {
	t.Helper()
	shimDir := t.TempDir()
	invocationLog := filepath.Join(t.TempDir(), "go-invocations.log")
	shim := filepath.Join(shimDir, "go")
	testEvents := "  if [ \"$1\" = test ]; then\n" +
		"    cat <<'EOF'\n%s\nEOF\n"
	body := "#!/bin/sh\n" +
		"log_file='" + invocationLog + "'\n" +
		"if [ \"$1\" = env ]; then\n" +
		"  printf 'goflags:%s\\n' \"$GOFLAGS\" >>\"$log_file.env\"\n" +
		"  printf '%s\\n' '{\"GOVERSION\":\"go1.25.4\",\"GOOS\":\"darwin\",\"GOARCH\":\"arm64\",\"GOCACHE\":\"/fixture/cache\",\"GOFLAGS\":\"\"}'\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf 'argv:%s\\n' \"$*\" >>\"$log_file\"\n" +
		"printf 'goflags:%s\\n' \"$GOFLAGS\" >>\"$log_file\"\n" +
		"printf 'claim:%s\\n' \"$GLM_SESSION_ROTATION_CLAIM_ID\" >>\"$log_file\"\n" +
		"printf 'thread:%s\\n' \"$GLM_PARENT_ACTION_CODEX_THREAD_ID\" >>\"$log_file\"\n" +
		"printf 'session:%s\\n' \"$GLM_PARENT_ACTION_CODEX_SESSION_ID\" >>\"$log_file\"\n" +
		"printf 'execution-unit:%s\\n' \"$GLM_EXECUTION_UNIT_DISPOSITION\" >>\"$log_file\"\n" +
		"if [ -f '" + failFlagPath + "' ]; then\n" +
		fmt.Sprintf(testEvents, qualityGateShimFailEvents) +
		"  fi\n" +
		"  printf '%s\\n' 'go shim: forced failure'\n" +
		"  exit 3\n" +
		"fi\n" +
		fmt.Sprintf(testEvents, qualityGateShimPassEvents) +
		"    exit 0\n" +
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
	launchQualityGateRunner = func(st *state.StateStore, record qualitygate.RunRecord) (qualityGateRunnerWait, error) {
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

func TestQualityGateEnvScrubsSessionTransport(t *testing.T) {
	t.Setenv(executionunit.DispositionEnv, executionunit.ExecutionUnitSingle)
	t.Setenv("GOFLAGS", "-exec=/bin/sh")
	t.Setenv(state.SessionRotationClaimIDEnv, "21d4da2f-2834-4e1f-9939-184290652f51")
	t.Setenv(state.ParentActionCodexThreadIDEnv, "01a0a889-bf94-7892-bdd8-97df5e7a2e52")
	t.Setenv(state.ParentActionCodexSessionIDEnv, "01a0a889-bf94-7892-bdd8-97df5e7a2e52")
	t.Setenv("GLM_WORKER_HOME", "/keep-this")

	env := qualityGateEnv()
	keptOther := false
	for _, entry := range env {
		if qualityGateSessionTransportEnv(entry) {
			t.Fatalf("session transport envがsubprocess envへ残っています: %s", entry)
		}
		if strings.HasPrefix(entry, "GOFLAGS=") && entry != "GOFLAGS=" {
			t.Fatalf("GOFLAGSが空で再設定されていません: %s", entry)
		}
		if entry == "GLM_WORKER_HOME=/keep-this" {
			keptOther = true
		}
	}
	if !keptOther {
		t.Fatal("session transport以外のenvが失われています")
	}
}

func TestQualityGateParseAcceptsOnlyExactForms(t *testing.T) {
	t.Parallel()
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
			var usage *machinecli.UsageError
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
		instructionSurfaceRunnerFactory,
		nil,
		&bytes.Buffer{},
		&stderr,
	)
	var usage *machinecli.UsageError
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
		form      string
		wantArgv  string
		wantLines int
	}{
		{form: "go-test", wantArgv: "test -json -timeout=45m ./...", wantLines: 6},
		{form: "go-test-race", wantArgv: "test -race -json -timeout=45m ./...", wantLines: 6},
	}
	for _, tc := range cases {
		t.Run(tc.form, func(t *testing.T) {
			useInlineQualityGateRunner(t)
			shimDir, invocationLog := writeQualityGateGoShim(t, filepath.Join(t.TempDir(), "absent-flag"))
			t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("GOFLAGS", "-exec=/bin/sh")
			t.Setenv(executionunit.DispositionEnv, executionunit.ExecutionUnitSingle)
			t.Setenv(state.SessionRotationClaimIDEnv, "21d4da2f-2834-4e1f-9939-184290652f51")
			t.Setenv(state.ParentActionCodexThreadIDEnv, "01a0a889-bf94-7892-bdd8-97df5e7a2e52")
			t.Setenv(state.ParentActionCodexSessionIDEnv, "01a0a889-bf94-7892-bdd8-97df5e7a2e52")
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
			if out.Status != qualitygate.StatusPass || out.Form != tc.form {
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
			if !strings.Contains(string(logData), `"Action":"pass","Package":"example.com/m"`) {
				t.Fatalf("logへgo test JSON出力が保存されていません: %s", logData)
			}

			lines := qualityGateInvocationLines(t, invocationLog)
			if len(lines) != tc.wantLines {
				t.Fatalf("go呼出記録が想定と異なります: %v", lines)
			}
			envLog, err := os.ReadFile(invocationLog + ".env")
			if err != nil || string(envLog) != "goflags:\n" {
				t.Fatalf("toolchain query did not use scrubbed execution environment: %s %v", envLog, err)
			}
			if lines[0] != "argv:"+tc.wantArgv {
				t.Fatalf("固定argvが起動されていません: %s", lines[0])
			}
			if lines[1] != "goflags:" {
				t.Fatalf("GOFLAGSが排除されていません: %s", lines[1])
			}
			if lines[2] != "claim:" || lines[3] != "thread:" || lines[4] != "session:" || lines[5] != "execution-unit:" {
				t.Fatalf("session transport envがsubprocessへ漏れています: %v", lines)
			}
		})
	}
}

func TestQualityGateRejectsSourceMutationDespiteTargetSuccess(t *testing.T) {
	for _, ignored := range []bool{false, true} {
		t.Run(fmt.Sprint(ignored), func(t *testing.T) {
			useInlineQualityGateRunner(t)
			repo := initGitRepo(t)
			if ignored {
				if err := os.WriteFile(filepath.Join(repo, ".git", "info", "exclude"), []byte("untracked-input.txt\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			previousDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(repo); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chdir(previousDir) })
			shimDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(shimDir, "go"), []byte("#!/bin/sh\nprintf 'changed during validation\\n' > untracked-input.txt\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			_, st := newQualityGateEnv(t)
			var output bytes.Buffer
			err = runQualityGate("go-test", st, &output)
			if err == nil {
				t.Fatal("target success must not validate a source mutation")
			}
			entries, err := os.ReadDir(st.Path(qualitygate.RunDirectory))
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, entry := range entries {
				if !qualitygate.ValidRunID(entry.Name()) {
					continue
				}
				record, err := qualitygate.Read(st, entry.Name())
				if err != nil || record.Status != qualitygate.StatusFail || record.ExitSource != state.ValidationExitSourceWrapper {
					t.Fatalf("source mutation must remain a wrapper failure: %#v %v", record, err)
				}
				found = true
			}
			if !found {
				t.Fatal("source mutation lost its run evidence")
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
	if gateFail.ExitCode != 3 || gateFail.Form != "go-test" || !qualitygate.ValidRunID(gateFail.ValidationRunID) {
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
	if envelope.Error.Detail["exit_code"] != float64(3) || envelope.Error.Detail["command"] != "go test -json -timeout=45m ./..." {
		t.Fatalf("error detailが想定と異なります: %+v", envelope.Error.Detail)
	}
	if lines := qualityGateInvocationLines(t, invocationLog); len(lines) == 0 {
		t.Fatal("go processが起動されていません")
	}
}
