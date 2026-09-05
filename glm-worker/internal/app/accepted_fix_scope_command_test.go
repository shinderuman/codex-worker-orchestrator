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

func TestApproveSurfaceRequiresExactCurrentDiffScope(t *testing.T) {
	command, err := ParseCommand([]string{"--approve-surface", "current-diff"})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeApproveSurface || command.AcceptedScope != "current-diff" {
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
		{"--fix-stdin", "12", "--accepted-scope", "current-diff", "--approval-only"},
	} {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("accepted invalid args: %v", args)
		}
	}
}

func TestApproveSurfaceRejectsOtherScopesAndOptions(t *testing.T) {
	for _, args := range [][]string{
		{"--approve-surface"},
		{"--approve-surface", "other-scope"},
		{"--approve-surface", "current-diff", "current-diff"},
		{"--approve-surface", "--accepted-scope", "current-diff"},
	} {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("accepted invalid args: %v", args)
		}
	}
}
