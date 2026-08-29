package app

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type qualityGateSignalWriter struct {
	mu    sync.Mutex
	data  bytes.Buffer
	once  sync.Once
	wrote chan struct{}
}

func newQualityGateSignalWriter() *qualityGateSignalWriter {
	return &qualityGateSignalWriter{wrote: make(chan struct{})}
}

func (w *qualityGateSignalWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.data.Write(p)
	if n > 0 {
		w.once.Do(func() { close(w.wrote) })
	}
	return n, err
}

func (w *qualityGateSignalWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.data.String()
}

func TestQualityGateRecoveryCommandParsesRunSurfaces(t *testing.T) {
	runID := strings.Repeat("a", 32)
	for _, action := range []string{qualityGateActionStatus, qualityGateActionWatch, qualityGateActionResult} {
		cmd, err := ParseCommand([]string{"--quality-gate", action, runID})
		if err != nil {
			t.Fatalf("%s parse: %v", action, err)
		}
		if cmd.Mode != ModeQualityGate || cmd.Payload != action+qualityGateActionSeparator+runID {
			t.Fatalf("%s command = %+v", action, cmd)
		}
	}
	if _, err := ParseCommand([]string{"--quality-gate", qualityGateActionStatus, "../../state"}); err == nil {
		t.Fatal("path-like validation run id must fail closed")
	}
}

func TestQualityGateRunningIdentityMatchesOnlyExactSnapshot(t *testing.T) {
	_, st := newQualityGateEnv(t)
	runID := strings.Repeat("b", 32)
	snapshot := state.GitSnapshot{Head: "head-a", IndexDigest: "index-a", WorktreeDigest: "worktree-a"}
	record := qualityGateRunRecord{
		ValidationRunID: runID,
		Form:            "go-test",
		Repository:      "/repo",
		WorkingDir:      "/repo/glm-worker",
		Head:            snapshot.Head,
		IndexDigest:     snapshot.IndexDigest,
		WorktreeDigest:  snapshot.WorktreeDigest,
		StartedAt:       time.Now().UTC(),
		Status:          qualityGateStatusRunning,
	}
	if err := writeQualityGateRun(st, record); err != nil {
		t.Fatal(err)
	}
	got, found := findRunningQualityGateRun(st, "go-test", "/repo", snapshot)
	if !found || got.ValidationRunID != runID {
		t.Fatalf("same snapshot running gate not found: found=%v record=%+v", found, got)
	}
	changed := snapshot
	changed.WorktreeDigest = "worktree-b"
	if _, found := findRunningQualityGateRun(st, "go-test", "/repo", changed); found {
		t.Fatal("changed snapshot reused a running gate")
	}
	if _, found := findRunningQualityGateRun(st, "go-test-race", "/repo", snapshot); found {
		t.Fatal("different form reused a running gate")
	}
}

func TestQualityGateCompletedRunIsNotReused(t *testing.T) {
	_, st := newQualityGateEnv(t)
	runID := strings.Repeat("c", 32)
	snapshot := state.GitSnapshot{Head: "head", IndexDigest: "index", WorktreeDigest: "worktree"}
	record := qualityGateRunRecord{
		ValidationRunID: runID,
		Form:            "go-test",
		Repository:      "/repo",
		Head:            snapshot.Head,
		IndexDigest:     snapshot.IndexDigest,
		WorktreeDigest:  snapshot.WorktreeDigest,
		StartedAt:       time.Now().Add(-time.Second).UTC(),
		Status:          qualityGateStatusPass,
	}
	if err := writeQualityGateRun(st, record); err != nil {
		t.Fatal(err)
	}
	if _, found := findRunningQualityGateRun(st, "go-test", "/repo", snapshot); found {
		t.Fatal("completed validation must not be reused as a running attachment")
	}
}

