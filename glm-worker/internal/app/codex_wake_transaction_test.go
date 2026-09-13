package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexlimit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

type codexWakeErrorWriter struct {
	err error
}

const (
	testAppCodexWakeThread = "01a03a9e-10a0-7f11-801c-f04e5dbd5490"
	testAppOtherThread     = "01a05f46-47aa-77d2-912c-0d6b078cb856"
)

func (w codexWakeErrorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func TestParseCodexWakeCommands(t *testing.T) {
	plan, err := ParseCommand([]string{"--codex-wake-plan", testAppCodexWakeThread})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mode != ModeCodexWakePlan || plan.CodexWake.ThreadID != testAppCodexWakeThread || plan.CodexWake.AutomationID != "" {
		t.Fatalf("plan = %#v", plan)
	}

	key := autoresume.CodexWakeAutomationKey(testAppCodexWakeThread)
	wake, err := ParseCommand([]string{"--codex-wake-plan", testAppCodexWakeThread, "--fired-automation-id", key})
	if err != nil {
		t.Fatal(err)
	}
	if wake.Mode != ModeCodexWakePlan || wake.CodexWake.ThreadID != testAppCodexWakeThread || wake.CodexWake.AutomationID != key {
		t.Fatalf("wake = %#v", wake)
	}

	digest := strings.Repeat("a", 64)
	response, err := ParseCommand([]string{"--codex-wake-response-stdin", "123", "opaque-token", "--sha256", digest})
	if err != nil {
		t.Fatal(err)
	}
	if response.Mode != ModeCodexWakeResponse || response.StdinBytes != 123 || response.CodexWake.Token != "opaque-token" || response.SHA256 != digest {
		t.Fatalf("response = %#v", response)
	}
}

func TestParseCodexWakeCommandsRejectsInvalidInputs(t *testing.T) {
	cases := [][]string{
		{"--codex-wake-plan"},
		{"--codex-wake-plan", "not-a-thread"},
		{"--codex-wake-plan", testAppCodexWakeThread, "--fired-automation-id", ""},
		{"--codex-wake-plan", testAppCodexWakeThread, "--wrong-option", "id"},
		{"--codex-wake-response-stdin", "0", "token"},
		{"--codex-wake-response-stdin", "1", ""},
		{"--codex-wake-response-stdin", "1", "token", "--sha256", "bad"},
	}
	for _, args := range cases {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("ParseCommand(%q) succeeded", args)
		}
	}
}

func TestCodexWakeResponseRequiresTrustedTokenState(t *testing.T) {
	cmd := Command{
		Mode:      ModeCodexWakeResponse,
		Payload:   `{}`,
		CodexWake: CodexWakeArgs{Token: "invalid-token"},
	}
	var stdout bytes.Buffer
	err := executeRuntimeControl(cmd, config.AppConfig{CodexConfigDir: t.TempDir()}, &stdout)
	if err == nil {
		t.Fatal("untrusted wake response was accepted")
	}
	if stdout.Len() != 0 {
		t.Fatalf("untrusted token produced output: %q", stdout.String())
	}
}

func TestCodexWakeInvocationRejectsCrossThreadContext(t *testing.T) {
	now := time.Date(2026, 9, 11, 2, 0, 0, 0, time.UTC)
	reset := now.Add(time.Hour)
	resetEpoch := reset.Unix()
	resetRFC3339 := reset.Format(time.RFC3339)
	snapshot := codexlimit.Snapshot{FiveHour: codexlimit.Window{
		ResetsAt:        &resetEpoch,
		ResetsAtRFC3339: &resetRFC3339,
	}}
	key := autoresume.CodexWakeAutomationKey(testAppCodexWakeThread)
	plan, err := autoresume.BuildCodexWakeTransaction(snapshot, testAppCodexWakeThread, key, t.TempDir(), now)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.AppConfig{CodexConfigDir: t.TempDir()}
	if err := persistCodexWakeToken(cfg.CodexConfigDir, plan.Token); err != nil {
		t.Fatal(err)
	}

	t.Setenv(codexThreadIDEnv, testAppOtherThread)
	cmd := Command{Mode: ModeCodexWakeResponse, Payload: `{}`, CodexWake: CodexWakeArgs{Token: plan.Token}}
	if err := printCodexWakeResponse(cmd, cfg, io.Discard); err == nil {
		t.Fatal("cross-thread wake response was accepted")
	}
	lease, err := beginCodexWakeToken(cfg.CodexConfigDir, plan.Token)
	if err != nil {
		t.Fatalf("rejected cross-thread response consumed token: %v", err)
	}
	lease.rollback()
}

