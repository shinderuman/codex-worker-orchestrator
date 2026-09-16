package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
)

func TestAutoResumePlanExposesBoundedAbortCommand(t *testing.T) {
	cfg := newAppConfig(t)
	writeRateLimitedState(t, cfg, time.Now().Add(time.Hour))
	t.Setenv(codexThreadIDEnv, testAppCodexWakeThread)

	var stdout bytes.Buffer
	cmd := Command{Mode: ModeAutoResumePlan, AutoResume: AutoResumeArgs{ParentThreadID: testAppCodexWakeThread}}
	if err := printAutoResumePlan(cmd, cfg, &stdout); err != nil {
		t.Fatal(err)
	}
	var output struct {
		autoresume.AutoResumeOutput
		AbortCommand []string `json:"abort_command"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Status != autoresume.AutoResumeStatusWriteRequired || output.Token == "" {
		t.Fatalf("plan = %#v", output.AutoResumeOutput)
	}
	want := []string{"glm-worker", "--auto-resume-abort", output.Token, autoresume.AutoResumeAbortAutomationUpdateUnavailable}
	if strings.Join(output.AbortCommand, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("abort command = %#v want %#v", output.AbortCommand, want)
	}
}

func TestAutoResumeAbortConsumesTrustedTransaction(t *testing.T) {
	cfg := newAppConfig(t)
	writeRateLimitedState(t, cfg, time.Now().Add(time.Hour))
	t.Setenv(codexThreadIDEnv, testAppCodexWakeThread)
	plan := testAppAutoResumePlan(t, cfg)

	cmd, err := ParseCommand([]string{"--auto-resume-abort", plan.Token, autoresume.AutoResumeAbortAutomationUpdateUnavailable})
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := executeRuntimeControl(cmd, cfg, &stdout); err != nil {
		t.Fatal(err)
	}
	var output autoresume.AutoResumeOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Status != autoresume.AutoResumeStatusFailed || output.Token != "" || output.Write != nil || output.Cleanup != nil {
		t.Fatalf("abort output = %#v", output)
	}
	if !strings.Contains(output.Reason, "automation_update capability is unavailable") {
		t.Fatalf("abort reason = %q", output.Reason)
	}
	if err := executeRuntimeControl(cmd, cfg, &bytes.Buffer{}); err == nil {
		t.Fatal("consumed abort token was replayed")
	}
}

func TestParseAutoResumeAbortRejectsUnknownReason(t *testing.T) {
	if _, err := ParseCommand([]string{"--auto-resume-abort", "token", "explore_other_threads"}); err == nil {
		t.Fatal("unknown abort reason was accepted")
	}
}
