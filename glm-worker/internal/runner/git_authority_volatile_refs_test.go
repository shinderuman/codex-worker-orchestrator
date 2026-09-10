package runner

import (
	"errors"
	"strings"
	"testing"
)

func TestGitAuthorityGuardIgnoresCodexDesktopVolatileRefs(t *testing.T) {
	cases := []struct {
		name      string
		refPrefix string
		operation string
	}{
		{name: "turn diffs add", refPrefix: gitAuthorityTurnDiffsPrefix, operation: "add"},
		{name: "turn diffs delete", refPrefix: gitAuthorityTurnDiffsPrefix, operation: "delete"},
		{name: "turn diffs update", refPrefix: gitAuthorityTurnDiffsPrefix, operation: "update"},
		{name: "snapshots add", refPrefix: gitAuthoritySnapshotsPrefix, operation: "add"},
		{name: "snapshots delete", refPrefix: gitAuthoritySnapshotsPrefix, operation: "delete"},
		{name: "snapshots update", refPrefix: gitAuthoritySnapshotsPrefix, operation: "update"},
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
				t.Fatalf("volatile ref mutation failed guard: %v", err)
			}
			afterDigest, err := CaptureGitAuthorityRefDigest(root)
			if err != nil {
				t.Fatal(err)
			}
			if afterDigest != beforeDigest {
				t.Fatalf("volatile ref changed authority digest: before=%s after=%s", beforeDigest, afterDigest)
			}
		})
	}
}

func TestGitAuthorityGuardStillDetectsNonvolatileCodexRef(t *testing.T) {
	root := newGitAuthorityRepo(t)
	objectID := strings.TrimSpace(runGitAuthorityOutput(t, root, "rev-parse", "HEAD"))
	refName := "refs/codex/authority/fixture"
	guard, err := prepareGitAuthorityGuard(root)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.cleanup()
	runGitAuthorityCommand(t, root, "update-ref", refName, objectID)

	err = guard.verify()
	var guardErr *GitAuthorityGuardError
	if !errors.As(err, &guardErr) || guardErr.Stage != guardStageAfterCallMutation {
		t.Fatalf("guard error = %#v", err)
	}
	if len(guardErr.RefChanges) != 1 || guardErr.RefChanges[0].Name != refName {
		t.Fatalf("ref changes = %#v", guardErr.RefChanges)
	}
}
