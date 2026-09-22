package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func runQualityGate(payload string, st *state.StateStore, stdout io.Writer) error {
	return runQualityGateWithDiagnostics(payload, st, stdout, io.Discard)
}

func runQualityGateWithDiagnostics(payload string, st *state.StateStore, stdout, diagnostics io.Writer) error {
	if action, runID, ok := splitQualityGateAction(payload); ok {
		return runQualityGateAction(action, runID, st, stdout)
	}
	return startQualityGate(payload, st, stdout, diagnostics)
}

func runQualityGateAction(action, runID string, st *state.StateStore, stdout io.Writer) error {
	switch action {
	case qualityGateActionStatus, qualityGateActionResult:
		return printQualityGateRun(st, runID, false, stdout)
	case qualityGateActionWatch:
		return printQualityGateRun(st, runID, true, stdout)
	case qualityGateActionInternal:
		if err := executeQualityGateRun(st, runID); err != nil {
			return err
		}
		return printQualityGateRun(st, runID, false, stdout)
	default:
		return machinecli.UsageErrorf("%s", qualityGateCommandUsage)
	}
}

func startQualityGate(form string, st *state.StateStore, stdout, diagnostics io.Writer) error {
	identity, err := prepareQualityGateStart(form, st)
	if err != nil {
		return err
	}
	lock, err := acquireQualityGateStartLock(st)
	if err != nil {
		return err
	}
	if existing, found := findRunningQualityGateRun(st, identity.Form, identity.Repository, identity.Snapshot); found {
		_ = lock.Close()
		_ = emitQualityGateStarted(diagnostics, existing.ValidationRunID, true)
		final, err := waitQualityGateRun(st, existing.ValidationRunID)
		if err != nil {
			return err
		}
		return finishQualityGateCommand(final, qualityGateForms[final.Form], stdout)
	}
	record, err := newQualityGateRunRecord(identity)
	if err != nil {
		_ = lock.Close()
		return err
	}
	if err := writeQualityGateRun(st, record); err != nil {
		_ = lock.Close()
		return err
	}
	wait, err := launchQualityGateRunner(st, record)
	if err != nil {
		_ = lock.Close()
		return failQualityGateLaunch(st, record, err)
	}
	_ = lock.Close()
	_ = emitQualityGateStarted(diagnostics, record.ValidationRunID, false)
	_ = wait()
	final, err := reconcileQualityGateAfterWait(st, record.ValidationRunID)
	if err != nil {
		return err
	}
	return finishQualityGateCommand(final, identity.GoArgs, stdout)
}

func prepareQualityGateStart(form string, st *state.StateStore) (qualityGateStartIdentity, error) {
	goArgs, ok := qualityGateForms[form]
	if !ok {
		return qualityGateStartIdentity{}, machinecli.UsageErrorf("%s", qualityGateCommandUsage)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return qualityGateStartIdentity{}, fmt.Errorf("quality gateの作業dirを取得できません: %w", err)
	}
	repository, err := qualityGateRepositoryRoot(workingDir)
	if err != nil {
		return qualityGateStartIdentity{}, err
	}
	snapshot, err := state.CaptureGitSnapshot(repository)
	if err != nil {
		return qualityGateStartIdentity{}, fmt.Errorf("quality gate snapshotを取得できません: %w", err)
	}
	if err := os.MkdirAll(st.Path(qualityGateRunDirectory), 0o700); err != nil {
		return qualityGateStartIdentity{}, fmt.Errorf("quality gate run directoryを作成できません: %w", err)
	}
	return qualityGateStartIdentity{
		Form:       form,
		GoArgs:     goArgs,
		Repository: repository,
		WorkingDir: workingDir,
		TaskID:     st.ReadOr("task.id", ""),
		Snapshot:   snapshot,
	}, nil
}

func newQualityGateRunRecord(identity qualityGateStartIdentity) (qualityGateRunRecord, error) {
	runID, err := newValidationRunID()
	if err != nil {
		return qualityGateRunRecord{}, err
	}
	return qualityGateRunRecord{
		ValidationRunID:               runID,
		Form:                          identity.Form,
		Repository:                    identity.Repository,
		WorkingDir:                    identity.WorkingDir,
		Head:                          identity.Snapshot.Head,
		IndexDigest:                   identity.Snapshot.IndexDigest,
		WorktreeDigest:                identity.Snapshot.WorktreeDigest,
		WorktreeDigestExcludingParent: identity.Snapshot.WorktreeDigestExcludingParent,
		TaskID:                        identity.TaskID,
		StartedAt:                     time.Now().UTC(),
		Status:                        qualityGateStatusRunning,
	}, nil
}

func launchQualityGateRunnerProcess(_ *state.StateStore, record qualityGateRunRecord) (qualityGateRunnerWait, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("glm-worker executableを解決できません: %w", err)
	}
	child := exec.Command(executable, "--quality-gate", qualityGateActionInternal, record.ValidationRunID)
	child.Dir = record.WorkingDir
	child.Env = qualityGateEnv()
	child.Stdout = io.Discard
	child.Stderr = io.Discard
	if err := child.Start(); err != nil {
		return nil, fmt.Errorf("quality gate runnerを開始できません: %w", err)
	}
	return child.Wait, nil
}

