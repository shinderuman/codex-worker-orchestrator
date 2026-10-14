package controller

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

func TestCleanupArchivesIgnoredParentActionTransportAndDetectsRace(t *testing.T) {
	f, retired, candidate := retiredLaneFixture(t)
	root := f.source.Workspace.Root
	exclude := filepath.Join(f.store.Identity().CommonDir, "info", "exclude")
	file, err := os.OpenFile(exclude, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\n" + parentaction.StageDirName + "/\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parentaction.StageDirName, "decision-proof.txt")
	writeSuspensionTestFile(t, root, path, "preserved parent action\n")
	op, head, err := f.store.planExecutionCleanup(CleanupExecutionInput{ExpectedGeneration: retired.Head.ControllerGeneration, WorkspaceID: f.source.Workspace.ID, SealRef: candidate.SealRef})
	if err != nil {
		t.Fatal(err)
	}
	if got := controllerGitOutput(t, root, "show", op.CleanupTrees.WorktreeTree+":"+filepath.ToSlash(path)); got != "preserved parent action" {
		t.Fatal("transport bytes were not archived")
	}
	if err := f.store.prepareExecutionOperation(&op, head); err != nil {
		t.Fatal(err)
	}
	writeSuspensionTestFile(t, root, path, "changed ignored transport\n")
	if _, err := f.store.RecoverExecutionOperation(op.Transition.TransitionID); err == nil {
		t.Fatal("ignored staging race was deleted")
	}
	if _, err := os.Stat(filepath.Join(root, path)); err != nil {
		t.Fatal("rejected cleanup destroyed transport")
	}
	writeSuspensionTestFile(t, root, path, "preserved parent action\n")
	if _, err := f.store.RecoverExecutionOperation(op.Transition.TransitionID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("valid archived cleanup did not finish")
	}
}

func TestCleanupParentActionsRejectsSymlinkWithoutFollowingIt(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(out, []byte("outside evidence"), 0o644); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(root, parentaction.StageDirName)
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, filepath.Join(stage, "decision.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := captureCleanupParentActions(root, nil); err == nil {
		t.Fatal("staging symlink accepted")
	}
	if got, err := os.ReadFile(out); err != nil || string(got) != "outside evidence" {
		t.Fatal("outside file changed")
	}
}
