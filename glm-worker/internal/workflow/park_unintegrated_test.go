package workflow

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestUnparkRejectsUnintegratedInterruptCommitWithOriginalHeadUnchanged(t *testing.T) {
	fixture := newParkFixture(t)
	parked := fixture.park(t)

	interruptChange := filepath.Join(fixture.worktree, "interrupt-feature.md")
	if err := os.WriteFile(interruptChange, []byte("interrupt task result\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRetentionGit(t, fixture.worktree, "add", "interrupt-feature.md")
	runRetentionGit(t, fixture.worktree, "commit", "-q", "-m", "interrupt task")

	var stdout bytes.Buffer
	err := fixture.w.ExecuteUnpark(&stdout)
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) {
		t.Fatalf("unpark error = %T, want WorkerError: %v", err, err)
	}
	if !strings.Contains(workerErr.Message, "統合されていない") {
		t.Fatalf("unpark error = %q, want unintegrated interrupt rejection for %s", workerErr.Message, parked.Branch)
	}
	if fixture.st.TaskStatus() != state.TaskStatusParked {
		t.Fatalf("task status = %s, want parked after rejected unpark", fixture.st.TaskStatus())
	}
}
