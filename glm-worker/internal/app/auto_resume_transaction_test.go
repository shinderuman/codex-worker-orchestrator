package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const testAutoResumeTaskID = "12345678-aaaa-bbbb-cccc-dddddddddddd"

func TestParseAutoResumeCommands(t *testing.T) {
	plan, err := ParseCommand([]string{"--auto-resume-plan"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mode != ModeAutoResumePlan || plan.AutoResume.RunControl != "" {
		t.Fatalf("plan = %#v", plan)
	}

	withRunControl, err := ParseCommand([]string{"--auto-resume-plan", "--run-control", "現在のACTIVE完了後は次taskを開始せず停止"})
	if err != nil {
		t.Fatal(err)
	}
	if withRunControl.Mode != ModeAutoResumePlan || withRunControl.AutoResume.RunControl != "現在のACTIVE完了後は次taskを開始せず停止" {
		t.Fatalf("plan with run control = %#v", withRunControl)
	}

	digest := strings.Repeat("a", 64)
	response, err := ParseCommand([]string{"--auto-resume-response-stdin", "123", "opaque-token", "--sha256", digest})
	if err != nil {
		t.Fatal(err)
	}
	if response.Mode != ModeAutoResumeResponse || response.StdinBytes != 123 || response.AutoResume.Token != "opaque-token" || response.SHA256 != digest {
		t.Fatalf("response = %#v", response)
	}
}

func TestParseAutoResumeCommandsRejectsInvalidInputs(t *testing.T) {
	cases := [][]string{
		{"--auto-resume-plan", "--run-control"},
		{"--auto-resume-plan", "--run-control", ""},
		{"--auto-resume-plan", "--wrong-option", "value"},
		{"--auto-resume-plan", "extra"},
		{"--auto-resume-response-stdin", "0", "token"},
		{"--auto-resume-response-stdin", "1", ""},
		{"--auto-resume-response-stdin", "1", "token", "--sha256", "bad"},
	}
	for _, args := range cases {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("ParseCommand(%q) succeeded", args)
		}
	}
}

func TestAutoResumePlanGeneratesTransactionFromStoppedState(t *testing.T) {
	cfg := newAppConfig(t)
	writeRateLimitedState(t, cfg, time.Now().Add(time.Hour))
	t.Setenv(codexThreadIDEnv, testAppCodexWakeThread)

	var stdout bytes.Buffer
	cmd := Command{Mode: ModeAutoResumePlan, AutoResume: AutoResumeArgs{ParentThreadID: testAppCodexWakeThread}}
	if err := printAutoResumePlan(cmd, cfg, &stdout); err != nil {
		t.Fatal(err)
	}
	var output autoresume.AutoResumeOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("成功出力がmachine JSON 1行として読めません: %v: %q", err, stdout.String())
	}
	wantKey := runner.AutoResumeKeyFor(cfg.RepoShort, testAutoResumeTaskID)
	if output.Status != autoresume.AutoResumeStatusWriteRequired || output.Stage != "create_placeholder" {
		t.Fatalf("output = %#v", output)
	}
	if output.ExpectedAutomationID != wantKey || output.ParentThreadID != testAppCodexWakeThread || output.TaskID != testAutoResumeTaskID {
		t.Fatalf("identity = %#v", output)
	}
	if output.Authority == nil || output.VerifyCommand == nil {
		t.Fatalf("authority and verify command are required: %#v", output)
	}
	if strings.Join(output.VerifyCommand, " ") != "glm-worker --verify-auto-resume "+wantKey+" "+output.ResumeAtRFC3339 {
		t.Fatalf("verify command = %v", output.VerifyCommand)
	}
	if strings.Count(stdout.String(), "\n") != 1 {
		t.Fatalf("出力はJSON 1行だけ: %q", stdout.String())
	}
	lease, err := beginAutoResumeToken(cfg.CodexConfigDir, output.Token)
	if err != nil {
		t.Fatalf("plan token was not persisted: %v", err)
	}
	lease.rollback()
}

func TestAutoResumePlanFailsClosedWhenTaskIsNotRateLimited(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("task.id", testAutoResumeTaskID); err != nil {
		t.Fatal(err)
	}
	t.Setenv(codexThreadIDEnv, testAppCodexWakeThread)

	cmd := Command{Mode: ModeAutoResumePlan, AutoResume: AutoResumeArgs{ParentThreadID: testAppCodexWakeThread}}
	err = printAutoResumePlan(cmd, cfg, &bytes.Buffer{})
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("error = %v", err)
	}
}

