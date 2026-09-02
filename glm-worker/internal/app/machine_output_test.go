package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type releasableMachineOutput interface {
	Write([]byte) (int, error)
	release() error
}

func TestSingleShotOutputReleasesExactlyOneJSONObject(t *testing.T) {
	var target bytes.Buffer
	output := newSingleShotOutput(&target)

	if _, err := output.Write([]byte("{\"status\":\"ok\"}\n")); err != nil {
		t.Fatal(err)
	}
	if target.Len() != 0 {
		t.Fatalf("release前に対象stdoutへ出力が漏れています: %q", target.String())
	}
	if err := output.release(); err != nil {
		t.Fatalf("単一JSON objectのreleaseが失敗しました: %v", err)
	}
	if got, want := target.String(), "{\"status\":\"ok\"}\n"; got != want {
		t.Fatalf("release後の出力 = %q want %q", got, want)
	}
}

func TestSingleShotOutputReleaseRejectsContractViolations(t *testing.T) {
	cases := map[string]string{
		"空":                  "",
		"textだけ":             "install smoke: PASS\n",
		"JSON+trailing text": "{\"a\":1}\ninstall smoke: PASS\n",
		"leading text+JSON":  "install smoke: PASS\n{\"a\":1}\n",
		"2つ目のJSON":           "{\"a\":1}\n{\"b\":2}\n",
		"JSONL":              "{\"a\":1}\n{\"b\":2}\n{\"c\":3}\n",
		"JSON array":         "[1,2,3]\n",
		"JSON scalar":        "42\n",
		"JSON null":          "null\n",
	}
	for name, rendered := range cases {
		t.Run(name, func(t *testing.T) {
			assertMachineOutputRejected(t, rendered, func(target *bytes.Buffer) releasableMachineOutput {
				return newSingleShotOutput(target)
			})
		})
	}
}

func TestSingleShotOutputRejectsSubprocessWiredToMachineStdout(t *testing.T) {
	assertSubprocessOutputRejected(t, func(target *bytes.Buffer) releasableMachineOutput {
		return newSingleShotOutput(target)
	})
}

func TestEarlyCommandTrailingTextFailsBeforeRealStdoutRelease(t *testing.T) {
	var target bytes.Buffer
	output := newSingleShotOutput(&target)
	if handled, err := runHelp([]string{"--help"}, output); !handled || err != nil {
		t.Fatalf("runHelp: handled=%v err=%v", handled, err)
	}
	if _, err := output.Write([]byte("trailing plain text\n")); err != nil {
		t.Fatal(err)
	}
	if err := output.release(); err == nil {
		t.Fatal("早期commandのJSON + trailing text出力がreleaseされました")
	}
	if target.Len() != 0 {
		t.Fatalf("契約違反出力が実stdoutへreleaseされました: %q", target.String())
	}
}

func TestSingleShotOutputRejectsSubprocessTextAfterSerializedJSON(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go commandがないためsubprocess再現をskipします: %v", err)
	}

	var target bytes.Buffer
	output := newSingleShotOutput(&target)
	if err := writeJSON(output, map[string]string{"status": "ok"}); err != nil {
		t.Fatal(err)
	}
	leak := exec.Command("go", "version")
	leak.Stdout = output
	if err := leak.Run(); err != nil {
		t.Fatal(err)
	}
	if err := output.release(); err == nil {
		t.Fatalf("JSON後のsubprocess text混入がreleaseされました: %q", target.String())
	}
	if target.Len() != 0 {
		t.Fatalf("混入出力がmachine stdoutへ漏れています: %q", target.String())
	}
}

func TestMachineOutputViolationErrorMapsToProcessErrorKind(t *testing.T) {
	err := buildProcessError(&MachineOutputViolationError{HeldBytes: 12, Cause: errors.New("boom")})
	if err.Kind != errorKindMachineOutputViolation {
		t.Fatalf("kind = %q want %q", err.Kind, errorKindMachineOutputViolation)
	}
	if err.Message == "" {
		t.Fatal("messageが空です")
	}
	detail, ok := err.Detail["held_bytes"].(int)
	if !ok || detail != 12 {
		t.Fatalf("held_bytes = %#v want 12", err.Detail["held_bytes"])
	}
}

func TestStructuredLinesOutputReleasesTypedWarningLines(t *testing.T) {
	var target bytes.Buffer
	diagnostics := newStructuredLinesOutput(&target)
	warning := "{\"type\":\"warning\",\"scope\":\"task_stats\",\"message\":\"観測用mirrorのため続行します\"}\n"
	if _, err := diagnostics.Write([]byte(warning)); err != nil {
		t.Fatal(err)
	}
	if target.Len() != 0 {
		t.Fatalf("release前に対象stderrへ出力が漏れています: %q", target.String())
	}
	if err := diagnostics.release(); err != nil {
		t.Fatalf("typed warning行のreleaseが失敗しました: %v", err)
	}
	if got := target.String(); got != warning {
		t.Fatalf("release後のstderr = %q want %q", got, warning)
	}
}