func TestQualityGateRunLogsRemainAddressablePerRun(t *testing.T) {
	_, st := newQualityGateEnv(t)
	firstID := strings.Repeat("d", 32)
	secondID := strings.Repeat("e", 32)
	firstPath, err := writeQualityGateRunLog(st, firstID, []byte("first\n"))
	if err != nil {
		t.Fatal(err)
	}
	secondPath, err := writeQualityGateRunLog(st, secondID, []byte("second\n"))
	if err != nil {
		t.Fatal(err)
	}
	if firstPath == secondPath {
		t.Fatalf("run logs share path: %q", firstPath)
	}
	first, _ := os.ReadFile(firstPath)
	second, _ := os.ReadFile(secondPath)
	if string(first) != "first\n" || string(second) != "second\n" {
		t.Fatalf("run log evidence overwritten: first=%q second=%q", first, second)
	}
}

func TestQualityGateStatusIsMachineReadableByRunID(t *testing.T) {
	_, st := newQualityGateEnv(t)
	runID := strings.Repeat("f", 32)
	record := qualityGateRunRecord{
		ValidationRunID: runID,
		Form:            "go-test",
		Repository:      "/repo",
		Head:            "head",
		IndexDigest:     "index",
		WorktreeDigest:  "worktree",
		StartedAt:       time.Now().UTC(),
		Status:          qualityGateStatusRunning,
	}
	if err := writeQualityGateRun(st, record); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := printQualityGateRun(st, runID, false, &stdout); err != nil {
		t.Fatal(err)
	}
	var got qualityGateRunRecord
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("status is not JSON: %v: %s", err, stdout.String())
	}
	if got.ValidationRunID != runID || got.Status != qualityGateStatusRunning {
		t.Fatalf("status output = %+v", got)
	}
}

func TestQualityGateConcurrentSameSnapshotAttachesAndStreamsRunID(t *testing.T) {
	cfg, st := newQualityGateEnv(t)
	previous := launchQualityGateRunner
	defer func() { launchQualityGateRunner = previous }()

	var launches atomic.Int32
	release := make(chan struct{})
	started := make(chan qualityGateRunRecord, 1)
	launchQualityGateRunner = func(_ *state.StateStore, record qualityGateRunRecord) (qualityGateRunnerWait, error) {
		launches.Add(1)
		started <- record
		return func() error {
			<-release
			completed := time.Now().UTC()
			record.Status = qualityGateStatusPass
			record.CompletedAt = &completed
			record.DurationMS = completed.Sub(record.StartedAt).Milliseconds()
			return writeQualityGateRun(st, record)
		}, nil
	}

	firstDiagnostics := newQualityGateSignalWriter()
	var firstOutput bytes.Buffer
	firstErr := make(chan error, 1)
	go func() {
		firstErr <- dispatchMachineOutput(Command{Mode: ModeQualityGate, Payload: "go-test"}, cfg, defaultRunnerFactory, &firstOutput, firstDiagnostics)
	}()

	record := <-started
	<-firstDiagnostics.wrote
	if !strings.Contains(firstDiagnostics.String(), record.ValidationRunID) || !strings.Contains(firstDiagnostics.String(), `"attached":false`) {
		t.Fatalf("first start event = %s", firstDiagnostics.String())
	}

	secondDiagnostics := newQualityGateSignalWriter()
	var secondOutput bytes.Buffer
	secondErr := make(chan error, 1)
	go func() {
		secondErr <- startQualityGate("go-test", st, &secondOutput, secondDiagnostics)
	}()
	<-secondDiagnostics.wrote
	if launches.Load() != 1 {
		t.Fatalf("same snapshot launched %d runners", launches.Load())
	}
	if !strings.Contains(secondDiagnostics.String(), record.ValidationRunID) || !strings.Contains(secondDiagnostics.String(), `"attached":true`) {
		t.Fatalf("attach event = %s", secondDiagnostics.String())
	}

	close(release)
	if err := <-firstErr; err != nil {
		t.Fatalf("first quality gate: %v", err)
	}
	if err := <-secondErr; err != nil {
		t.Fatalf("attached quality gate: %v", err)
	}
	for label, raw := range map[string][]byte{"first": firstOutput.Bytes(), "second": secondOutput.Bytes()} {
		var out qualityGateOutput
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("%s output: %v: %s", label, err, raw)
		}
		if out.ValidationRunID != record.ValidationRunID || out.Status != qualityGateStatusPass {
			t.Fatalf("%s output = %+v", label, out)
		}
	}
}

