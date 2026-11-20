package app

import "testing"

func TestStagedFixAcceptsExplicitCurrentDiffScope(t *testing.T) {
	t.Parallel()
	command, err := ParseCommand(stagedTestCommandArgs("fix", "--origin", "glm-reviewer",
		"--accepted-scope", "current-diff"))
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeFix || command.Origin != "glm-reviewer" || command.AcceptedScope != "current-diff" {
		t.Fatalf("command = %#v", command)
	}
}

func TestStagedFixParsesCauseWithOrigin(t *testing.T) {
	t.Parallel()
	command, err := ParseCommand(stagedTestCommandArgs("fix", "--origin", "codex-review",
		"--cause", "reviewer"))
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeFix || command.Origin != "codex-review" || command.Cause != "reviewer" {
		t.Fatalf("command = %#v", command)
	}
}

func TestApproveSurfaceRequiresExactCurrentDiffScope(t *testing.T) {
	t.Parallel()
	command, err := ParseCommand([]string{"--approve-surface", "current-diff"})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeApproveSurface || command.AcceptedScope != "current-diff" {
		t.Fatalf("command = %#v", command)
	}
}

func TestAcceptedScopeIsFixOnlyAndClosedValue(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		stagedTestCommandArgs("decision", "--accepted-scope", "current-diff"),
		stagedTestCommandArgs("fix", "--accepted-scope", "anything-else"),
		stagedTestCommandArgs("fix", "--accepted-scope", "current-diff", "--accepted-scope", "current-diff"),
		stagedTestCommandArgs("decision", "--cause", "worker"),
		stagedTestCommandArgs("fix", "--origin", "codex-review"),
		stagedTestCommandArgs("fix", "--origin", "codex-review", "--cause", "legacy-layer"),
		stagedTestCommandArgs("fix", "--origin", "glm-reviewer", "--cause", "worker", "--cause", "reviewer"),
		stagedTestCommandArgs("fix", "--accepted-scope", "current-diff", "--approval-only"),
	} {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("accepted invalid args: %v", args)
		}
	}
}

func TestApproveSurfaceRejectsOtherScopesAndOptions(t *testing.T) {
	t.Parallel()
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
