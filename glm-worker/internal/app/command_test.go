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
			args: []string{"--verify-auto-resume", "key-1234", "2026-08-12T20:01:20+09:00", "thread-uuid"},
			mode: ModeVerifyAutoResume,
		},
		{
			name: "check-wake-coalesce",
			args: []string{"--check-wake-coalesce", "thread-uuid", "2026-08-12T20:01:20+09:00"},
			mode: ModeCheckWakeCoalesce,
		},
		{name: "eval-ab", args: []string{"--eval-ab", "/tmp/ab-run"}, mode: ModeEvalAB, payload: "/tmp/ab-run"},
		{name: "call-outliers", args: []string{"--call-outliers"}, mode: ModeCallOutliers},
		{name: "model-routing", args: []string{"--model-routing"}, mode: ModeModelRouting},
		{name: "test-impact", args: []string{"--test-impact"}, mode: ModeTestImpact},
		{name: "codex-limit", args: []string{"--codex-limit"}, mode: ModeCodexLimit},
		{name: "repo-search", args: []string{"--repo-search", "worker dispatch"}, mode: ModeRepoSearch, payload: "worker dispatch"},
		{name: "repo-search-eval", args: []string{"--repo-search-eval"}, mode: ModeRepoSearchEval},
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
	tests := []struct {
		args  []string
		modes []string
	}{
		{args: []string{"--decision", "A案で進める"}, modes: []string{"--decision-file", "--decision-stdin"}},
		{args: []string{"--decision"}, modes: []string{"--decision-file", "--decision-stdin"}},
		{args: []string{"--fix", "指摘を修正"}, modes: []string{"--fix-file", "--fix-stdin"}},
		{args: []string{"--fix"}, modes: []string{"--fix-file", "--fix-stdin"}},
		{args: []string{"--fix", "--origin", "codex-review", "指摘を修正"}, modes: []string{"--fix-file", "--fix-stdin"}},
	}
	for _, test := range tests {
		command, err := ParseCommand(test.args)
		if err == nil {
			t.Fatalf("argv埋込みを受理しました: %#v", test.args)
		}
		if command.Payload != "" {
			t.Fatalf("argv埋込み本文をcommandへ解釈しています: %#v", command)
		}
		for _, mode := range test.modes {
			if !strings.Contains(err.Error(), mode) {
				t.Fatalf("安全なpayload mode %qへの案内がありません: %v", mode, err)
			}
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
		{"--verify-auto-resume", "key", "date"},
		{"--verify-auto-resume", "key", "date", "thread", "extra"},
		{"--check-wake-coalesce"},
		{"--check-wake-coalesce", "thread"},
		{"--check-wake-coalesce", "thread", "date", "extra"},
		{"--eval-ab"},
		{"--eval-ab", "dir", "extra"},
		{"--call-outliers", "extra"},
		{"--model-routing", "extra"},
		{"--test-impact", "extra"},
		{"--codex-limit", "extra"},
		{"--repo-search"},
		{"--repo-search", "query", "extra"},
		{"--repo-search-eval", "extra"},
	}

	for _, args := range tests {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("invalid argsを受理しました: %#v", args)
		}
	}
}

func TestParseCommandVerifyAutoResumeArgs(t *testing.T) {
	command, err := ParseCommand([]string{
		"--verify-auto-resume",
		"glm-worker-resume-abcd1234-ef012345",
		"2026-08-12T20:01:20+09:00",
		"019f88f8-0e70-7d53-a2a3-f0c61666827c",
	})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeVerifyAutoResume {
		t.Fatalf("Mode = %d", command.Mode)
	}
	if command.Verify.Key != "glm-worker-resume-abcd1234-ef012345" {
		t.Fatalf("Key = %q", command.Verify.Key)
	}
	if command.Verify.RFC3339 != "2026-08-12T20:01:20+09:00" {
		t.Fatalf("RFC3339 = %q", command.Verify.RFC3339)
	}
	if command.Verify.ThreadID != "019f88f8-0e70-7d53-a2a3-f0c61666827c" {
		t.Fatalf("ThreadID = %q", command.Verify.ThreadID)
	}
}

func TestParseCommandCheckWakeCoalesceArgs(t *testing.T) {
	command, err := ParseCommand([]string{
		"--check-wake-coalesce",
		"019f88f8-0e70-7d53-a2a3-f0c61666827c",
		"2026-08-26T15:17:55Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeCheckWakeCoalesce {
		t.Fatalf("Mode = %d", command.Mode)
	}
	if command.Coalesce.ParentThreadID != "019f88f8-0e70-7d53-a2a3-f0c61666827c" {
		t.Fatalf("ParentThreadID = %q", command.Coalesce.ParentThreadID)
	}
	if command.Coalesce.ResumeAtRFC3339 != "2026-08-26T15:17:55Z" {
		t.Fatalf("ResumeAtRFC3339 = %q", command.Coalesce.ResumeAtRFC3339)
	}
}
