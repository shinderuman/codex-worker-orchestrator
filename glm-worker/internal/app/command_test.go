package app

import (
	"strings"
	"testing"
)

func TestParseCommandModes(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		mode    CommandMode
		payload string
	}{
		{name: "new task", args: []string{"調査して", "実装する"}, mode: ModeNewTask, payload: "調査して 実装する"},
		{name: "resume", args: []string{"--resume"}, mode: ModeResume},
		{name: "stop", args: []string{"--stop"}, mode: ModeStop},
		{name: "isolate", args: []string{"--isolate"}, mode: ModeIsolate},
		{name: "status", args: []string{"--status"}, mode: ModeStatus},
		{name: "stats", args: []string{"--stats"}, mode: ModeStats},
		{name: "reset", args: []string{"--reset"}, mode: ModeReset},
		{
			name: "verify-auto-resume",
			args: []string{"--verify-auto-resume", "key-1234", "2026-08-12T20:01:20+09:00"},
			mode: ModeVerifyAutoResume,
		},
		{
			name: "check-wake-coalesce",
			args: []string{"--check-wake-coalesce", "2026-08-12T20:01:20+09:00"},
			mode: ModeCheckWakeCoalesce,
		},
		{name: "eval-ab", args: []string{"--eval-ab", "/tmp/ab-run"}, mode: ModeEvalAB, payload: "/tmp/ab-run"},
		{name: "call-outliers", args: []string{"--call-outliers"}, mode: ModeCallOutliers},
		{name: "model-routing", args: []string{"--model-routing"}, mode: ModeModelRouting},
		{name: "test-impact", args: []string{"--test-impact"}, mode: ModeTestImpact},
		{name: "codex-limit", args: []string{"--codex-limit"}, mode: ModeCodexLimit},
		{name: "repo-search", args: []string{"--repo-search", "worker dispatch"}, mode: ModeRepoSearch, payload: "worker dispatch"},
		{name: "repo-search-eval", args: []string{"--repo-search-eval"}, mode: ModeRepoSearchEval},
		{name: "parent-usage", args: []string{"--parent-usage"}, mode: ModeParentUsage},
		{name: "parent-usage task", args: []string{"--parent-usage", "task-123"}, mode: ModeParentUsage, payload: "task-123"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command, err := ParseCommand(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if command.Mode != test.mode || command.Payload != test.payload {
				t.Fatalf("command = %#v", command)
			}
		})
	}
}

func TestParseCommandStdinPayloadModes(t *testing.T) {
	digest := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	tests := []struct {
		name        string
		args        []string
		mode        CommandMode
		stdinBytes  int64
		expectedSHA string
	}{
		{name: "decision stdin", args: []string{"--decision-stdin", "3850"}, mode: ModeDecision, stdinBytes: 3850},
		{name: "fix stdin", args: []string{"--fix-stdin", "2507"}, mode: ModeFix, stdinBytes: 2507},
		{
			name:        "decision stdin with sha256",
			args:        []string{"--decision-stdin", "100", "--sha256", strings.ToUpper(digest)},
			mode:        ModeDecision,
			stdinBytes:  100,
			expectedSHA: digest,
		},
		{
			name:        "fix stdin with sha256",
			args:        []string{"--fix-stdin", "1", "--sha256", digest},
			mode:        ModeFix,
			stdinBytes:  1,
			expectedSHA: digest,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command, err := ParseCommand(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if command.Mode != test.mode || command.StdinBytes != test.stdinBytes || command.Payload != "" || command.SHA256 != test.expectedSHA {
				t.Fatalf("command = %#v", command)
			}
		})
	}
}

func TestParseCommandRejectsInvalidStdinArguments(t *testing.T) {
	digest := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tests := [][]string{
		{"--decision-stdin"},
		{"--decision-stdin", "abc"},
		{"--decision-stdin", "0"},
		{"--decision-stdin", "-1"},
		{"--decision-stdin", "10", "extra"},
		{"--decision-stdin", "10", "--sha256"},
		{"--decision-stdin", "10", "--sha256", "short"},
		{"--decision-stdin", "10", "--sha256", digest[:63] + "g"},
		{"--decision-stdin", "10", "--sha256", digest, "extra"},
		{"--decision-stdin", "10", "--decision", "payload"},
		{"--fix-stdin"},
		{"--fix-stdin", "10", "--checksum", digest},
	}

	for _, args := range tests {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("invalid argsを受理しました: %#v", args)
		}
	}
}