func executeQualityGateRun(st *state.StateStore, runID string) error {
	record, goArgs, active, err := beginQualityGateRun(st, runID)
	if err != nil || !active {
		return err
	}
	gateLog, runErr := runQualityGateProcess(record, goArgs)
	final, err := completeQualityGateRun(st, record, gateLog, runErr)
	if err != nil {
		return err
	}
	if final.Status != qualityGateStatusPass {
		return qualityGateErrorFromRecord(final)
	}
	return nil
}

func beginQualityGateRun(st *state.StateStore, runID string) (qualityGateRunRecord, []string, bool, error) {
	lock, err := acquireQualityGateRunStateLock(st, runID)
	if err != nil {
		return qualityGateRunRecord{}, nil, false, err
	}
	defer func() { _ = lock.Close() }()

	record, err := readQualityGateRun(st, runID)
	if err != nil {
		return qualityGateRunRecord{}, nil, false, err
	}
	if record.Status != qualityGateStatusRunning {
		return record, nil, false, nil
	}
	goArgs, ok := qualityGateForms[record.Form]
	if !ok {
		return qualityGateRunRecord{}, nil, false, fmt.Errorf("quality gate run %s has unknown form %q", runID, record.Form)
	}
	record.RunnerPID = os.Getpid()
	if err := writeQualityGateRun(st, record); err != nil {
		return qualityGateRunRecord{}, nil, false, err
	}
	return record, goArgs, true, nil
}

func runQualityGateProcess(record qualityGateRunRecord, goArgs []string) ([]byte, error) {
	var gateLog bytes.Buffer
	gate := exec.Command("go", goArgs...)
	gate.Dir = record.WorkingDir
	gate.Env = qualityGateEnv()
	gate.Stdout = &gateLog
	gate.Stderr = &gateLog
	err := gate.Run()
	return gateLog.Bytes(), err
}

func completeQualityGateRun(st *state.StateStore, record qualityGateRunRecord, gateLog []byte, runErr error) (qualityGateRunRecord, error) {
	lock, err := acquireQualityGateRunStateLock(st, record.ValidationRunID)
	if err != nil {
		return qualityGateRunRecord{}, err
	}
	defer func() { _ = lock.Close() }()

	status, exitCode, exitSource := qualityGateProcessOutcome(runErr)
	logPath, logErr := writeQualityGateRunLog(st, record.ValidationRunID, gateLog)
	if logErr != nil {
		status = qualityGateStatusFail
		if exitCode == 0 {
			exitCode = 1
			exitSource = state.ValidationExitSourceWrapper
		}
		logPath = ""
	}
	completed := time.Now().UTC()
	record.Status = status
	record.CompletedAt = &completed
	record.ExitCode = exitCode
	record.ExitSource = exitSource
	record.DurationMS = completed.Sub(record.StartedAt).Milliseconds()
	record.Log = logPath
	if err := writeQualityGateRun(st, record); err != nil {
		return qualityGateRunRecord{}, err
	}
	recordQualityGateValidation(st, record)
	return record, nil
}

func qualityGateProcessOutcome(runErr error) (string, int, string) {
	if runErr == nil {
		return qualityGateStatusPass, 0, state.ValidationExitSourceTarget
	}
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) {
		return qualityGateStatusFail, 1, state.ValidationExitSourceWrapper
	}
	exitCode := exitErr.ExitCode()
	if exitCode < 0 {
		return qualityGateStatusInterrupted, exitCode, state.ValidationExitSourceUnknown
	}
	return qualityGateStatusFail, exitCode, state.ValidationExitSourceTarget
}

func reconcileQualityGateAfterWait(st *state.StateStore, runID string) (qualityGateRunRecord, error) {
	final, err := reconcileQualityGateRun(st, runID)
	if err != nil || final.Status != qualityGateStatusRunning {
		return final, err
	}
	return markQualityGateInterrupted(st, runID, "quality gate runner exited before persisting a terminal result")
}

func finishQualityGateCommand(record qualityGateRunRecord, goArgs []string, stdout io.Writer) error {
	if record.Status != qualityGateStatusPass {
		return qualityGateErrorFromRecord(record)
	}
	return machinecli.WriteJSON(stdout, qualityGateOutput{
		Status:          record.Status,
		ValidationRunID: record.ValidationRunID,
		Form:            record.Form,
		Command:         "go " + strings.Join(goArgs, " "),
		WorkingDir:      record.WorkingDir,
		DurationMS:      record.DurationMS,
		Log:             record.Log,
	})
}

func qualityGateErrorFromRecord(record qualityGateRunRecord) *QualityGateError {
	return &QualityGateError{
		ValidationRunID: record.ValidationRunID,
		Form:            record.Form,
		Command:         "go " + strings.Join(qualityGateForms[record.Form], " "),
		WorkingDir:      record.WorkingDir,
		ExitCode:        record.ExitCode,
		DurationMS:      record.DurationMS,
		LogPath:         record.Log,
	}
}

func printQualityGateRun(st *state.StateStore, runID string, watch bool, stdout io.Writer) error {
	record, err := reconcileQualityGateRun(st, runID)
	if watch && err == nil {
		record, err = waitQualityGateRun(st, runID)
	}
	if err != nil {
		return err
	}
	return machinecli.WriteJSON(stdout, record)
}

func waitQualityGateRun(st *state.StateStore, runID string) (qualityGateRunRecord, error) {
	for {
		record, err := reconcileQualityGateRun(st, runID)
		if err != nil || record.Status != qualityGateStatusRunning {
			return record, err
		}
		time.Sleep(500 * time.Millisecond)
	}
}