func TestCodexWakeRegistrationResponseDoesNotBindParentThread(t *testing.T) {
	now := time.Date(2026, 9, 11, 2, 0, 0, 0, time.UTC)
	reset := now.Add(time.Hour)
	resetEpoch := reset.Unix()
	resetRFC3339 := reset.Format(time.RFC3339)
	snapshot := codexlimit.Snapshot{FiveHour: codexlimit.Window{
		ResetsAt:        &resetEpoch,
		ResetsAtRFC3339: &resetRFC3339,
	}}
	plan, err := autoresume.BuildCodexWakeTransaction(snapshot, testAppCodexWakeThread, "", t.TempDir()+"/missing", now)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.AppConfig{CodexConfigDir: t.TempDir()}
	if err := persistCodexWakeToken(cfg.CodexConfigDir, plan.Token); err != nil {
		t.Fatal(err)
	}

	t.Setenv(codexThreadIDEnv, testAppOtherThread)
	cmd := Command{Mode: ModeCodexWakeResponse, Payload: `{}`, CodexWake: CodexWakeArgs{Token: plan.Token}}
	var stdout bytes.Buffer
	if err := printCodexWakeResponse(cmd, cfg, &stdout); err != nil {
		t.Fatal(err)
	}
	var output autoresume.CodexWakeOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Status != autoresume.CodexWakeStatusFailed {
		t.Fatalf("output = %#v", output)
	}
}

func TestCodexWakeResponseRejectsForgedTokenWithRecomputedChecksum(t *testing.T) {
	plan := testAppCodexWakePlan(t)
	cfg := config.AppConfig{CodexConfigDir: t.TempDir()}
	if err := persistCodexWakeToken(cfg.CodexConfigDir, plan.Token); err != nil {
		t.Fatal(err)
	}
	forged := forgeCodexWakeToken(t, plan.Token)
	cmd := Command{Mode: ModeCodexWakeResponse, Payload: `{}`, CodexWake: CodexWakeArgs{Token: forged}}
	if err := printCodexWakeResponse(cmd, cfg, io.Discard); err == nil {
		t.Fatal("forged token with recomputed checksum was accepted")
	}
}

func TestCodexWakeResponseConsumesTokenOnce(t *testing.T) {
	plan := testAppCodexWakePlan(t)
	cfg := config.AppConfig{CodexConfigDir: t.TempDir()}
	if err := persistCodexWakeToken(cfg.CodexConfigDir, plan.Token); err != nil {
		t.Fatal(err)
	}
	cmd := Command{Mode: ModeCodexWakeResponse, Payload: `{}`, CodexWake: CodexWakeArgs{Token: plan.Token}}
	var stdout bytes.Buffer
	if err := printCodexWakeResponse(cmd, cfg, &stdout); err != nil {
		t.Fatal(err)
	}
	if err := printCodexWakeResponse(cmd, cfg, io.Discard); err == nil {
		t.Fatal("consumed wake token was replayed")
	}
}

