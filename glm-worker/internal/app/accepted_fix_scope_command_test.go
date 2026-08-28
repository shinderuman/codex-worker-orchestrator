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

func TestAcceptedScopeIsFixOnlyAndClosedValue(t *testing.T) {
	for _, args := range [][]string{
		{"--decision-stdin", "12", "--accepted-scope", "current-diff"},
		{"--fix-stdin", "12", "--accepted-scope", "anything-else"},
		{"--fix-stdin", "12", "--accepted-scope", "current-diff", "--accepted-scope", "current-diff"},
	} {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("accepted invalid args: %v", args)
		}
	}
}
