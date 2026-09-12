package workflow

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestUnparkLogicalCommitFailurePreservesParkResources(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("state directory permission failure injection is Unix-oriented")
	}
	fixture := newParkFixture(t)
	parked := fixture.park(t)

	record, err := fixture.st.LoadParkRecord()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.w.verifyParkIntegration(&record); err != nil {
		t.Fatal(err)
	}

	stateDir := filepath.Dir(fixture.st.Path("task.status"))
	if err := os.Chmod(stateDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(stateDir, 0o700) })

	var stdout bytes.Buffer
	err = fixture.w.ExecuteUnpark(&stdout)
	if chmodErr := os.Chmod(stateDir, 0o700); chmodErr != nil {
		t.Fatal(chmodErr)
	}
	if err == nil {
		t.Fatal("logical unpark commit failure was accepted")
	}
	if stdout.Len() != 0 || fixture.st.TaskStatus() != state.TaskStatusParked {
		t.Fatalf("failed logical unpark changed semantic state: status=%s output=%s", fixture.st.TaskStatus(), stdout.String())
	}
	if _, err := os.Stat(fixture.worktree); err != nil {
		t.Fatalf("logical unpark failure removed worktree: %v", err)
	}
	if _, err := state.ResolveBranchTip(fixture.repo, parked.Branch); err != nil {
		t.Fatalf("logical unpark failure removed branch: %v", err)
	}
	if _, err := os.Stat(fixture.st.ParkContentPath("parked-dirty.md")); err != nil {
		t.Fatalf("logical unpark failure removed park content: %v", err)
	}
	if _, err := fixture.st.AttachSiblingStore(config.RepoHashFor(fixture.worktree)).LoadParkOrigin(); err != nil {
		t.Fatalf("logical unpark failure removed park origin: %v", err)
	}
	persisted, err := fixture.st.LoadParkRecord()
	if err != nil || persisted.Cleanup == nil {
		t.Fatalf("logical unpark failure lost cleanup checkpoint: %#v err=%v", persisted, err)
	}
}
