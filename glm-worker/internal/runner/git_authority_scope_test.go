package runner

import (
	"errors"
	"strings"
	"testing"
)

func TestGitAuthorityGuardIgnoresRefsOutsideWorkspaceAuthority(t *testing.T) {
	cases := []struct {
		name      string
		refPrefix string
		operation string
	}{
		{name: "turn diffs add", refPrefix: "refs/codex/turn-diffs/", operation: "add"},
		{name: "turn diffs delete", refPrefix: "refs/codex/turn-diffs/", operation: "delete"},
		{name: "turn diffs update", refPrefix: "refs/codex/turn-diffs/", operation: "update"},
		{name: "snapshots add", refPrefix: "refs/codex/snapshots/", operation: "add"},
		{name: "snapshots delete", refPrefix: "refs/codex/snapshots/", operation: "delete"},
		{name: "snapshots update", refPrefix: "refs/codex/snapshots/", operation: "update"},
		{name: "unrelated branch add", refPrefix: "refs/heads/unrelated-", operation: "add"},
		{name: "unrelated branch delete", refPrefix: "refs/heads/unrelated-", operation: "delete"},
		{name: "unrelated remote tracking add", refPrefix: "refs/remotes/origin/unrelated-", operation: "add"},
		{name: "unrelated remote tracking delete", refPrefix: "refs/remotes/origin/unrelated-", operation: "delete"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newGitAuthorityRepo(t)
			runGitAuthorityCommand(t, root, "commit", "--allow-empty", "-q", "-m", "second")
			oldObjectID := strings.TrimSpace(runGitAuthorityOutput(t, root, "rev-parse", "HEAD~1"))
			newObjectID := strings.TrimSpace(runGitAuthorityOutput(t, root, "rev-parse", "HEAD"))
			refName := tc.refPrefix + "fixture"
			if tc.operation != "add" {
				runGitAuthorityCommand(t, root, "update-ref", refName, oldObjectID)
			}

			beforeDigest, err := CaptureGitAuthorityRefDigest(root)
			if err != nil {
				t.Fatal(err)
			}
			guard, err := prepareGitAuthorityGuard(root)
			if err != nil {
				t.Fatal(err)
			}
			defer guard.cleanup()

			switch tc.operation {
			case "add", "update":
				runGitAuthorityCommand(t, root, "update-ref", refName, newObjectID)
			case "delete":
				runGitAuthorityCommand(t, root, "update-ref", "-d", refName)
			}
			if err := guard.verify(); err != nil {
				t.Fatalf("ref outside workspace authority failed guard: %v", err)
			}
			afterDigest, err := CaptureGitAuthorityRefDigest(root)
			if err != nil {
				t.Fatal(err)
			}
			if afterDigest != beforeDigest {
				t.Fatalf("ref outside workspace authority changed authority digest: before=%s after=%s", beforeDigest, afterDigest)
			}
		})
	}
}

func TestGitAuthorityGuardDetectsHeadSymbolicTargetMutation(t *testing.T) {
	root := newGitAuthorityRepo(t)
	currentBranch := strings.TrimSpace(runGitAuthorityOutput(t, root, "symbolic-ref", "--short", "HEAD"))
	guard, err := prepareGitAuthorityGuard(root)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.cleanup()
	runGitAuthorityCommand(t, root, "branch", "-m", currentBranch, "bypassed-authority")

	err = guard.verify()
	var guardErr *GitAuthorityGuardError
	if !errors.As(err, &guardErr) || guardErr.Stage != guardStageAfterCallMutation {
		t.Fatalf("guard error = %#v", err)
	}
	if guardErr.RefAfterDigest == "" || guardErr.RefAfterDigest == guardErr.RefBeforeDigest {
		t.Fatalf("authority digest did not track the renamed checked-out branch: %#v", guardErr)
	}
	names := map[string]bool{}
	for _, change := range guardErr.RefChanges {
		names[change.Name] = true
	}
	if !names["refs/heads/"+currentBranch] || !names["refs/heads/bypassed-authority"] {
		t.Fatalf("ref change evidence lost the symbolic target rename: %#v", guardErr.RefChanges)
	}
}
