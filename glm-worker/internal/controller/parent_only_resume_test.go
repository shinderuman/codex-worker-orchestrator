package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParentOnlyRootResumePreservesSourceAndRecoversPartialMaterialization(t *testing.T) {
	for _, partial := range []bool{false, true} {
		name := "direct"
		if partial {
			name = "partial-recovery"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo, store, policy, root, episode := startParentOnlyBlockerEpisodeWithRootMutation(t, func(repo string) {
				writeSuspensionTestFile(t, repo, "root-output.txt", "root staged\n")
				runControllerGit(t, repo, "add", "--", "root-output.txt")
				writeSuspensionTestFile(t, repo, "root-output.txt", "root worktree\n")
				writeSuspensionTestFile(t, repo, "root-untracked.txt", "root untracked\n")
			})
			projected := publishParentOnlyBlocker(t, store, root, episode, policy)
			if _, err := os.Lstat(filepath.Join(repo, "blocker-result.txt")); !os.IsNotExist(err) {
				t.Fatalf("child output was manually materialized: %v", err)
			}
			op, head, err := store.planParentOnlyExecution(ParentOnlyMaterializeInput{ExpectedGeneration: projected.Execution.ExpectedGeneration, EpisodeID: projected.Execution.EpisodeID, EpisodeRevision: projected.Execution.EpisodeRevision})
			if err != nil {
				t.Fatal(err)
			}
			if err := store.prepareExecutionOperation(&op, head); err != nil {
				t.Fatal(err)
			}
			if partial {
				writeSuspensionTestFile(t, repo, "blocker-result.txt", "implemented blocker\n")
			}
			resumed, err := store.RecoverExecutionOperation(op.Transition.TransitionID)
			if err != nil || resumed.Admission == nil {
				t.Fatalf("Root resume: %#v %v", resumed, err)
			}
			if got := controllerGitOutput(t, repo, "show", ":root-output.txt"); got != "root staged" {
				t.Fatalf("staged Root output changed: %q", got)
			}
			for path, want := range map[string]string{"root-output.txt": "root worktree\n", "root-untracked.txt": "root untracked\n", "blocker-result.txt": "implemented blocker\n"} {
				got, err := os.ReadFile(filepath.Join(repo, path))
				if err != nil || string(got) != want {
					t.Fatalf("%s: %q %v", path, got, err)
				}
			}
			tree, err := store.PreviewCandidateTree(*resumed.Admission, policy)
			if err != nil {
				t.Fatal(err)
			}
			for path, want := range map[string]string{"root-output.txt": "root worktree", "root-untracked.txt": "root untracked", "blocker-result.txt": "implemented blocker"} {
				if got := controllerGitOutput(t, repo, "show", tree+":"+path); got != want {
					t.Fatalf("candidate lost %s: %q", path, got)
				}
			}
		})
	}
}

func TestParentOnlyRootResumeRejectsSuspendedSourceConflict(t *testing.T) {
	t.Parallel()
	repo, store, policy, root, episode := startParentOnlyBlockerEpisodeWithRootMutation(t, func(repo string) {
		writeSuspensionTestFile(t, repo, "blocker-result.txt", "Root owns different output\n")
	})
	projected := publishParentOnlyBlocker(t, store, root, episode, policy)
	_, err := store.AdmitParentOnlyExecution(ParentOnlyMaterializeInput{ExpectedGeneration: projected.Execution.ExpectedGeneration, EpisodeID: projected.Execution.EpisodeID, EpisodeRevision: projected.Execution.EpisodeRevision})
	if err == nil {
		t.Fatal("conflicting Root and child output was silently accepted")
	}
	got, err := os.ReadFile(filepath.Join(repo, "blocker-result.txt"))
	if err != nil || string(got) != "Root owns different output\n" {
		t.Fatalf("conflict destroyed Root output: %q %v", got, err)
	}
}

func TestParentOnlyRootResumePreservesCleanGitlink(t *testing.T) {
	for _, initialized := range []bool{false, true} {
		name := "deinitialized"
		if initialized {
			name = "initialized"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			child := newControllerRepo(t)
			repo, store, policy, root, episode := startParentOnlyBlockerEpisodeWithRootMutation(t, func(repo string) {
				runControllerGit(t, repo, "-c", "protocol.file.allow=always", "submodule", "add", child, "clean-child")
				if !initialized {
					runControllerGit(t, repo, "submodule", "deinit", "--force", "clean-child")
				}
			})
			projected := publishParentOnlyBlocker(t, store, root, episode, policy)
			resumed, err := store.AdmitParentOnlyExecution(ParentOnlyMaterializeInput{ExpectedGeneration: projected.Execution.ExpectedGeneration, EpisodeID: projected.Execution.EpisodeID, EpisodeRevision: projected.Execution.EpisodeRevision})
			if err != nil || resumed.Admission == nil {
				t.Fatalf("clean gitlink prevented Root resume: %#v %v", resumed, err)
			}
			tree, err := store.PreviewCandidateTree(*resumed.Admission, policy)
			if err != nil {
				t.Fatal(err)
			}
			want := controllerGitOutput(t, child, "rev-parse", "HEAD")
			if got := controllerGitOutput(t, repo, "rev-parse", tree+":clean-child"); got != want {
				t.Fatalf("Root gitlink changed: %s != %s", got, want)
			}
		})
	}
}
