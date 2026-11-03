package app

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestQualityGateIdenticalFailureDoesNotLaunchSecondRunner(t *testing.T) {
	useInlineQualityGateRunner(t)
	enterQualityGateGitWorkingDir(t)
	t.Setenv("PATH", writeTestTimingsGoShim(t, 1)+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, st := newQualityGateEnv(t)
	setQualityGateRetryAttemptTransport(t)
	var output bytes.Buffer
	if err := startQualityGate("go-test", st, &output, &bytes.Buffer{}); err == nil {
		t.Fatal("initial target failure unexpectedly passed")
	}
	if got := countQualityGateRunRecords(t, st); got != 1 {
		t.Fatalf("initial failed gate wrote %d runs", got)
	}
	setQualityGateRetryAttemptTransport(t)
	if err := startQualityGate("go-test", st, &bytes.Buffer{}, &bytes.Buffer{}); err == nil ||
		!strings.Contains(err.Error(), "have already produced a Go failure") {
		t.Fatalf("identical target failure was re-executed: %v", err)
	}
	if got := countQualityGateRunRecords(t, st); got != 1 {
		t.Fatalf("identical failure created %d gate runs", got)
	}
	t.Setenv("QUALITY_GATE_RETRY_DIAGNOSTIC", "new evidence")
	setQualityGateRetryAttemptTransport(t)
	if err := startQualityGate("go-test", st, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("changed diagnostic environment unexpectedly passed")
	}
	if got := countQualityGateRunRecords(t, st); got != 2 {
		t.Fatalf("new diagnostic environment did not admit another run: %d", got)
	}
}

func setQualityGateRetryAttemptTransport(t *testing.T) {
	t.Helper()
	attemptID, err := state.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(state.ParentActionAttemptIDEnv, attemptID)
	t.Setenv(state.ParentActionAttemptEnterEnv, time.Now().Format(time.RFC3339Nano))
	t.Setenv(state.WorkerProcessLaunchEnv, time.Now().Format(time.RFC3339Nano))
}

func countQualityGateRunRecords(t *testing.T, st *state.StateStore) int {
	t.Helper()
	entries, err := os.ReadDir(st.Path(qualitygate.RunDirectory))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() && qualitygate.ValidRunID(entry.Name()) {
			count++
		}
	}
	return count
}