func TestQualityGateStatusMarksDeadRunnerInterrupted(t *testing.T) {
	_, st := newQualityGateEnv(t)
	runID := strings.Repeat("9", 32)
	record := qualityGateRunRecord{
		ValidationRunID: runID,
		Form:            "go-test",
		Repository:      "/repo",
		StartedAt:       time.Now().Add(-time.Second).UTC(),
		Status:          qualityGateStatusRunning,
		RunnerPID:       2147483647,
	}
	if err := writeQualityGateRun(st, record); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := printQualityGateRun(st, runID, false, &stdout); err != nil {
		t.Fatal(err)
	}
	var got qualityGateRunRecord
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != qualityGateStatusInterrupted || got.ExitCode != -1 || got.CompletedAt == nil || got.Log == "" {
		t.Fatalf("reconciled record = %+v", got)
	}
}

func TestQualityGateStatusKeepsFreshRunnerWithoutPIDRunning(t *testing.T) {
	_, st := newQualityGateEnv(t)
	runID := strings.Repeat("8", 32)
	record := qualityGateRunRecord{
		ValidationRunID: runID,
		Form:            "go-test",
		Repository:      "/repo",
		StartedAt:       time.Now().UTC(),
		Status:          qualityGateStatusRunning,
	}
	if err := writeQualityGateRun(st, record); err != nil {
		t.Fatal(err)
	}
	got, err := reconcileQualityGateRun(st, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != qualityGateStatusRunning || got.CompletedAt != nil {
		t.Fatalf("fresh runner without pid was reconciled too early: %+v", got)
	}
}

func TestQualityGateStatusMarksRunnerWithoutPIDInterruptedAfterStartupGrace(t *testing.T) {
	_, st := newQualityGateEnv(t)
	runID := strings.Repeat("7", 32)
	record := qualityGateRunRecord{
		ValidationRunID: runID,
		Form:            "go-test",
		Repository:      "/repo",
		StartedAt:       time.Now().Add(-qualityGateRunnerStartupGrace - time.Second).UTC(),
		Status:          qualityGateStatusRunning,
	}
	if err := writeQualityGateRun(st, record); err != nil {
		t.Fatal(err)
	}
	got, err := reconcileQualityGateRun(st, runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != qualityGateStatusInterrupted || got.ExitCode != -1 || got.CompletedAt == nil || got.Log == "" {
		t.Fatalf("runner without pid did not recover after startup grace: %+v", got)
	}
}

func TestQualityGateReconcileDoesNotClobberTerminalResultWrittenUnderRunLock(t *testing.T) {
	_, st := newQualityGateEnv(t)
	runID := strings.Repeat("6", 32)
	record := qualityGateRunRecord{
		ValidationRunID: runID,
		Form:            "go-test",
		Repository:      "/repo",
		StartedAt:       time.Now().Add(-time.Second).UTC(),
		Status:          qualityGateStatusRunning,
		RunnerPID:       2147483647,
	}
	if err := writeQualityGateRun(st, record); err != nil {
		t.Fatal(err)
	}

	lock, err := acquireQualityGateRunStateLock(st, runID)
	if err != nil {
		t.Fatal(err)
	}
	reconciled := make(chan qualityGateRunRecord, 1)
	reconcileErr := make(chan error, 1)
	go func() {
		got, err := reconcileQualityGateRun(st, runID)
		if err != nil {
			reconcileErr <- err
			return
		}
		reconciled <- got
	}()

	completed := time.Now().UTC()
	record.Status = qualityGateStatusPass
	record.CompletedAt = &completed
	record.ExitCode = 0
	if err := writeQualityGateRun(st, record); err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-reconcileErr:
		t.Fatal(err)
	case got := <-reconciled:
		if got.Status != qualityGateStatusPass || got.CompletedAt == nil {
			t.Fatalf("terminal result was clobbered by reconcile: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reconcile did not complete after run-state lock release")
	}
}