func TestParseCommandRejectsArgvDecisionFix(t *testing.T) {
	for _, args := range [][]string{
		{"--decision", "A案で進める"},
		{"--decision"},
		{"--fix", "指摘を修正"},
		{"--fix"},
		{"--fix", "--origin", "codex-review", "指摘を修正"},
	} {
		command, err := ParseCommand(args)
		if err == nil {
			t.Fatalf("argv埋込みを受理しました: %#v", args)
		}
		if command.Payload != "" {
			t.Fatalf("argv埋込み本文をcommandへ解釈しています: %#v", command)
		}
		if !strings.Contains(err.Error(), "--decision-stdin") || !strings.Contains(err.Error(), "--fix-stdin") {
			t.Fatalf("stdin modeへの案内がありません: %v", err)
		}
	}
}

func TestParseCommandRejectsInvalidArguments(t *testing.T) {
	tests := [][]string{
		nil,
		{"--decision"},
		{"--decision", "   "},
		{"--fix"},
		{"--resume", "extra"},
		{"--stop", "extra"},
		{"--isolate", "extra"},
		{"--status", "extra"},
		{"--stats", "extra"},
		{"--reset", "extra"},
		{"--verify-auto-resume"},
		{"--verify-auto-resume", "key"},
		{"--verify-auto-resume", "key", "date", "thread"},
		{"--verify-codex-wake"},
		{"--verify-codex-wake", "01a03a9e-10a0-7f11-801c-f04e5dbd5490"},
		{"--verify-codex-wake", "01a03a9e-10a0-7f11-801c-f04e5dbd5490", "2026-08-26T15:17:55Z", "extra"},
		{"--verify-codex-wake", "codex-5h-wake-01a03a9e-10a0-7f11-801c-f04e5dbd5490", "2026-08-26T15:17:55Z"},
		{"--verify-codex-wake", "01A03A9E-10A0-7F11-801C-F04E5DBD5490", "2026-08-26T15:17:55Z"},
		{"--verify-codex-wake", "not-a-thread-id", "2026-08-26T15:17:55Z"},
		{"--check-wake-coalesce"},
		{"--check-wake-coalesce", "date", "thread"},
		{"--eval-ab"},
		{"--eval-ab", "dir", "extra"},
		{"--call-outliers", "extra"},
		{"--model-routing", "extra"},
		{"--test-impact", "extra"},
		{"--codex-limit", "extra"},
		{"--repo-search"},
		{"--repo-search", "query", "extra"},
		{"--repo-search-eval", "extra"},
		{"--parent-usage", "task-1", "extra"},
	}

	for _, args := range tests {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("invalid argsを受理しました: %#v", args)
		}
	}
}

func TestParseCommandVerifyAutomationArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		mode     CommandMode
		key      string
		threadID string
	}{
		{
			name:     "auto resume takes the automation key only",
			args:     []string{"--verify-auto-resume", "glm-worker-resume-abcd1234-ef012345", "2026-08-12T20:01:20+09:00"},
			mode:     ModeVerifyAutoResume,
			key:      "glm-worker-resume-abcd1234-ef012345",
			threadID: "",
		},
		{
			name:     "codex wake takes the wake thread ID only",
			args:     []string{"--verify-codex-wake", "01a03a9e-10a0-7f11-801c-f04e5dbd5490", "2026-08-12T20:01:20+09:00"},
			mode:     ModeVerifyCodexWake,
			key:      "",
			threadID: "01a03a9e-10a0-7f11-801c-f04e5dbd5490",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command, err := ParseCommand(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if command.Mode != test.mode {
				t.Fatalf("Mode = %d", command.Mode)
			}
			if command.Verify.Key != test.key {
				t.Fatalf("Key = %q", command.Verify.Key)
			}
			if command.Verify.ThreadID != test.threadID {
				t.Fatalf("ThreadID = %q", command.Verify.ThreadID)
			}
			if command.Verify.RFC3339 != "2026-08-12T20:01:20+09:00" {
				t.Fatalf("RFC3339 = %q", command.Verify.RFC3339)
			}
		})
	}
}

func TestParseCommandCheckWakeCoalesceArgs(t *testing.T) {
	command, err := ParseCommand([]string{
		"--check-wake-coalesce",
		"2026-08-26T15:17:55Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeCheckWakeCoalesce {
		t.Fatalf("Mode = %d", command.Mode)
	}
	if command.Coalesce.ParentThreadID != "" {
		t.Fatalf("parent thread IDをargvから受理しています: %q", command.Coalesce.ParentThreadID)
	}
	if command.Coalesce.ResumeAtRFC3339 != "2026-08-26T15:17:55Z" {
		t.Fatalf("ResumeAtRFC3339 = %q", command.Coalesce.ResumeAtRFC3339)
	}
}
