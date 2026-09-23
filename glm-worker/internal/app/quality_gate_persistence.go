package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskview"
)

func reconcileQualityGateRun(st *state.StateStore, runID string) (qualitygate.RunRecord, error) {
	lock, err := acquireQualityGateRunStateLock(st, runID)
	if err != nil {
		return qualitygate.RunRecord{}, err
	}
	defer func() { _ = lock.Close() }()

	record, err := readQualityGateRun(st, runID)
	if err != nil {
		return qualitygate.RunRecord{}, err
	}
	if record.Status != qualitygate.StatusRunning {
		return record, nil
	}
	if record.RunnerPID > 0 {
		if !qualityGateProcessAlive(record.RunnerPID) {
			return markQualityGateInterruptedLocked(st, record, "quality gate runner is no longer running")
		}
		return record, nil
	}
	if time.Since(record.StartedAt) >= qualityGateRunnerStartupGrace {
		return markQualityGateInterruptedLocked(st, record, "quality gate runner did not publish its pid before startup grace elapsed")
	}
	return record, nil
}

func markQualityGateInterrupted(st *state.StateStore, runID, reason string) (qualitygate.RunRecord, error) {
	lock, err := acquireQualityGateRunStateLock(st, runID)
	if err != nil {
		return qualitygate.RunRecord{}, err
	}
	defer func() { _ = lock.Close() }()

	record, err := readQualityGateRun(st, runID)
	if err != nil {
		return qualitygate.RunRecord{}, err
	}
	if record.Status != qualitygate.StatusRunning {
		return record, nil
	}
	return markQualityGateInterruptedLocked(st, record, reason)
}

func markQualityGateInterruptedLocked(st *state.StateStore, record qualitygate.RunRecord, reason string) (qualitygate.RunRecord, error) {
	completed := time.Now().UTC()
	record.Status = qualitygate.StatusInterrupted
	record.ExitCode = -1
	record.ExitSource = state.ValidationExitSourceUnknown
	record.CompletedAt = &completed
	record.DurationMS = completed.Sub(record.StartedAt).Milliseconds()
	if record.Log == "" {
		if logPath, err := writeQualityGateRunLog(st, record.ValidationRunID, []byte(reason+"\n")); err == nil {
			record.Log = logPath
		}
	}
	if err := writeQualityGateRun(st, record); err != nil {
		return qualitygate.RunRecord{}, err
	}
	recordQualityGateValidation(st, record)
	return record, nil
}

func findRunningQualityGateRun(st *state.StateStore, form, repository string, snapshot state.GitSnapshot) (qualitygate.RunRecord, bool) {
	entries, err := os.ReadDir(st.Path(qualitygate.RunDirectory))
	if err != nil {
		return qualitygate.RunRecord{}, false
	}
	for _, entry := range entries {
		if !entry.IsDir() || !qualitygate.ValidRunID(entry.Name()) {
			continue
		}
		record, err := reconcileQualityGateRun(st, entry.Name())
		if err != nil || record.Status != qualitygate.StatusRunning {
			continue
		}
		if sameQualityGateSnapshot(record, form, repository, snapshot) {
			return record, true
		}
	}
	return qualitygate.RunRecord{}, false
}

func sameQualityGateSnapshot(record qualitygate.RunRecord, form, repository string, snapshot state.GitSnapshot) bool {
	return record.Form == form &&
		record.Repository == repository &&
		record.Head == snapshot.Head &&
		record.IndexDigest == snapshot.IndexDigest &&
		record.WorktreeDigest == snapshot.WorktreeDigest
}

func acquireQualityGateStartLock(st *state.StateStore) (*RepoLock, error) {
	return acquireQualityGateLock(st.Path(filepath.Join(qualitygate.RunDirectory, "start.lock")))
}

func acquireQualityGateRunStateLock(st *state.StateStore, runID string) (*RepoLock, error) {
	if !qualitygate.ValidRunID(runID) {
		return nil, fmt.Errorf("invalid validation run id")
	}
	return acquireQualityGateLock(st.Path(filepath.Join(qualitygate.RunDirectory, runID, qualityGateRunStateLock)))
}

