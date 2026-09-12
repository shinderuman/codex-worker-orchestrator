package workflow

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestParkRejectsPendingUnparkCleanup(t *testing.T) {
	fixture := newParkFixture(t)
	parked := fixture.park(t)
	originPath := fixture.st.AttachSiblingStore(config.RepoHashFor(fixture.worktree)).Path("park.origin.json")
	if err := os.Remove(originPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(originPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(originPath, "block-cleanup"), []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}

	var unparkOutput bytes.Buffer
	if err := fixture.w.ExecuteUnpark(&unparkOutput); err == nil || !strings.Contains(err.Error(), "cleanup") {
		t.Fatalf("unpark cleanup failure = %v", err)
	}
	if fixture.st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("cleanup-pending status = %s", fixture.st.TaskStatus())
	}
	if _, admitted, err := fixture.st.AdmitParentAction(state.ParentActionAccept); err != nil || admitted {
		t.Fatalf("accept admitted during cleanup pending: admitted=%v err=%v", admitted, err)
	}

	var parkAgain bytes.Buffer
	if err := fixture.w.ExecutePark(&parkAgain); err == nil || !strings.Contains(err.Error(), "unpark cleanup") {
		t.Fatalf("park during cleanup pending = %v", err)
	}
	if parkAgain.Len() != 0 {
		t.Fatalf("park during cleanup pending wrote output: %s", parkAgain.String())
	}
	record, err := fixture.st.LoadParkRecord()
	if err != nil {
		t.Fatal(err)
	}
	if record.ParkID != parked.ParkID || record.Cleanup == nil {
		t.Fatalf("park cleanup journal was overwritten: %#v", record)
	}
}
