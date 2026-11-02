package runner

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestGitAuthorityGuardIgnoresRefsOutsideWorkspaceAuthority(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

func TestGitAuthorityRefEvidenceIgnoresMoreThanSixtyFourUnrelatedChanges(t *testing.T) {
	t.Parallel()
	root := newGitAuthorityRepo(t)
	runGitAuthorityCommand(t, root, "commit", "--allow-empty", "-q", "-m", "new authority")
	oldOID := strings.TrimSpace(runGitAuthorityOutput(t, root, "rev-parse", "HEAD~1"))
	newOID := strings.TrimSpace(runGitAuthorityOutput(t, root, "rev-parse", "HEAD"))
	runGitAuthorityCommand(t, root, "reset", "--hard", oldOID)
	current := strings.TrimSpace(runGitAuthorityOutput(t, root, "symbolic-ref", "HEAD"))
	guard, err := prepareGitAuthorityGuard(root)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.cleanup()
	for i := 0; i < 80; i++ {
		runGitAuthorityCommand(t, root, "update-ref", fmt.Sprintf("refs/heads/irrelevant-%03d", i), newOID)
	}
	runGitAuthorityCommand(t, root, "update-ref", "HEAD", newOID)
	err = guard.verify()
	var guardErr *GitAuthorityGuardError
	if !errors.As(err, &guardErr) || guardErr.Stage != guardStageAfterCallMutation {
		t.Fatalf("authority mutation was not rejected: %v", err)
	}
	if guardErr.RefChangesTruncated || len(guardErr.RefChanges) != 1 {
		t.Fatalf("unrelated churn displaced authority evidence: %#v", guardErr)
	}
	change := guardErr.RefChanges[0]
	if change.Name != current || change.Before == nil || change.After == nil ||
		change.Before.ObjectID != oldOID || change.After.ObjectID != newOID {
		t.Fatalf("wrong current-branch ref evidence: %#v", change)
	}
}

func TestGitAuthorityDetachedHeadDoesNotReportUnrelatedRefs(t *testing.T) {
	t.Parallel()
	root := newGitAuthorityRepo(t)
	runGitAuthorityCommand(t, root, "checkout", "--detach", "-q", "HEAD")
	guard, err := prepareGitAuthorityGuard(root)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.cleanup()
	runGitAuthorityCommand(t, root, "commit", "--allow-empty", "-q", "-m", "detached commit")
	err = guard.verify()
	var guardErr *GitAuthorityGuardError
	if !errors.As(err, &guardErr) || guardErr.Stage != guardStageAfterCallMutation {
		t.Fatalf("detached HEAD movement not rejected: %v", err)
	}
	if len(guardErr.RefChanges) != 0 || guardErr.RefChangesTruncated {
		t.Fatalf("detached HEAD reported unrelated ref changes: %#v", guardErr)
	}
}
