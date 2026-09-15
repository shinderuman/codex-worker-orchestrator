package app

import (
	"bytes"
	"errors"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
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

	plan := Command{Mode: ModeAutoResumePlan}
	if err := bindCurrentCodexThreadIdentity(&plan); err != nil {
		t.Fatal(err)
	}
	if plan.AutoResume.ParentThreadID != threadID {
		t.Fatalf("auto-resume plan thread ID = %q", plan.AutoResume.ParentThreadID)
	}
}

func TestBindCurrentCodexThreadIdentityRejectsMissingOrInvalidEnvironment(t *testing.T) {
	cases := []struct {
		name  string
		mode  CommandMode
		bound func(*Command) string
	}{
		{"verify-auto-resume", ModeVerifyAutoResume, func(cmd *Command) string { return cmd.Verify.ThreadID }},
		{"auto-resume-plan", ModeAutoResumePlan, func(cmd *Command) string { return cmd.AutoResume.ParentThreadID }},
	}
	for _, testCase := range cases {
		for _, value := range []string{"", "not-a-thread-id"} {
			t.Run(testCase.name+"/"+value, func(t *testing.T) {
				t.Setenv(codexThreadIDEnv, value)
				cmd := Command{Mode: testCase.mode}
				err := bindCurrentCodexThreadIdentity(&cmd)
				var notFound *machinecli.NotFoundError
				if !errors.As(err, &notFound) {
					t.Fatalf("error = %v", err)
				}
				if bound := testCase.bound(&cmd); bound != "" {
					t.Fatalf("invalid environment was bound: %q", bound)
				}
			})
		}
	}
}

func TestBindCurrentCodexThreadIdentityLeavesCodexWakeVerificationUnbound(t *testing.T) {
	wakeThreadID := "01a03a9e-10a0-7f11-801c-f04e5dbd5490"
	for _, envValue := range []string{"01a05f46-47aa-77d2-912c-0d6b078cb856", "", "not-a-thread-id"} {
		t.Run(envValue, func(t *testing.T) {
			t.Setenv(codexThreadIDEnv, envValue)
			cmd := Command{Mode: ModeVerifyCodexWake, Verify: VerifyArgs{ThreadID: wakeThreadID}}
			if err := bindCurrentCodexThreadIdentity(&cmd); err != nil {
				t.Fatal(err)
			}
			if cmd.Verify.ThreadID != wakeThreadID {
				t.Fatalf("wake thread ID = %q", cmd.Verify.ThreadID)
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
	var notFound *machinecli.NotFoundError
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