func acquireQualityGateLock(path string) (*RepoLock, error) {
	for attempt := 0; attempt < 100; attempt++ {
		lock, err := AcquireRepoLock(path)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, ErrRepoLockHeld) {
			return nil, err
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil, ErrRepoLockHeld
}

func qualityGateRepositoryRoot(workingDir string) (string, error) {
	out, err := exec.Command("git", "-C", workingDir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("quality gate repository identityを取得できません: %w", err)
	}
	return filepath.Clean(strings.TrimSpace(string(out))), nil
}

func writeQualityGateRun(st *state.StateStore, record qualitygate.RunRecord) error {
	if !qualitygate.ValidRunID(record.ValidationRunID) {
		return fmt.Errorf("invalid validation run id")
	}
	data, err := qualitygate.Encode(record)
	if err != nil {
		return fmt.Errorf("quality gate run recordをencodeできません: %w", err)
	}
	if err := st.Write(qualitygate.RunRelativePath(record.ValidationRunID), string(data)); err != nil {
		return fmt.Errorf("quality gate run recordを保存できません: %w", err)
	}
	return nil
}

func readQualityGateRun(st *state.StateStore, runID string) (qualitygate.RunRecord, error) {
	if !qualitygate.ValidRunID(runID) {
		return qualitygate.RunRecord{}, &machinecli.NotFoundError{Message: "quality gate runが見つかりません"}
	}
	data, err := os.ReadFile(st.Path(qualitygate.RunRelativePath(runID)))
	if errors.Is(err, os.ErrNotExist) {
		return qualitygate.RunRecord{}, &machinecli.NotFoundError{Message: "quality gate runが見つかりません"}
	}
	if err != nil {
		return qualitygate.RunRecord{}, err
	}
	record, err := qualitygate.Decode(data)
	if err != nil {
		return qualitygate.RunRecord{}, fmt.Errorf("quality gate run recordをdecodeできません: %w", err)
	}
	if record.ValidationRunID != runID {
		return qualitygate.RunRecord{}, fmt.Errorf("quality gate run record identity mismatch")
	}
	return record, nil
}

func writeQualityGateRunLog(st *state.StateStore, runID string, data []byte) (string, error) {
	if !qualitygate.ValidRunID(runID) {
		return "", fmt.Errorf("invalid validation run id")
	}
	path := st.Path(filepath.Join(qualitygate.RunDirectory, runID, qualitygate.RunLog))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func failQualityGateLaunch(st *state.StateStore, record qualitygate.RunRecord, launchErr error) error {
	completed := time.Now().UTC()
	record.Status = qualitygate.StatusFail
	record.ExitCode = 1
	record.ExitSource = state.ValidationExitSourceWrapper
	record.CompletedAt = &completed
	record.DurationMS = completed.Sub(record.StartedAt).Milliseconds()
	if logPath, err := writeQualityGateRunLog(st, record.ValidationRunID, []byte(launchErr.Error()+"\n")); err == nil {
		record.Log = logPath
	}
	if err := writeQualityGateRun(st, record); err != nil {
		return errors.Join(launchErr, err)
	}
	recordQualityGateValidation(st, record)
	return qualityGateErrorFromRecord(record)
}

func recordQualityGateValidation(st *state.StateStore, record qualitygate.RunRecord) {
	evidence := ""
	if record.Log != "" {
		evidence = filepath.ToSlash(filepath.Join(qualitygate.RunDirectory, record.ValidationRunID, qualitygate.RunLog))
	}
	st.RecordValidationEvent(state.TaskValidationEvent{
		Source:          "quality-gate",
		Form:            record.Form,
		ValidationRunID: record.ValidationRunID,
		GateClass:       state.ValidationGateClass(record.Form),
		Suite:           record.Form,
		SnapshotID:      state.ValidationSnapshotID(record.Head, record.IndexDigest, record.WorktreeDigest),
		Phase:           "quality-gate",
		Attempt:         state.ValidationAttemptInitial,
		Result:          record.Status,
		ExitCode:        record.ExitCode,
		ExitSource:      record.ExitSource,
		DurationMS:      record.DurationMS,
		Evidence:        evidence,
	})
}

func emitQualityGateStarted(diagnostics io.Writer, runID string, attached bool) error {
	line, err := taskview.MarshalEventLine(qualityGateStartedEvent{
		Type:            "control",
		Event:           "quality_gate_started",
		ValidationRunID: runID,
		Attached:        attached,
	})
	if err != nil {
		return err
	}
	_, err = diagnostics.Write(line)
	return err
}

func qualityGateProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func qualityGateEnv() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GOFLAGS=") || qualityGateSessionTransportEnv(entry) {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "GOFLAGS=")
}

func qualityGateSessionTransportEnv(entry string) bool {
	for _, name := range []string{
		state.ParentActionCodexThreadIDEnv,
		state.ParentActionCodexSessionIDEnv,
		state.SessionRotationClaimIDEnv,
	} {
		if strings.HasPrefix(entry, name+"=") {
			return true
		}
	}
	return false
}
