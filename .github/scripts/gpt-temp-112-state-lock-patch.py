from pathlib import Path

path = Path("glm-worker/internal/app/quality_gate.go")
text = path.read_text()


def replace_once(old: str, new: str) -> None:
    global text
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"expected one match, got {count}: {old[:80]!r}")
    text = text.replace(old, new, 1)


replace_once(
    '\tqualityGateRunLog             = "gate.log"\n',
    '\tqualityGateRunLog             = "gate.log"\n\tqualityGateRunStateLock       = "state.lock"\n',
)

replace_once(
    """func beginQualityGateRun(st *state.StateStore, runID string) (qualityGateRunRecord, []string, bool, error) {
\trecord, err := readQualityGateRun(st, runID)
\tif err != nil {
\t\treturn qualityGateRunRecord{}, nil, false, err
\t}
\tif record.Status != qualityGateStatusRunning {
\t\treturn record, nil, false, nil
\t}
\tgoArgs, ok := qualityGateForms[record.Form]
\tif !ok {
\t\treturn qualityGateRunRecord{}, nil, false, fmt.Errorf("quality gate run %s has unknown form %q", runID, record.Form)
\t}
\trecord.RunnerPID = os.Getpid()
\tif err := writeQualityGateRun(st, record); err != nil {
\t\treturn qualityGateRunRecord{}, nil, false, err
\t}
\treturn record, goArgs, true, nil
}
""",
    """func beginQualityGateRun(st *state.StateStore, runID string) (qualityGateRunRecord, []string, bool, error) {
\tlock, err := acquireQualityGateRunStateLock(st, runID)
\tif err != nil {
\t\treturn qualityGateRunRecord{}, nil, false, err
\t}
\tdefer func() { _ = lock.Close() }()

\trecord, err := readQualityGateRun(st, runID)
\tif err != nil {
\t\treturn qualityGateRunRecord{}, nil, false, err
\t}
\tif record.Status != qualityGateStatusRunning {
\t\treturn record, nil, false, nil
\t}
\tgoArgs, ok := qualityGateForms[record.Form]
\tif !ok {
\t\treturn qualityGateRunRecord{}, nil, false, fmt.Errorf("quality gate run %s has unknown form %q", runID, record.Form)
\t}
\trecord.RunnerPID = os.Getpid()
\tif err := writeQualityGateRun(st, record); err != nil {
\t\treturn qualityGateRunRecord{}, nil, false, err
\t}
\treturn record, goArgs, true, nil
}
""",
)

replace_once(
    """func completeQualityGateRun(st *state.StateStore, record qualityGateRunRecord, gateLog []byte, runErr error) (qualityGateRunRecord, error) {
\tstatus, exitCode := qualityGateProcessOutcome(runErr)
""",
    """func completeQualityGateRun(st *state.StateStore, record qualityGateRunRecord, gateLog []byte, runErr error) (qualityGateRunRecord, error) {
\tlock, err := acquireQualityGateRunStateLock(st, record.ValidationRunID)
\tif err != nil {
\t\treturn qualityGateRunRecord{}, err
\t}
\tdefer func() { _ = lock.Close() }()

\tstatus, exitCode := qualityGateProcessOutcome(runErr)
""",
)

replace_once(
    """func reconcileQualityGateRun(st *state.StateStore, runID string) (qualityGateRunRecord, error) {
\trecord, err := readQualityGateRun(st, runID)
\tif err != nil {
\t\treturn qualityGateRunRecord{}, err
\t}
\tif record.Status != qualityGateStatusRunning {
\t\treturn record, nil
\t}
\tif record.RunnerPID > 0 {
\t\tif !qualityGateProcessAlive(record.RunnerPID) {
\t\t\treturn markQualityGateInterrupted(st, record, "quality gate runner is no longer running")
\t\t}
\t\treturn record, nil
\t}
\tif time.Since(record.StartedAt) >= qualityGateRunnerStartupGrace {
\t\treturn markQualityGateInterrupted(st, record, "quality gate runner did not publish its pid before startup grace elapsed")
\t}
\treturn record, nil
}

func markQualityGateInterrupted(st *state.StateStore, record qualityGateRunRecord, reason string) (qualityGateRunRecord, error) {
""",
    """func reconcileQualityGateRun(st *state.StateStore, runID string) (qualityGateRunRecord, error) {
\tlock, err := acquireQualityGateRunStateLock(st, runID)
\tif err != nil {
\t\treturn qualityGateRunRecord{}, err
\t}
\tdefer func() { _ = lock.Close() }()

\trecord, err := readQualityGateRun(st, runID)
\tif err != nil {
\t\treturn qualityGateRunRecord{}, err
\t}
\tif record.Status != qualityGateStatusRunning {
\t\treturn record, nil
\t}
\tif record.RunnerPID > 0 {
\t\tif !qualityGateProcessAlive(record.RunnerPID) {
\t\t\treturn markQualityGateInterruptedLocked(st, record, "quality gate runner is no longer running")
\t\t}
\t\treturn record, nil
\t}
\tif time.Since(record.StartedAt) >= qualityGateRunnerStartupGrace {
\t\treturn markQualityGateInterruptedLocked(st, record, "quality gate runner did not publish its pid before startup grace elapsed")
\t}
\treturn record, nil
}

func markQualityGateInterruptedLocked(st *state.StateStore, record qualityGateRunRecord, reason string) (qualityGateRunRecord, error) {
""",
)

replace_once(
    """func acquireQualityGateStartLock(st *state.StateStore) (*RepoLock, error) {
\tpath := st.Path(filepath.Join(qualityGateRunDirectory, "start.lock"))
\tfor attempt := 0; attempt < 100; attempt++ {
\t\tlock, err := AcquireRepoLock(path)
\t\tif err == nil {
\t\t\treturn lock, nil
\t\t}
\t\tif !errors.Is(err, ErrRepoLockHeld) {
\t\t\treturn nil, err
\t\t}
\t\ttime.Sleep(20 * time.Millisecond)
\t}
\treturn nil, ErrRepoLockHeld
}
""",
    """func acquireQualityGateStartLock(st *state.StateStore) (*RepoLock, error) {
\treturn acquireQualityGateLock(st.Path(filepath.Join(qualityGateRunDirectory, "start.lock")))
}

func acquireQualityGateRunStateLock(st *state.StateStore, runID string) (*RepoLock, error) {
\tif !validValidationRunID(runID) {
\t\treturn nil, fmt.Errorf("invalid validation run id")
\t}
\treturn acquireQualityGateLock(st.Path(filepath.Join(qualityGateRunDirectory, runID, qualityGateRunStateLock)))
}

func acquireQualityGateLock(path string) (*RepoLock, error) {
\tfor attempt := 0; attempt < 100; attempt++ {
\t\tlock, err := AcquireRepoLock(path)
\t\tif err == nil {
\t\t\treturn lock, nil
\t\t}
\t\tif !errors.Is(err, ErrRepoLockHeld) {
\t\t\treturn nil, err
\t\t}
\t\ttime.Sleep(20 * time.Millisecond)
\t}
\treturn nil, ErrRepoLockHeld
}
""",
)

path.write_text(text)

test_path = Path("glm-worker/internal/app/quality_gate_recovery_test.go")
test_text = test_path.read_text()
addition = r'''

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
'''
if "TestQualityGateReconcileDoesNotClobberTerminalResultWrittenUnderRunLock" in test_text:
    raise SystemExit("regression test already exists")
test_path.write_text(test_text + addition)
