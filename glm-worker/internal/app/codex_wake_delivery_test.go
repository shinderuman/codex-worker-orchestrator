package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

type codexWakeFullErrorWriter struct {
	err error
}

func (w codexWakeFullErrorWriter) Write(p []byte) (int, error) {
	return len(p), w.err
}

func TestCodexWakeResponseCompleteWriteDoesNotRestoreInputToken(t *testing.T) {
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
	writeErr := errors.New("writer reported an error after accepting the full response")
	if err := printCodexWakeResponse(cmd, cfg, codexWakeFullErrorWriter{err: writeErr}); err != nil {
		t.Fatalf("complete response delivery was treated as failed: %v", err)
	}
	if err := printCodexWakeResponse(cmd, cfg, &bytes.Buffer{}); err == nil {
		t.Fatal("fully delivered wake response restored the input token")
	}
}

func TestCodexWakeResponseCommitFailureCannotReplayDeliveredToken(t *testing.T) {
	plan := testAppCodexWakePlan(t)
	cfg := config.AppConfig{CodexConfigDir: t.TempDir()}
	if err := persistCodexWakeToken(cfg.CodexConfigDir, plan.Token); err != nil {
		t.Fatal(err)
	}
	cmd := Command{Mode: ModeCodexWakeResponse, Payload: `{}`, CodexWake: CodexWakeArgs{Token: plan.Token}}

	removeErr := errors.New("injected token cleanup failure")
	originalRemove := removeCodexWakeLeaseFile
	removeCodexWakeLeaseFile = func(string) error { return removeErr }
	t.Cleanup(func() { removeCodexWakeLeaseFile = originalRemove })

	var stdout bytes.Buffer
	if err := printCodexWakeResponse(cmd, cfg, &stdout); !errors.Is(err, removeErr) {
		t.Fatalf("commit failure = %v, want %v", err, removeErr)
	}
	if stdout.Len() == 0 {
		t.Fatal("commit failure occurred before response delivery")
	}
	if err := printCodexWakeResponse(cmd, cfg, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "delivery is unresolved") {
		t.Fatalf("delivered token was replayable after cleanup failure: %v", err)
	}
}

func TestCodexWakeResponseDoesNotRecoverInterruptedDeliveryPhase(t *testing.T) {
	plan := testAppCodexWakePlan(t)
	cfg := config.AppConfig{CodexConfigDir: t.TempDir()}
	if err := persistCodexWakeToken(cfg.CodexConfigDir, plan.Token); err != nil {
		t.Fatal(err)
	}
	lease, err := beginCodexWakeToken(cfg.CodexConfigDir, plan.Token)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.markDelivering(); err != nil {
		t.Fatal(err)
	}
	lease.releaseLock()

	cmd := Command{Mode: ModeCodexWakeResponse, Payload: `{}`, CodexWake: CodexWakeArgs{Token: plan.Token}}
	if err := printCodexWakeResponse(cmd, cfg, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "delivery is unresolved") {
		t.Fatalf("interrupted delivery phase was replayed: %v", err)
	}
}
