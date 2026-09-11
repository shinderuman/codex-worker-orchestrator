package app

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexlimit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

const (
	testAppCodexWakeThread = "01a03a9e-10a0-7f11-801c-f04e5dbd5490"
	testAppOtherThread     = "01a05f46-47aa-77d2-912c-0d6b078cb856"
)

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

func TestCodexWakeResponseDispatchIsStateless(t *testing.T) {
	cmd := Command{
		Mode:      ModeCodexWakeResponse,
		Payload:   `{}`,
		CodexWake: CodexWakeArgs{Token: "invalid-token"},
	}
	var stdout bytes.Buffer
	handled, err := executeStateless(cmd, config.AppConfig{CodexConfigDir: t.TempDir()}, &stdout)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("Codex wake response command was not dispatched")
	}
	var output autoresume.CodexWakeOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Status != autoresume.CodexWakeStatusFailed {
		t.Fatalf("output = %#v", output)
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

	t.Setenv(codexThreadIDEnv, testAppOtherThread)
	cmd := Command{Mode: ModeCodexWakeResponse, Payload: `{}`, CodexWake: CodexWakeArgs{Token: plan.Token}}
	if err := printCodexWakeResponse(cmd, config.AppConfig{CodexConfigDir: t.TempDir()}, io.Discard); err == nil {
		t.Fatal("cross-thread wake response was accepted")
	}
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

	t.Setenv(codexThreadIDEnv, testAppOtherThread)
	cmd := Command{Mode: ModeCodexWakeResponse, Payload: `{}`, CodexWake: CodexWakeArgs{Token: plan.Token}}
	var stdout bytes.Buffer
	if err := printCodexWakeResponse(cmd, config.AppConfig{CodexConfigDir: t.TempDir()}, &stdout); err != nil {
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