func TestAutoResumePlanFailsClosedWhenResetEvidenceIsMissing(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("task.id", testAutoResumeTaskID); err != nil {
		t.Fatal(err)
	}
	if err := st.EnterStop(state.ResumeCheckpoint{
		Model:    "opus",
		Stage:    state.ResumeStageWorker,
		Role:     state.WorkerRole,
		Prompt:   "prompt",
		Request:  "request",
		StopKind: state.ResumeStopRateLimited,
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(codexThreadIDEnv, testAppCodexWakeThread)

	cmd := Command{Mode: ModeAutoResumePlan, AutoResume: AutoResumeArgs{ParentThreadID: testAppCodexWakeThread}}
	err = printAutoResumePlan(cmd, cfg, &bytes.Buffer{})
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("error = %v", err)
	}
}

func TestAutoResumeResponseRequiresTrustedTokenState(t *testing.T) {
	cmd := Command{
		Mode:       ModeAutoResumeResponse,
		Payload:    `{}`,
		AutoResume: AutoResumeArgs{Token: "invalid-token"},
	}
	var stdout bytes.Buffer
	err := executeRuntimeControl(cmd, config.AppConfig{CodexConfigDir: t.TempDir()}, &stdout)
	if err == nil {
		t.Fatal("untrusted auto-resume response was accepted")
	}
	if stdout.Len() != 0 {
		t.Fatalf("untrusted token produced output: %q", stdout.String())
	}
}

func TestAutoResumeResponseRejectsCrossThreadContext(t *testing.T) {
	cfg := newAppConfig(t)
	writeRateLimitedState(t, cfg, time.Now().Add(time.Hour))
	t.Setenv(codexThreadIDEnv, testAppCodexWakeThread)
	plan := testAppAutoResumePlan(t, cfg)

	t.Setenv(codexThreadIDEnv, testAppOtherThread)
	cmd := Command{Mode: ModeAutoResumeResponse, Payload: `{}`, AutoResume: AutoResumeArgs{Token: plan.Token}}
	if err := printAutoResumeResponse(cmd, cfg, &bytes.Buffer{}); err == nil {
		t.Fatal("cross-thread auto-resume response was accepted")
	}
	t.Setenv(codexThreadIDEnv, testAppCodexWakeThread)
	lease, err := beginAutoResumeToken(cfg.CodexConfigDir, plan.Token)
	if err != nil {
		t.Fatalf("rejected cross-thread response consumed token: %v", err)
	}
	lease.rollback()
}

func TestAutoResumeResponseAdvancesCreateStage(t *testing.T) {
	cfg := newAppConfig(t)
	writeRateLimitedState(t, cfg, time.Now().Add(time.Hour))
	t.Setenv(codexThreadIDEnv, testAppCodexWakeThread)
	plan := testAppAutoResumePlan(t, cfg)

	cmd := Command{
		Mode:       ModeAutoResumeResponse,
		Payload:    testAppCreateResponse(t, plan.ExpectedAutomationID),
		AutoResume: AutoResumeArgs{Token: plan.Token},
	}
	var stdout bytes.Buffer
	if err := printAutoResumeResponse(cmd, cfg, &stdout); err != nil {
		t.Fatal(err)
	}
	var output autoresume.AutoResumeOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("成功出力がmachine JSON 1行として読めません: %v: %q", err, stdout.String())
	}
	if output.Status != autoresume.AutoResumeStatusWriteRequired || output.Stage != "update_one_shot" || output.Write == nil {
		t.Fatalf("output = %#v", output)
	}
	if output.Write.AutomationID != plan.ExpectedAutomationID || output.Write.Status != "ACTIVE" {
		t.Fatalf("update write = %#v", output.Write)
	}
	if !strings.Contains(output.Write.RRule, "DTSTART:") || !strings.Contains(output.Write.RRule, "RRULE:FREQ=DAILY;COUNT=1") {
		t.Fatalf("update rrule = %q", output.Write.RRule)
	}
	if output.Token == "" || output.Token == plan.Token {
		t.Fatalf("successor token = %q", output.Token)
	}
	if err := printAutoResumeResponse(cmd, cfg, &bytes.Buffer{}); err == nil {
		t.Fatal("consumed auto-resume token was replayed")
	}
}

func TestExecuteVerifyAutoResumeCreatesNoStateDirectory(t *testing.T) {
	cfg := newAppConfig(t)
	cfg.StateBase = filepath.Join(t.TempDir(), "absent-state")

	var out bytes.Buffer
	err := Execute(Command{
		Mode: ModeVerifyAutoResume,
		Verify: VerifyArgs{
			Key:      "glm-worker-resume-nonexist-00000000",
			RFC3339:  "2026-08-12T20:01:20+09:00",
			ThreadID: "019f88f8-0e70-7d53-a2a3-f0c61666827c",
		},
	}, cfg, nil, &out, &bytes.Buffer{})

	var verification *VerificationError
	if !errors.As(err, &verification) {
		t.Fatalf("verification fail typed errorを期待: %v", err)
	}
	if _, statErr := os.Stat(cfg.StateBase); !os.IsNotExist(statErr) {
		t.Fatalf("read-only verify created session state: %v", statErr)
	}
}

func writeRateLimitedState(t *testing.T, cfg config.AppConfig, resetAt time.Time) {
	t.Helper()
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("task.id", testAutoResumeTaskID); err != nil {
		t.Fatal(err)
	}
	if err := st.EnterStop(state.ResumeCheckpoint{
		Model:          "opus",
		Stage:          state.ResumeStageWorker,
		Role:           state.WorkerRole,
		Prompt:         "prompt",
		Request:        "request",
		StopKind:       state.ResumeStopRateLimited,
		ResetAtRFC3339: resetAt.Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
}

func testAppAutoResumePlan(t *testing.T, cfg config.AppConfig) autoresume.AutoResumeOutput {
	t.Helper()
	var stdout bytes.Buffer
	cmd := Command{Mode: ModeAutoResumePlan, AutoResume: AutoResumeArgs{ParentThreadID: testAppCodexWakeThread}}
	if err := printAutoResumePlan(cmd, cfg, &stdout); err != nil {
		t.Fatalf("plan failed: %v", err)
	}
	var output autoresume.AutoResumeOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("plan outputはmachine JSON 1行: %v: %q", err, stdout.String())
	}
	return output
}