func TestStructuredLinesOutputReleaseAllowsEmptyHeldOutput(t *testing.T) {
	var target bytes.Buffer
	diagnostics := newStructuredLinesOutput(&target)
	if err := diagnostics.release(); err != nil {
		t.Fatalf("保留なしならreleaseは成功するべきです: %v", err)
	}
	if target.Len() != 0 {
		t.Fatalf("保留出力がないのにstderrへ書き込まれました: %q", target.String())
	}
}

func TestStructuredLinesOutputReleaseRejectsContractViolations(t *testing.T) {
	cases := map[string]string{
		"text行だけ":             "install smoke: FAIL\n",
		"JSON null行":          "null\n",
		"warning行+text行の混在":   "{\"type\":\"warning\"}\ninstall smoke: FAIL\n",
		"warning+nullの混在":     "{\"type\":\"warning\"}\nnull\n",
		"warning+JSON scalar": "{\"type\":\"warning\"}\n42\n",
	}
	for name, rendered := range cases {
		t.Run(name, func(t *testing.T) {
			assertMachineOutputRejected(t, rendered, func(target *bytes.Buffer) releasableMachineOutput {
				return newStructuredLinesOutput(target)
			})
		})
	}
}

func assertMachineOutputRejected(t *testing.T, rendered string, newOutput func(*bytes.Buffer) releasableMachineOutput) {
	t.Helper()
	var target bytes.Buffer
	output := newOutput(&target)
	if _, err := output.Write([]byte(rendered)); err != nil {
		t.Fatal(err)
	}
	err := output.release()
	if err == nil {
		t.Fatalf("契約違反出力がreleaseされました: %q", rendered)
	}
	var violation *MachineOutputViolationError
	if !errors.As(err, &violation) {
		t.Fatalf("release errorがMachineOutputViolationErrorではありません: %v", err)
	}
	if target.Len() != 0 {
		t.Fatalf("違反出力が対象streamへ漏れています: %q", target.String())
	}
}

func TestStructuredLinesOutputRejectsSubprocessWiredToMachineStderrOnSuccessExit(t *testing.T) {
	assertSubprocessOutputRejected(t, func(target *bytes.Buffer) releasableMachineOutput {
		return newStructuredLinesOutput(target)
	})
}

func assertSubprocessOutputRejected(t *testing.T, newOutput func(*bytes.Buffer) releasableMachineOutput) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go commandがないためsubprocess再現をskipします: %v", err)
	}

	var target bytes.Buffer
	output := newOutput(&target)
	leak := exec.Command("go", "version")
	leak.Stdout = output
	leak.Stderr = output
	if err := leak.Run(); err != nil {
		t.Fatal(err)
	}
	releaseErr := output.release()
	if releaseErr == nil {
		t.Fatalf("subprocess textの直結出力がreleaseされました: %q", target.String())
	}
	var violation *MachineOutputViolationError
	if !errors.As(releaseErr, &violation) {
		t.Fatalf("release errorがMachineOutputViolationErrorではありません: %v", releaseErr)
	}
	if target.Len() != 0 {
		t.Fatalf("subprocessのtextがmachine streamへ漏れています: %q", target.String())
	}
	if violation.HeldBytes == 0 {
		t.Fatalf("違反errorが保留byte数を保持していません: %v", violation)
	}
}

func TestStructuredLinesOutputRejectsSubprocessStderrOnFailureExit(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go commandがないためsubprocess再現をskipします: %v", err)
	}

	var target bytes.Buffer
	diagnostics := newStructuredLinesOutput(&target)
	failing := exec.Command("go", "build", "./no-such-package")
	failing.Dir = t.TempDir()
	failing.Stdout = diagnostics
	failing.Stderr = diagnostics
	runErr := failing.Run()
	if runErr == nil {
		t.Fatal("失敗exitの再現commandが成功しました")
	}
	releaseErr := diagnostics.release()
	if releaseErr == nil {
		t.Fatalf("失敗時subprocess stderrの直結がreleaseされました: %q", target.String())
	}
	var violation *MachineOutputViolationError
	if !errors.As(releaseErr, &violation) {
		t.Fatalf("release errorがMachineOutputViolationErrorではありません: %v", releaseErr)
	}
	if target.Len() != 0 {
		t.Fatalf("失敗時subprocessのtextがmachine stderrへ漏れています: %q", target.String())
	}
}

