package controller

import (
	"strings"
	"testing"
)

func TestWorkspaceSnapshotTracksOnlyHeadSymbolicTarget(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		refPrefix string
		operation string
	}{
		{name: "codex turn diffs add", refPrefix: "refs/codex/turn-diffs/", operation: "add"},
		{name: "codex turn diffs update", refPrefix: "refs/codex/turn-diffs/", operation: "update"},
		{name: "codex turn diffs delete", refPrefix: "refs/codex/turn-diffs/", operation: "delete"},
		{name: "codex snapshots add", refPrefix: "refs/codex/snapshots/", operation: "add"},
		{name: "codex snapshots update", refPrefix: "refs/codex/snapshots/", operation: "update"},
		{name: "codex snapshots delete", refPrefix: "refs/codex/snapshots/", operation: "delete"},
		{name: "unrelated branch add", refPrefix: "refs/heads/unrelated-", operation: "add"},
		{name: "unrelated branch update", refPrefix: "refs/heads/unrelated-", operation: "update"},
		{name: "unrelated branch delete", refPrefix: "refs/heads/unrelated-", operation: "delete"},
		{name: "unrelated remote tracking add", refPrefix: "refs/remotes/origin/unrelated-", operation: "add"},
		{name: "unrelated remote tracking delete", refPrefix: "refs/remotes/origin/unrelated-", operation: "delete"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newVolatileRefFixtureRepo(t)
			oldObjectID := controllerGitOutput(t, root, "rev-parse", "HEAD~1")
			newObjectID := controllerGitOutput(t, root, "rev-parse", "HEAD")
			refName := tc.refPrefix + "fixture"
			if tc.operation != "add" {
				runControllerGit(t, root, "update-ref", refName, oldObjectID)
			}
			before, err := CaptureWorkspaceSnapshot(root)
			if err != nil {
				t.Fatal(err)
			}
			switch tc.operation {
			case "add", "update":
				runControllerGit(t, root, "update-ref", refName, newObjectID)
			case "delete":
				runControllerGit(t, root, "update-ref", "-d", refName)
			}
			after, err := CaptureWorkspaceSnapshot(root)
			if err != nil {
				t.Fatal(err)
			}
			if after.ID != before.ID || after.RefDigest != before.RefDigest {
				t.Fatalf("ref outside the workspace authority changed workspace snapshot: before=%s after=%s", before.ID, after.ID)
			}
		})
	}
}

func TestWorkspaceSnapshotTracksHeadSymbolicTargetIdentity(t *testing.T) {
	t.Parallel()
	root := newVolatileRefFixtureRepo(t)
	before, err := CaptureWorkspaceSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, root, "branch", "-m", "renamed-authority")
	after, err := CaptureWorkspaceSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if after.Head != before.Head {
		t.Fatalf("branch rename unexpectedly moved HEAD: before=%s after=%s", before.Head, after.Head)
	}
	if after.RefDigest == before.RefDigest || after.ID == before.ID {
		t.Fatalf("checked-out branch identity change was ignored: before=%#v after=%#v", before, after)
	}
}

func TestDetachedWorkspaceSnapshotIgnoresSharedRepositoryRefActivity(t *testing.T) {
	t.Parallel()
	repo, linked := newControllerLinkedWorktree(t)
	objectID := controllerGitOutput(t, repo, "rev-parse", "HEAD")
	before, err := CaptureWorkspaceSnapshot(linked)
	if err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "branch", "codex/repository-audit-october", objectID)
	runControllerGit(t, repo, "update-ref", "refs/remotes/origin/codex/repository-audit-october", objectID)
	runControllerGit(t, repo, "update-ref", "refs/remotes/origin/main", objectID)
	after, err := CaptureWorkspaceSnapshot(linked)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("shared-repository ref activity changed the detached lane snapshot: before=%#v after=%#v", before, after)
	}
}

