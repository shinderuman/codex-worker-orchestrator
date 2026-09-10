package parentactioncmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/guardrepair"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type guardRepairOrigin struct {
	checkpoint state.ResumeCheckpoint
	boundary   state.GitSnapshot
}

type guardRepairCandidate struct {
	worktree string
	changed  []string
	cleanup  func()
}

func executeResumeWithGuardRepair(cfg config.AppConfig, stdout, stderr io.Writer, extraEnv []string) error {
	st := state.AttachStateStore(cfg)
	if record, ok := reusableGuardRepairRecord(cfg, st); ok {
		return resumeWithRepairedWorker(cfg, st, record, stdout, stderr, extraEnv, errors.New(record.Failure))
	}

	initialStdout, initialStderr, initialErr := executeInitialResume(cfg, extraEnv)
	if initialErr == nil {
		copyOutput(stdout, initialStdout)
		copyOutput(stderr, initialStderr)
		return nil
	}

	record, ok := reusableGuardRepairRecord(cfg, st)
	if !ok {
		copyOutput(stdout, initialStdout)
		copyOutput(stderr, initialStderr)
		return initialErr
	}
	return resumeWithRepairedWorker(cfg, st, record, stdout, stderr, extraEnv, initialErr)
}

func executeInitialResume(cfg config.AppConfig, extraEnv []string) (*bytes.Buffer, *bytes.Buffer, error) {
	var stdout, stderr bytes.Buffer
	env := appendEnv(extraEnv, state.GuardRepairParentActionEnv, state.GuardRepairParentActionResume)
	err := runWorker(cfg.RepoRoot, []string{"--resume"}, nil, &stdout, &stderr, env)
	return &stdout, &stderr, err
}

func currentGuardRepairRecord(st *state.StateStore) (state.GuardRepairRecord, bool) {
	if st.TaskStatus() != state.TaskStatusGuardRecoverable {
		return state.GuardRepairRecord{}, false
	}
	record, err := st.LoadGuardRepairRecord()
	if err != nil || record.TaskID != st.ReadOr("task.id", "") {
		return state.GuardRepairRecord{}, false
	}
	return record, true
}

func reusableGuardRepairRecord(cfg config.AppConfig, st *state.StateStore) (state.GuardRepairRecord, bool) {
	record, ok := currentGuardRepairRecord(st)
	if !ok {
		return state.GuardRepairRecord{}, false
	}
	digest, err := guardrepair.RelevantDigest(cfg.RepoRoot)
	if err != nil {
		return state.GuardRepairRecord{}, false
	}
	if record.Status == state.GuardRepairRunning {
		return record, true
	}
	if record.Status == state.GuardRepairReady || record.Status == state.GuardRepairComplete ||
		record.Status == state.GuardRepairFailed && record.RepairedDigest != "" {
		return record, record.RepairedDigest != "" && digest == record.RepairedDigest
	}
	return record, digest == record.RelevantDigest
}

func prepareGuardRepairForResume(cfg config.AppConfig, st *state.StateStore, record state.GuardRepairRecord) (state.GuardRepairRecord, error) {
	switch record.Status {
	case state.GuardRepairRequested:
		return performBoundedGuardRepair(cfg, st, record)
	case state.GuardRepairReady:
		return validateReadyGuardRepair(cfg, st, record)
	case state.GuardRepairRunning:
		return record, fmt.Errorf("guard repair strategy is already running; unchanged recovery is not repeated")
	case state.GuardRepairFailed:
		return record, fmt.Errorf("guard repair strategy already failed for this evidence; unchanged recovery is not repeated")
	case state.GuardRepairComplete:
		return record, fmt.Errorf("guard repair is already complete but the original task is still not resumable")
	default:
		return record, fmt.Errorf("unsupported guard repair status %q", record.Status)
	}
}

func validateReadyGuardRepair(cfg config.AppConfig, st *state.StateStore, record state.GuardRepairRecord) (state.GuardRepairRecord, error) {
	checkpoint, err := currentGuardRepairCheckpoint(st, record, record.Phase)
	if err != nil || checkpoint.StopGitSnapshot == nil {
		return record, fmt.Errorf("guard repair ready state no longer matches original checkpoint")
	}
	if err := validateGuardRepairDirtyOrigin(cfg.RepoRoot, checkpoint); err != nil {
		return record, err
	}
	currentDigest, err := guardrepair.RelevantDigest(cfg.RepoRoot)
	if err != nil {
		return record, err
	}
	if currentDigest != record.RepairedDigest {
		return record, fmt.Errorf("guard repair ready state no longer matches repaired source")
	}
	return record, nil
}