func TestDispatchReleasesTypedStatsWarningThroughMachineStderr(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path("task-stats.json"), []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err = run(
		[]string{"--accept"},
		func() (config.AppConfig, error) { return cfg, nil },
		nil,
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(stdout.String()), "{\"accepted\":false}"; got != want {
		t.Fatalf("stdout = %q want %q", got, want)
	}

	lines := strings.Split(strings.TrimRight(stderr.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("machine stderrへ出力された行数 = %d want 1: %q", len(lines), stderr.String())
	}
	var event struct {
		Type    string `json:"type"`
		Scope   string `json:"scope"`
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatalf("machine stderr行がJSON objectとして解析できません: %v: %q", err, lines[0])
	}
	if event.Type != "warning" || event.Scope != "task_stats" || event.Message == "" || event.Error == "" {
		t.Fatalf("typed warning eventの契約が守られていません: %#v", event)
	}
}

func TestDispatchKeepsMachineStreamsCleanThroughInstallSmokeSuccess(t *testing.T) {
	cfg, _, _, countPath := newInstallSmokeEnv(t)

	var stdout, stderr bytes.Buffer
	err := run(
		[]string{"--install-smoke", "--role", "worker"},
		func() (config.AppConfig, error) { return cfg, nil },
		nil,
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSingleMachineJSONObject(stdout.Bytes()); err != nil {
		t.Fatalf("install smoke成功時のmachine stdoutが単一JSON objectではありません: %v: %q", err, stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("install smoke成功時にmachine stderrへ出力が漏れています: %q", stderr.String())
	}
	if count := smokeInvocationCount(t, countPath); count != 1 {
		t.Fatalf("install smoke実行回数 = %d want 1", count)
	}
}

func TestDispatchKeepsMachineStreamsCleanThroughInstallSmokeFailure(t *testing.T) {
	cfg, _, failFlagPath, countPath := newInstallSmokeEnv(t)
	if err := os.WriteFile(failFlagPath, []byte("fail\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := run(
		[]string{"--install-smoke", "--role", "worker"},
		func() (config.AppConfig, error) { return cfg, nil },
		nil,
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err == nil {
		t.Fatal("install smoke失敗時のerrorが伝搬していません")
	}
	var smokeFail *InstallSmokeError
	if !errors.As(err, &smokeFail) {
		t.Fatalf("install smoke失敗errorがInstallSmokeErrorではありません: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("install smoke失敗時にmachine stdoutへ出力が漏れています: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("install smoke失敗時にsubprocess textがmachine stderrへ漏れています: %q", stderr.String())
	}
	if count := smokeInvocationCount(t, countPath); count != 1 {
		t.Fatalf("install smoke実行回数 = %d want 1", count)
	}
}

func TestStdinReadyControlEventStaysTypedJSONLine(t *testing.T) {
	var stderr bytes.Buffer
	if err := emitStdinReadyControlEvent(&stderr); err != nil {
		t.Fatal(err)
	}
	data := stderr.Bytes()
	if err := validateStructuredJSONLines(data); err != nil {
		t.Fatalf("stdin_ready control eventがstderr境界の構造契約を満たしません: %v: %q", err, stderr.String())
	}
	rendered := stderr.String()
	if !strings.HasSuffix(rendered, "\n") || strings.Count(strings.TrimRight(rendered, "\n"), "\n") != 0 {
		t.Fatalf("stdin_ready control eventが単一行ではありません: %q", rendered)
	}
	var event struct {
		Type  string `json:"type"`
		Event string `json:"event"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(rendered)), &event); err != nil {
		t.Fatalf("stdin_ready control eventがJSON objectとして解析できません: %v: %q", err, rendered)
	}
	if event.Type != "control" || event.Event != "stdin_ready" {
		t.Fatalf("stdin_ready control eventの契約が守られていません: %#v", event)
	}
}

func TestDispatchWithholdsTypedWarningWhenExecuteFailsAfterWarning(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path("task-stats.json"), []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{steps: []fakeStep{{runErr: errors.New("model call failed")}}}

	var stdout, stderr bytes.Buffer
	err = run(
		[]string{"new task request"},
		func() (config.AppConfig, error) { return cfg, nil },
		r.factory(),
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err == nil {
		t.Fatal("Execute errorが伝搬していません")
	}
	if stdout.Len() != 0 {
		t.Fatalf("失敗時にmachine stdoutへ出力が漏れています: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("失敗確定後にtyped warningがmachine stderrへ先行releaseされています: %q", stderr.String())
	}

	if err := WriteProcessError(&stderr, err); err != nil {
		t.Fatal(err)
	}
	if err := validateSingleMachineJSONObject(stderr.Bytes()); err != nil {
		t.Fatalf("失敗時のmachine stderrがprocess error JSON 1件になっていません: %v: %q", err, stderr.String())
	}
	var envelope struct {
		Error struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
		t.Fatalf("process error JSONを解析できません: %v: %q", err, stderr.String())
	}
	if envelope.Error.Kind == "" || envelope.Error.Message == "" {
		t.Fatalf("process error JSONのkind/messageが空です: %q", stderr.String())
	}
}
