package parentactioncmd

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestResumeWithRepairedWorkerDoesNotCompleteFailedResume(t *testing.T) {
	cfg, st, record := newGuardRepairLifecycleState(t)
	source := guardRepairLifecycleWorkerSource(t, st, state.TaskStatusRateLimited, true)
	source = strings.TrimSuffix(source, " }\n") + "; os.Exit(23) }\n"
	writeGuardRepairWorkerModule(t, cfg.RepoRoot, source)
	persistReadyGuardRepair(t, cfg, st, &record)

	err := resumeWithRepairedWorker(cfg, st, record, io.Discard, io.Discard, nil, errors.New("initial self-block"))
	if err == nil {
		t.Fatal("failed resumed execution was accepted")
	}
	got, loadErr := st.LoadGuardRepairRecord()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if got.Status != state.GuardRepairReady || got.OriginalResumeObserved {
		t.Fatalf("failed resumed execution recorded repair completion: %#v", got)
	}
	if st.TaskStatus() != state.TaskStatusRateLimited {
		t.Fatalf("original task status = %s want %s", st.TaskStatus(), state.TaskStatusRateLimited)
	}
}