func TestCodexWakeResponseOutputFailureKeepsInputTokenRetryable(t *testing.T) {
	plan := testAppCodexWakePlan(t)
	cfg := config.AppConfig{CodexConfigDir: t.TempDir()}
	if err := persistCodexWakeToken(cfg.CodexConfigDir, plan.Token); err != nil {
		t.Fatal(err)
	}
	cmd := Command{
		Mode:      ModeCodexWakeResponse,
		Payload:   testAppCodexWakeCreateResponse(t, plan.ExpectedAutomationID),
		CodexWake: CodexWakeArgs{Token: plan.Token},
	}
	writeErr := errors.New("injected response output failure")
	if err := printCodexWakeResponse(cmd, cfg, codexWakeErrorWriter{err: writeErr}); !errors.Is(err, writeErr) {
		t.Fatalf("output failure = %v, want %v", err, writeErr)
	}

	var stdout bytes.Buffer
	if err := printCodexWakeResponse(cmd, cfg, &stdout); err != nil {
		t.Fatalf("retry with original token failed: %v", err)
	}
	var output autoresume.CodexWakeOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Status != autoresume.CodexWakeStatusWriteRequired || output.Token == "" {
		t.Fatalf("retry output = %#v", output)
	}
	lease, err := beginCodexWakeToken(cfg.CodexConfigDir, output.Token)
	if err != nil {
		t.Fatalf("successor token was not persisted after successful retry: %v", err)
	}
	lease.rollback()
	if err := printCodexWakeResponse(cmd, cfg, io.Discard); err == nil {
		t.Fatal("input token remained replayable after successful response delivery")
	}
}

func TestCodexWakeResponseRecoversInterruptedTokenClaim(t *testing.T) {
	plan := testAppCodexWakePlan(t)
	cfg := config.AppConfig{CodexConfigDir: t.TempDir()}
	if err := persistCodexWakeToken(cfg.CodexConfigDir, plan.Token); err != nil {
		t.Fatal(err)
	}
	lease, err := beginCodexWakeToken(cfg.CodexConfigDir, plan.Token)
	if err != nil {
		t.Fatal(err)
	}
	lease.releaseLock()

	cmd := Command{Mode: ModeCodexWakeResponse, Payload: `{}`, CodexWake: CodexWakeArgs{Token: plan.Token}}
	if err := printCodexWakeResponse(cmd, cfg, io.Discard); err != nil {
		t.Fatalf("orphaned inflight token was not recovered: %v", err)
	}
	if err := printCodexWakeResponse(cmd, cfg, io.Discard); err == nil {
		t.Fatal("recovered token was replayable after successful response delivery")
	}
}

func testAppCodexWakePlan(t *testing.T) autoresume.CodexWakeOutput {
	t.Helper()
	now := time.Date(2026, 9, 11, 2, 0, 0, 0, time.UTC)
	reset := now.Add(time.Hour)
	resetEpoch := reset.Unix()
	resetRFC3339 := reset.Format(time.RFC3339)
	snapshot := codexlimit.Snapshot{FiveHour: codexlimit.Window{ResetsAt: &resetEpoch, ResetsAtRFC3339: &resetRFC3339}}
	plan, err := autoresume.BuildCodexWakeTransaction(snapshot, testAppCodexWakeThread, "", t.TempDir()+"/missing", now)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func testAppCodexWakeCreateResponse(t *testing.T, automationID string) string {
	t.Helper()
	facts, err := json.Marshal(map[string]string{
		"automation_id": automationID,
		"mode":          "create",
		"status":        "PAUSED",
		"message":       "created successfully",
	})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := json.Marshal(map[string]any{
		"isError": false,
		"content": []map[string]string{{"type": "text", "text": string(facts)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(envelope)
}

func forgeCodexWakeToken(t *testing.T, token string) string {
	t.Helper()
	encoded, _, ok := strings.Cut(token, ".")
	if !ok {
		t.Fatal("token has no checksum")
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatal(err)
	}
	value["attempt"] = float64(2)
	payload, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + hex.EncodeToString(sum[:])
}