func TestLiveLeaseAdmitsUnrelatedRepositoryRefTurnoverAndNormalStopResume(t *testing.T) {
	t.Parallel()
	fixture := newFindingAcceptanceFixture(t)
	root := fixture.source.Workspace.Root
	authority := fixture.source.MutationAuthority()
	tip := controllerGitOutput(t, root, "rev-parse", "HEAD")
	base := controllerGitOutput(t, root, "rev-parse", "HEAD~1")

	steps := []struct {
		name  string
		apply func()
	}{
		{"volatile ref add", func() { runControllerGit(t, root, "update-ref", "refs/codex/turn-diffs/fixture", tip) }},
		{"volatile ref update", func() { runControllerGit(t, root, "update-ref", "refs/codex/turn-diffs/fixture", base) }},
		{"volatile ref delete", func() { runControllerGit(t, root, "update-ref", "-d", "refs/codex/turn-diffs/fixture") }},
		{"unrelated branch add", func() { runControllerGit(t, root, "branch", "unrelated-worktree-branch") }},
		{"unrelated branch update", func() { runControllerGit(t, root, "update-ref", "refs/heads/unrelated-worktree-branch", base) }},
		{"unrelated branch delete", func() { runControllerGit(t, root, "update-ref", "-d", "refs/heads/unrelated-worktree-branch") }},
		{"unrelated remote tracking add", func() { runControllerGit(t, root, "update-ref", "refs/remotes/origin/unrelated", tip) }},
		{"unrelated remote tracking delete", func() { runControllerGit(t, root, "update-ref", "-d", "refs/remotes/origin/unrelated") }},
	}
	for _, step := range steps {
		step.apply()
		after, err := CaptureWorkspaceSnapshot(root)
		if err != nil {
			t.Fatal(err)
		}
		if after != fixture.source.Snapshot {
			t.Fatalf("%s changed the lease-bound workspace snapshot: %#v", step.name, after)
		}
		if _, err := fixture.store.AdmitMutation(authority, fixture.source.Workspace, after); err != nil {
			t.Fatalf("%s was rejected by the live lease: %v", step.name, err)
		}
	}

	runControllerGit(t, root, "update-ref", "refs/codex/snapshots/fixture", tip)
	episode := planSuspensionTestEpisode(t, fixture)
	suspended, err := fixture.store.SuspendExecution(fixture.source, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatalf("normal stop was rejected with unrelated refs present: %v", err)
	}
	if suspended.Suspension == nil || suspended.Head.LiveLeaseID != "" {
		t.Fatalf("stop did not preserve quiescent authority: %#v", suspended)
	}
	resumed, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: suspended.Head.ControllerGeneration,
		EpisodeID:          episode.EpisodeID,
		EpisodeRevision:    episode.Revision,
	})
	if err != nil {
		t.Fatalf("normal resume was rejected after unrelated ref turnover: %v", err)
	}
	if resumed.Admission == nil || resumed.Admission.Lease.LeaseID == "" {
		t.Fatalf("resumed lane has no live lease: %#v", resumed)
	}
}

func TestLiveLeaseStillStopsOnIllegalWorkspaceChanges(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(t *testing.T, root string)
		changed func(before, after WorkspaceSnapshot) bool
	}{
		{
			name: "HEAD moved",
			mutate: func(t *testing.T, root string) {
				runControllerGit(t, root, "-c", "user.name=illegal head", "-c", "user.email=illegal@example.invalid",
					"commit", "--allow-empty", "-q", "-m", "illegal head move")
			},
			changed: func(before, after WorkspaceSnapshot) bool { return after.Head != before.Head },
		},
		{
			name: "index staged",
			mutate: func(t *testing.T, root string) {
				writeSuspensionTestFile(t, root, "illegal-staged.txt", "staged outside authority\n")
				runControllerGit(t, root, "add", "illegal-staged.txt")
			},
			changed: func(before, after WorkspaceSnapshot) bool { return after.IndexDigest != before.IndexDigest },
		},
		{
			name: "worktree modified",
			mutate: func(t *testing.T, root string) {
				writeSuspensionTestFile(t, root, "IMPLEMENTATION_TASKS/finding-child.md",
					"# finding child\n\n## Contract\n\nmodified outside authority\n\n## Dependencies\n\nnone\n")
			},
			changed: func(before, after WorkspaceSnapshot) bool { return after.WorktreeDigest != before.WorktreeDigest },
		},
		{
			name: "checked-out branch renamed",
			mutate: func(t *testing.T, root string) {
				runControllerGit(t, root, "branch", "-m", "illegal-authority-rename")
			},
			changed: func(before, after WorkspaceSnapshot) bool { return after.RefDigest != before.RefDigest },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newFindingAcceptanceFixture(t)
			root := fixture.source.Workspace.Root
			tc.mutate(t, root)
			after, err := CaptureWorkspaceSnapshot(root)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.changed(fixture.source.Snapshot, after) {
				t.Fatalf("%s did not change the expected workspace surface", tc.name)
			}
			_, err = fixture.store.AdmitMutation(fixture.source.MutationAuthority(), fixture.source.Workspace, after)
			if err == nil || !strings.Contains(err.Error(), "snapshot") {
				t.Fatalf("illegal %s was not safely stopped: %v", tc.name, err)
			}
		})
	}
}

func newVolatileRefFixtureRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runControllerGit(t, root, "init", "-q", "-b", "main", root)
	runControllerGit(t, root, "-c", "user.name=volatile ref test", "-c", "user.email=volatile@example.invalid", "commit", "--allow-empty", "-q", "-m", "first")
	runControllerGit(t, root, "-c", "user.name=volatile ref test", "-c", "user.email=volatile@example.invalid", "commit", "--allow-empty", "-q", "-m", "second")
	return root
}
