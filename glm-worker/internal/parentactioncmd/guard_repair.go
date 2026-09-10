package parentactioncmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/guardrepair"
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
	before, beforeErr := st.LoadGuardRepairRecord()
	initialStdout, initialStderr, initialErr := executeInitialResume(cfg, extraEnv)
	if initialErr == nil {
		copyOutput(stdout, initialStdout)
		copyOutput(stderr, initialStderr)
		return nil
	}

	record, ok := currentGuardRepairRecord(st)
	if !ok {
		copyOutput(stdout, initialStdout)
		copyOutput(stderr, initialStderr)
		return initialErr
	}
	if repeatedRequestedRepair(before, beforeErr, record) {
		copyOutput(stdout, initialStdout)
		copyOutput(stderr, initialStderr)
		return fmt.Errorf("guard repair strategy already requested for unchanged failure and evidence: %w", initialErr)
	}

	record, err := prepareGuardRepairForResume(cfg, st, record)
	if err != nil {
		return errors.Join(initialErr, err)
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

func repeatedRequestedRepair(before state.GuardRepairRecord, beforeErr error, current state.GuardRepairRecord) bool {
	return beforeErr == nil && before.Status == state.GuardRepairRequested && current.Status == state.GuardRepairRequested && sameGuardRepairRecord(before, current)
}

func prepareGuardRepairForResume(cfg config.AppConfig, st *state.StateStore, record state.GuardRepairRecord) (state.GuardRepairRecord, error) {
	switch record.Status {
	case state.GuardRepairRequested:
		return performBoundedGuardRepair(cfg, st, record)
	case state.GuardRepairReady:
		return validateReadyGuardRepair(cfg, record)
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

func validateReadyGuardRepair(cfg config.AppConfig, record state.GuardRepairRecord) (state.GuardRepairRecord, error) {
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
	if err := integrateGuardRepairCandidate(cfg, st, record, origin, candidate); err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}
	repairedDigest, err := guardrepair.RelevantDigest(cfg.RepoRoot)
	if err != nil {
		return record, markGuardRepairFailed(st, record, err)
	}
	if repairedDigest == record.RelevantDigest {
		return record, markGuardRepairFailed(st, record, fmt.Errorf("guard repair produced no relevant source change"))
	}
	record.Status = state.GuardRepairReady
	record.RepairedDigest = repairedDigest
	return record, st.SaveGuardRepairRecord(record)
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
) error {
	if err := validateOriginalRepairBoundary(cfg, record, origin.boundary); err != nil {
		return err
	}
	if err := copyGuardRepairChanges(candidate.worktree, cfg.RepoRoot, candidate.changed); err != nil {
		return err
	}
	checkpoint, err := currentGuardRepairCheckpoint(st, record, origin.checkpoint.Phase)
	if err != nil {
		return err
	}
	postRepairDirty, err := state.CaptureStopDirtyFiles(cfg.RepoRoot)
	if err != nil {
		return err
	}
	checkpoint.StopDirtyFiles = postRepairDirty
	return st.SaveResumeCheckpoint(checkpoint)
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
	worker, cleanup, err := buildGuardRepairWorker(cfg)
	if err != nil {
		return markGuardRepairFailed(st, record, errors.Join(initialErr, err))
	}
	defer cleanup()
	env := appendEnv(extraEnv, state.GuardRepairParentActionEnv, state.GuardRepairRebuiltResume)
	resumeErr := runResolvedWorker(worker, cfg.RepoRoot, []string{"--resume"}, nil, stdout, stderr, env)
	if st.TaskStatus() == state.TaskStatusGuardRecoverable {
		failure := fmt.Errorf("repaired worker did not leave guard-recoverable state")
		return markGuardRepairFailed(st, record, errors.Join(resumeErr, failure))
	}
	record.Status = state.GuardRepairComplete
	record.OriginalResumeObserved = true
	if err := st.SaveGuardRepairRecord(record); err != nil {
		return errors.Join(resumeErr, err)
	}
	return resumeErr
}

func sameGuardRepairRecord(a, b state.GuardRepairRecord) bool {
	return a.TaskID == b.TaskID && a.Fingerprint == b.Fingerprint && a.Strategy == b.Strategy &&
		a.RelevantDigest == b.RelevantDigest && a.UpdatedAt.Equal(b.UpdatedAt)
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
