package app

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestBindCurrentCodexThreadIdentity(t *testing.T) {
	threadID := "01a05f46-47aa-77d2-912c-0d6b078cb856"
	t.Setenv(codexThreadIDEnv, threadID)

	verify := Command{Mode: ModeVerifyAutoResume}
	if err := bindCurrentCodexThreadIdentity(&verify); err != nil {
		t.Fatal(err)
	}
	if verify.Verify.ThreadID != threadID {
		t.Fatalf("verify thread ID = %q", verify.Verify.ThreadID)
	}

	coalesce := Command{Mode: ModeCheckWakeCoalesce}
	if err := bindCurrentCodexThreadIdentity(&coalesce); err != nil {
		t.Fatal(err)
	}
	if coalesce.Coalesce.ParentThreadID != threadID {
		t.Fatalf("coalesce thread ID = %q", coalesce.Coalesce.ParentThreadID)
	}
}

func TestBindCurrentCodexThreadIdentityRejectsMissingOrInvalidEnvironment(t *testing.T) {
	for _, value := range []string{"", "not-a-thread-id"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv(codexThreadIDEnv, value)
			cmd := Command{Mode: ModeVerifyAutoResume}
			err := bindCurrentCodexThreadIdentity(&cmd)
			var notFound *NotFoundError
			if !errors.As(err, &notFound) {
				t.Fatalf("error = %v", err)
			}
			if cmd.Verify.ThreadID != "" {
				t.Fatalf("invalid environment was bound: %q", cmd.Verify.ThreadID)
			}
		})
	}
}

func TestRunAutoResumeIdentityFailureStopsBeforeConfig(t *testing.T) {
	t.Setenv(codexThreadIDEnv, "")
	loaded := false
	var stdout bytes.Buffer
	err := run(
		[]string{"--check-wake-coalesce", "2026-08-26T15:17:55Z"},
		func() (config.AppConfig, error) {
			loaded = true
			return config.AppConfig{}, nil
		},
		nil,
		bytes.NewReader(nil),
		&stdout,
		io.Discard,
	)
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("error = %v", err)
	}
	if loaded {
		t.Fatal("missing thread identity loaded repository config")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