func performBoundedGuardRepair(cfg config.AppConfig, st *state.StateStore, record state.GuardRepairRecord) (state.GuardRepairRecord, error) {
	origin, err := validateGuardRepairOrigin(cfg, st, record)
	if err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}
	if err := markGuardRepairRunning(st, &record); err != nil {
		return record, err
	}
	candidate, err := prepareGuardRepairCandidate(cfg, record)
	if err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}
	defer candidate.cleanup()
	rollback, err := integrateGuardRepairCandidate(cfg, st, record, origin, candidate)
	if err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}
	repairedDigest, err := guardrepair.RelevantDigest(cfg.RepoRoot)
	if err != nil {
		return record, markGuardRepairFailed(st, record, errors.Join(err, rollback()))
	}
	if repairedDigest == record.RelevantDigest {
		return record, markGuardRepairFailed(st, record, errors.Join(fmt.Errorf("guard repair produced no relevant source change"), rollback()))
	}
	record.Status = state.GuardRepairReady
	record.RepairedDigest = repairedDigest
	if err := st.SaveGuardRepairRecord(record); err != nil {
		record.RepairedDigest = ""
		return record, markGuardRepairFailed(st, record, errors.Join(err, rollback()))
	}
	return record, nil
}

func markGuardRepairRunning(st *state.StateStore, record *state.GuardRepairRecord) error {
	record.Status = state.GuardRepairRunning
	return st.SaveGuardRepairRecord(*record)
}

func prepareGuardRepairCandidate(cfg config.AppConfig, record state.GuardRepairRecord) (guardRepairCandidate, error) {
	worktree, cleanup, err := createGuardRepairWorktree(cfg)
	if err != nil {
		return guardRepairCandidate{}, err
	}
	failed := true
	defer func() {
		if failed {
			cleanup()
		}
	}()
	if err := overlayGuardRepairWorktree(cfg.RepoRoot, worktree); err != nil {
		return guardRepairCandidate{}, err
	}
	before, err := captureGuardRepairWorktree(worktree)
	if err != nil {
		return guardRepairCandidate{}, err
	}
	if err := invokeGuardRepairWorker(cfg, worktree, record); err != nil {
		return guardRepairCandidate{}, err
	}
	after, err := captureGuardRepairWorktree(worktree)
	if err != nil {
		return guardRepairCandidate{}, err
	}
	changed := guardrepair.ChangedDirtyPaths(before.dirty, after.dirty)
	if err := validateGuardRepairChanges(worktree, changed, before, after); err != nil {
		return guardRepairCandidate{}, err
	}
	if err := validateGuardRepairTests(worktree, changed); err != nil {
		return guardRepairCandidate{}, err
	}
	if err := reviewGuardRepair(cfg, worktree, changed); err != nil {
		return guardRepairCandidate{}, err
	}
	failed = false
	return guardRepairCandidate{worktree: worktree, changed: changed, cleanup: cleanup}, nil
}

func integrateGuardRepairCandidate(
	cfg config.AppConfig,
	st *state.StateStore,
	record state.GuardRepairRecord,
	origin guardRepairOrigin,
	candidate guardRepairCandidate,
) (func() error, error) {
	if err := validateOriginalRepairBoundary(cfg, record, origin.boundary); err != nil {
		return nil, err
	}
	rollbackFiles, err := copyGuardRepairChangesWithRollback(candidate.worktree, cfg.RepoRoot, candidate.changed)
	if err != nil {
		return nil, err
	}
	rollback := func() error {
		return errors.Join(rollbackFiles(), st.SaveResumeCheckpoint(origin.checkpoint))
	}
	checkpoint, err := currentGuardRepairCheckpoint(st, record, origin.checkpoint.Phase)
	if err != nil {
		return nil, errors.Join(err, rollback())
	}
	postRepairDirty, err := state.CaptureStopDirtyFiles(cfg.RepoRoot)
	if err != nil {
		return nil, errors.Join(err, rollback())
	}
	checkpoint.StopDirtyFiles = postRepairDirty
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		return nil, errors.Join(err, rollback())
	}
	return rollback, nil
}

func validateOriginalRepairBoundary(cfg config.AppConfig, record state.GuardRepairRecord, original state.GitSnapshot) error {
	current, err := state.CaptureRepositoryBoundarySnapshot(cfg.RepoRoot)
	if err != nil {
		return err
	}
	if !sameRepositoryBoundary(original, current) {
		return fmt.Errorf("original repository changed during guard repair")
	}
	currentDigest, err := guardrepair.RelevantDigest(cfg.RepoRoot)
	if err != nil {
		return err
	}
	if currentDigest != record.RelevantDigest {
		return fmt.Errorf("guard repair source changed during repair")
	}
	return nil
}

func currentGuardRepairCheckpoint(st *state.StateStore, record state.GuardRepairRecord, phase string) (state.ResumeCheckpoint, error) {
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil {
		return state.ResumeCheckpoint{}, err
	}
	if st.ReadOr("task.id", "") != record.TaskID || checkpoint.StopKind != state.ResumeStopGuardRecoverable || checkpoint.Phase != phase {
		return state.ResumeCheckpoint{}, fmt.Errorf("original guard recovery checkpoint changed before repair integration")
	}
	return checkpoint, nil
}

