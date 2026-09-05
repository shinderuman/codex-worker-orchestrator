package app

import "testing"

func TestFixStdinAcceptsExplicitCurrentDiffScope(t *testing.T) {
	command, err := ParseCommand([]string{
		"--fix-stdin", "12",
		"--origin", "glm-reviewer",
		"--accepted-scope", "current-diff",
	})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeFix || command.Origin != "glm-reviewer" || command.AcceptedScope != "current-diff" {
		t.Fatalf("command = %#v", command)
	}
}

func TestFixStdinParsesCauseWithOrigin(t *testing.T) {
	command, err := ParseCommand([]string{
		"--fix-stdin", "12",
		"--origin", "codex-review",
		"--cause", "reviewer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeFix || command.Origin != "codex-review" || command.Cause != "reviewer" {
		t.Fatalf("command = %#v", command)
	}
}

func TestFixStdinAcceptsApprovalOnlyForCurrentDiff(t *testing.T) {
	command, err := ParseCommand([]string{
		"--fix-stdin", "12",
		"--accepted-scope", "current-diff",
		"--approval-only",
	})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeFix || command.AcceptedScope != "current-diff" || !command.ApprovalOnly {
		t.Fatalf("command = %#v", command)
	}
}

func TestAcceptedScopeIsFixOnlyAndClosedValue(t *testing.T) {
	for _, args := range [][]string{
		{"--decision-stdin", "12", "--accepted-scope", "current-diff"},
		{"--fix-stdin", "12", "--accepted-scope", "anything-else"},
		{"--fix-stdin", "12", "--accepted-scope", "current-diff", "--accepted-scope", "current-diff"},
		{"--decision-stdin", "12", "--cause", "worker"},
		{"--fix-stdin", "12", "--origin", "codex-review"},
		{"--fix-stdin", "12", "--origin", "codex-review", "--cause", "legacy-layer"},
		{"--fix-stdin", "12", "--origin", "glm-reviewer", "--cause", "worker", "--cause", "reviewer"},
	} {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("accepted invalid args: %v", args)
		}
	}
}

func TestApprovalOnlyRequiresCurrentDiffWithoutOrigin(t *testing.T) {
	for _, args := range [][]string{
		{"--fix-stdin", "12", "--approval-only"},
		{"--fix-stdin", "12", "--accepted-scope", "current-diff", "--approval-only", "--approval-only"},
		{"--fix-stdin", "12", "--origin", "glm-reviewer", "--accepted-scope", "current-diff", "--approval-only"},
		{"--fix-stdin", "12", "--cause", "worker", "--accepted-scope", "current-diff", "--approval-only"},
		{"--decision-stdin", "12", "--approval-only"},
	} {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("accepted invalid args: %v", args)
		}
	}
}
