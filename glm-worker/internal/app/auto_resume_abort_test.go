package app

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func setAutoResumeFallbackDecision(t *testing.T, decision autoresume.AutoResumeFallbackDecision) {
	t.Helper()
	oldEvaluate := autoResumeFallbackEvaluate
	autoResumeFallbackEvaluate = func(token, _, _ string, _ autoresume.DBReader) (autoresume.AutoResumeFallbackPlan, autoresume.AutoResumeFallbackDecision, error) {
		plan, err := autoresume.AutoResumeFallbackPlanFromToken(token)
		return plan, decision, err
	}
	t.Cleanup(func() {
		autoResumeFallbackEvaluate = oldEvaluate
	})
}

func TestAutoResumePlanExposesBoundedFallbackCommand(t *testing.T) {
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
		FallbackCommand []string `json:"fallback_command"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Status != autoresume.AutoResumeStatusWriteRequired || output.Token == "" {
		t.Fatalf("plan = %#v", output.AutoResumeOutput)
	}
	want := []string{"glm-worker", "--auto-resume-fallback", output.Token}
	if strings.Join(output.FallbackCommand, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("fallback command = %#v want %#v", output.FallbackCommand, want)
	}
}

func TestAutoResumeFallbackWaitsToMachineBoundaryAndResumes(t *testing.T) {
	setAutoResumeFallbackDecision(t, autoresume.AutoResumeFallbackLocalWait)
	cfg := newAppConfig(t)
	resetAt := time.Now().Add(time.Hour).Truncate(time.Second)
	writeRateLimitedState(t, cfg, resetAt)
	t.Setenv(codexThreadIDEnv, testAppCodexWakeThread)
	plan := testAppAutoResumePlan(t, cfg)

	oldWait := autoResumeFallbackWaitUntil
	oldRun := autoResumeFallbackRunResume
	defer func() {
		autoResumeFallbackWaitUntil = oldWait
		autoResumeFallbackRunResume = oldRun
	}()
	var waitedUntil time.Time
	autoResumeFallbackWaitUntil = func(target time.Time) {
		waitedUntil = target
	}
	resumeCalls := 0
	autoResumeFallbackRunResume = func(got config.AppConfig, _ io.Writer) (bool, error) {
		resumeCalls++
		if got.RepoRoot != cfg.RepoRoot {
			t.Fatalf("resume repo = %q want %q", got.RepoRoot, cfg.RepoRoot)
		}
		return true, nil
	}

	cmd, err := ParseCommand([]string{"--auto-resume-fallback", plan.Token})
	if err != nil {
		t.Fatal(err)
	}
	if err := executeRuntimeControl(cmd, cfg, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	wantWait, err := time.Parse(time.RFC3339, plan.ResetAtRFC3339)
	if err != nil {
		t.Fatal(err)
	}
	if !waitedUntil.Equal(wantWait) {
		t.Fatalf("wait target = %s want exact provider reset %s", waitedUntil, wantWait)
	}
	if resumeCalls != 1 {
		t.Fatalf("resume calls = %d want 1", resumeCalls)
	}
	if err := executeRuntimeControl(cmd, cfg, &bytes.Buffer{}); err == nil {
		t.Fatal("consumed fallback token was replayed")
	}
}

func TestAutoResumeFallbackTrustsExistingExternalWakeWithoutLocalResume(t *testing.T) {
	setAutoResumeFallbackDecision(t, autoresume.AutoResumeFallbackExternalWake)
	cfg := newAppConfig(t)
	writeRateLimitedState(t, cfg, time.Now().Add(time.Hour).Truncate(time.Second))
	t.Setenv(codexThreadIDEnv, testAppCodexWakeThread)
	plan := testAppAutoResumePlan(t, cfg)

	oldWait := autoResumeFallbackWaitUntil
	oldRun := autoResumeFallbackRunResume
	defer func() {
		autoResumeFallbackWaitUntil = oldWait
		autoResumeFallbackRunResume = oldRun
	}()
	waitCalls := 0
	autoResumeFallbackWaitUntil = func(time.Time) {
		waitCalls++
	}
	resumeCalls := 0
	autoResumeFallbackRunResume = func(config.AppConfig, io.Writer) (bool, error) {
		resumeCalls++
		return true, nil
	}

	cmd, err := ParseCommand([]string{"--auto-resume-fallback", plan.Token})
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := executeRuntimeControl(cmd, cfg, &stdout); err != nil {
		t.Fatal(err)
	}
	if waitCalls != 0 || resumeCalls != 0 {
		t.Fatalf("local fallback ran with active external wake: wait=%d resume=%d", waitCalls, resumeCalls)
	}
	var output autoResumeFallbackExternalOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Status != "external_wake_active" || output.TaskID != plan.TaskID || output.ExpectedAutomationID == "" {
		t.Fatalf("external output = %#v", output)
	}
	if err := executeRuntimeControl(cmd, cfg, &bytes.Buffer{}); err == nil {
		t.Fatal("consumed external-wake fallback token was replayed")
	}
}

func TestAutoResumeFallbackRejectsStateChangeDuringWait(t *testing.T) {
	setAutoResumeFallbackDecision(t, autoresume.AutoResumeFallbackLocalWait)
	cfg := newAppConfig(t)
	resetAt := time.Now().Add(time.Hour).Truncate(time.Second)
	writeRateLimitedState(t, cfg, resetAt)
	t.Setenv(codexThreadIDEnv, testAppCodexWakeThread)
	plan := testAppAutoResumePlan(t, cfg)
	st := state.AttachStateStore(cfg)

	oldWait := autoResumeFallbackWaitUntil
	oldRun := autoResumeFallbackRunResume
	defer func() {
		autoResumeFallbackWaitUntil = oldWait
		autoResumeFallbackRunResume = oldRun
	}()
	autoResumeFallbackWaitUntil = func(time.Time) {
		checkpoint, err := st.LoadResumeCheckpoint()
		if err != nil {
			t.Fatal(err)
		}
		checkpoint.ResetAtRFC3339 = resetAt.Add(time.Hour).Format(time.RFC3339)
		if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
			t.Fatal(err)
		}
	}
	resumeCalls := 0
	autoResumeFallbackRunResume = func(config.AppConfig, io.Writer) (bool, error) {
		resumeCalls++
		return true, nil
	}

	cmd, err := ParseCommand([]string{"--auto-resume-fallback", plan.Token})
	if err != nil {
		t.Fatal(err)
	}
	err = executeRuntimeControl(cmd, cfg, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "reset boundary changed") {
		t.Fatalf("state-change error = %v", err)
	}
	if resumeCalls != 0 {
		t.Fatalf("resume calls = %d want 0", resumeCalls)
	}
	if err := executeRuntimeControl(cmd, cfg, &bytes.Buffer{}); err == nil {
		t.Fatal("stale fallback token was replayed")
	}
}

func TestAutoResumeFallbackRejectsTaskStatusChangeDuringWait(t *testing.T) {
	setAutoResumeFallbackDecision(t, autoresume.AutoResumeFallbackLocalWait)
	cfg := newAppConfig(t)
	resetAt := time.Now().Add(time.Hour).Truncate(time.Second)
	writeRateLimitedState(t, cfg, resetAt)
	t.Setenv(codexThreadIDEnv, testAppCodexWakeThread)
	plan := testAppAutoResumePlan(t, cfg)
	st := state.AttachStateStore(cfg)

	oldWait := autoResumeFallbackWaitUntil
	oldRun := autoResumeFallbackRunResume
	defer func() {
		autoResumeFallbackWaitUntil = oldWait
		autoResumeFallbackRunResume = oldRun
	}()
	autoResumeFallbackWaitUntil = func(time.Time) {
		if err := st.SetTaskStatus(state.TaskStatusInterrupted); err != nil {
			t.Fatal(err)
		}
	}
	resumeCalls := 0
	autoResumeFallbackRunResume = func(config.AppConfig, io.Writer) (bool, error) {
		resumeCalls++
		return true, nil
	}

	cmd, err := ParseCommand([]string{"--auto-resume-fallback", plan.Token})
	if err != nil {
		t.Fatal(err)
	}
	err = executeRuntimeControl(cmd, cfg, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "no longer rate-limited") {
		t.Fatalf("task-status-change error = %v", err)
	}
	if resumeCalls != 0 {
		t.Fatalf("resume calls = %d want 0", resumeCalls)
	}
	if err := executeRuntimeControl(cmd, cfg, &bytes.Buffer{}); err == nil {
		t.Fatal("changed-state fallback token was replayed")
	}
}

func TestAutoResumeFallbackRejectsCrossThreadWithoutConsumingToken(t *testing.T) {
	cfg := newAppConfig(t)
	writeRateLimitedState(t, cfg, time.Now().Add(time.Hour))
	t.Setenv(codexThreadIDEnv, testAppCodexWakeThread)
	plan := testAppAutoResumePlan(t, cfg)

	t.Setenv(codexThreadIDEnv, testAppOtherThread)
	cmd, err := ParseCommand([]string{"--auto-resume-fallback", plan.Token})
	if err != nil {
		t.Fatal(err)
	}
	if err := executeRuntimeControl(cmd, cfg, &bytes.Buffer{}); err == nil {
		t.Fatal("cross-thread fallback was accepted")
	}

	t.Setenv(codexThreadIDEnv, testAppCodexWakeThread)
	lease, err := beginAutoResumeToken(cfg.CodexConfigDir, plan.Token)
	if err != nil {
		t.Fatalf("rejected cross-thread fallback consumed token: %v", err)
	}
	lease.rollback()
}

func TestParseAutoResumeFallbackRejectsMissingToken(t *testing.T) {
	if _, err := ParseCommand([]string{"--auto-resume-fallback"}); err == nil {
		t.Fatal("missing fallback token was accepted")
	}
}