func validateGuardRepairOrigin(cfg config.AppConfig, st *state.StateStore, record state.GuardRepairRecord) (guardRepairOrigin, error) {
	if record.Strategy != guardrepair.StrategySourcePatch {
		return guardRepairOrigin{}, fmt.Errorf("unsupported guard repair strategy %q", record.Strategy)
	}
	if st.ReadOr("task.id", "") != record.TaskID || st.TaskStatus() != state.TaskStatusGuardRecoverable {
		return guardRepairOrigin{}, fmt.Errorf("guard repair requires the original guard-recoverable task")
	}
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil || checkpoint.StopKind != state.ResumeStopGuardRecoverable || checkpoint.StopGitSnapshot == nil {
		return guardRepairOrigin{}, fmt.Errorf("guard repair requires the original guard-recoverable checkpoint")
	}
	if err := validateGuardRepairDirtyOrigin(cfg.RepoRoot, checkpoint); err != nil {
		return guardRepairOrigin{}, err
	}
	currentDigest, err := guardrepair.RelevantDigest(cfg.RepoRoot)
	if err != nil {
		return guardRepairOrigin{}, err
	}
	if currentDigest != record.RelevantDigest {
		return guardRepairOrigin{}, fmt.Errorf("guard repair evidence no longer matches current source")
	}
	boundary, err := state.CaptureRepositoryBoundarySnapshot(cfg.RepoRoot)
	if err != nil {
		return guardRepairOrigin{}, err
	}
	return guardRepairOrigin{checkpoint: checkpoint, boundary: boundary}, nil
}

func validateGuardRepairDirtyOrigin(repoRoot string, checkpoint state.ResumeCheckpoint) error {
	currentDirty, err := state.CaptureStopDirtyFiles(repoRoot)
	if err != nil {
		return err
	}
	if diff := state.DescribeStopDirtyDiff(checkpoint.StopDirtyFiles, currentDirty); diff != "" {
		return fmt.Errorf("guard repair refuses dirty drift after stop: %s", diff)
	}
	return nil
}

func resumeWithRepairedWorker(
	cfg config.AppConfig,
	st *state.StateStore,
	record state.GuardRepairRecord,
	stdout, stderr io.Writer,
	extraEnv []string,
	initialErr error,
) error {
	lock, err := repolock.AcquireWait(st.LockPath())
	if err != nil {
		return errors.Join(initialErr, err)
	}
	record, err = prepareGuardRepairForResume(cfg, st, record)
	if err != nil {
		return errors.Join(initialErr, err, lock.Close())
	}
	worker, cleanup, err := buildGuardRepairWorker(cfg)
	if err != nil {
		return errors.Join(initialErr, err, lock.Close())
	}
	if err := lock.Close(); err != nil {
		cleanup()
		return errors.Join(initialErr, err)
	}
	defer cleanup()

	env := appendEnv(extraEnv, state.GuardRepairParentActionEnv, state.GuardRepairRebuiltResume)
	resumeErr := runResolvedWorker(worker, cfg.RepoRoot, []string{"--resume"}, nil, stdout, stderr, env)
	lock, err = repolock.AcquireWait(st.LockPath())
	if err != nil {
		return errors.Join(resumeErr, err)
	}
	if st.ReadOr("task.id", "") != record.TaskID {
		return errors.Join(resumeErr, fmt.Errorf("original task changed before guard repair resume completed"), lock.Close())
	}
	if st.TaskStatus() == state.TaskStatusGuardRecoverable {
		failure := markGuardRepairFailed(st, record, errors.Join(resumeErr, fmt.Errorf("repaired worker did not leave guard-recoverable state")))
		return errors.Join(failure, lock.Close())
	}
	record.Status = state.GuardRepairComplete
	record.OriginalResumeObserved = true
	saveErr := st.SaveGuardRepairRecord(record)
	return errors.Join(resumeErr, saveErr, lock.Close())
}

func appendEnv(env []string, key, value string) []string {
	result := append([]string(nil), env...)
	prefix := key + "="
	for i, item := range result {
		if strings.HasPrefix(item, prefix) {
			result[i] = prefix + value
			return result
		}
	}
	return append(result, prefix+value)
}

func copyOutput(dst io.Writer, src *bytes.Buffer) {
	if dst != nil && src != nil {
		_, _ = io.Copy(dst, src)
	}
}

func markGuardRepairFailed(st *state.StateStore, record state.GuardRepairRecord, cause error) error {
	record.Status = state.GuardRepairFailed
	record.OriginalResumeObserved = false
	if err := st.SaveGuardRepairRecord(record); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}
