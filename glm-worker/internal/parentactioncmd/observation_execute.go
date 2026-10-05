package parentactioncmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/observationexec"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentcontinuation"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type observationExecuteOutput struct {
	Status        string   `json:"status"`
	Result        string   `json:"result"`
	ExecutionID   string   `json:"execution_id"`
	Operation     string   `json:"operation"`
	ParamsDigest  string   `json:"params_digest"`
	Detail        string   `json:"detail,omitempty"`
	Artifacts     []string `json:"artifacts,omitempty"`
	ExitCode      int      `json:"exit_code,omitempty"`
	ExitSource    string   `json:"exit_source,omitempty"`
	DurationMS    int64    `json:"duration_ms"`
	DecisionRound int      `json:"decision_round"`
}

type observationShadowEvalArtifacts struct {
	Input      string `json:"input"`
	Decisions  string `json:"decisions,omitempty"`
	Comparison string `json:"comparison"`
}

type observationShadowEvalFailure struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
}

type observationShadowEvalOutput struct {
	TypedSchemaValid bool                           `json:"typed_schema_valid"`
	ValidationErrors []string                       `json:"validation_errors"`
	ShadowFailure    *observationShadowEvalFailure  `json:"shadow_failure"`
	Artifacts        observationShadowEvalArtifacts `json:"artifacts"`
}

type observationExecutionOutcome struct {
	Status     string
	Detail     string
	Artifacts  []string
	ExitCode   int
	ExitSource string
}

type observationExecutionPlan struct {
	admission    parentcontinuation.ObservationCapabilityAdmission
	request      observationexec.Request
	referenceAbs string
	moduleDir    string
}

const (
	observationExecuteUsage             = "usage: glm-parent-action observation-execute <token>"
	observationExecuteDetailLimit       = 2048
	observationShadowOutputLimit        = 1 << 20
	observationStatePollInterval        = 200 * time.Millisecond
	observationFinalizationLockTimeout  = 10 * time.Second
)

var (
	resolveObservationShadowEvalWorker    = resolveGLMWorker
	dispatchObservationExecutionForAction = dispatchObservationExecution
)

func prepareObservationExecuteAdmission(cfg config.AppConfig) error {
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	lock, err := repolock.Acquire(st.LockPath())
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	_, err = parentcontinuation.CurrentObservationCapabilityAdmission(st)
	return err
}

func executeObservationExecuteAction(cfg config.AppConfig, args []string, stdout io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("%s", observationExecuteUsage)
	}
	if err := persistParentCodexIdentity(cfg); err != nil {
		return err
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	lock, err := repolock.Acquire(st.LockPath())
	if err != nil {
		return err
	}
	plan, snapshot, executionID, startedAt, err := beginObservationExecution(cfg, st, args[1])
	if closeErr := lock.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return err
	}

	ctx, stop := observationExecutionContext(st, plan.admission)
	started := time.Now()
	outcome := dispatchObservationExecutionForAction(ctx, cfg, st, plan, executionID)
	stop()

	finalizeCtx, cancelFinalize := context.WithTimeout(context.Background(), observationFinalizationLockTimeout)
	lock, err = repolock.AcquireContext(finalizeCtx, st.LockPath())
	cancelFinalize()
	if err != nil {
		return fmt.Errorf("observation post-dispatch finalization lock unavailable: %w", err)
	}
	defer func() { _ = lock.Close() }()
	if boundaryErr := verifyObservationExecutionBoundary(st, plan.admission); boundaryErr != nil {
		if recoveryErr := st.ResolveObservationExecutionIndeterminate(
			executionID,
			"observation execution boundary changed after dispatch; the operation may have run and automatic replay is refused: "+boundaryErr.Error(),
			time.Now().UTC(),
		); recoveryErr != nil {
			return fmt.Errorf("%w; additionally failed to resolve in-flight observation: %v", boundaryErr, recoveryErr)
		}
		return boundaryErr
	}
	record := newObservationExecutionRecord(plan, outcome, executionID, snapshot, startedAt, time.Since(started).Milliseconds())
	if err := st.CompleteObservationExecution(record); err != nil {
		if recoveryErr := st.ResolveObservationExecutionIndeterminate(
			executionID,
			"observation dispatch completed but durable completion could not be established; automatic replay is refused: "+err.Error(),
			time.Now().UTC(),
		); recoveryErr != nil {
			return fmt.Errorf("%w; additionally failed to resolve in-flight observation: %v", err, recoveryErr)
		}
		return err
	}
	return encodeObservationExecuteOutput(stdout, record)
}

func beginObservationExecution(
	cfg config.AppConfig,
	st *state.StateStore,
	token string,
) (observationExecutionPlan, state.GitSnapshot, string, time.Time, error) {
	plan, err := prepareObservationExecutionPlan(cfg, st, token)
	if err != nil {
		return observationExecutionPlan{}, state.GitSnapshot{}, "", time.Time{}, err
	}
	snapshot, err := state.CaptureGitSnapshot(cfg.RepoRoot)
	if err != nil {
		return observationExecutionPlan{}, state.GitSnapshot{}, "", time.Time{}, fmt.Errorf("observation実行前のrepository snapshotを取得できません: %w", err)
	}
	executionID, err := state.NewUUID()
	if err != nil {
		return observationExecutionPlan{}, state.GitSnapshot{}, "", time.Time{}, err
	}
	startedAt := time.Now().UTC()
	if err := st.BeginObservationExecution(newObservationExecutionInFlightRecord(plan, executionID, snapshot, startedAt)); err != nil {
		return observationExecutionPlan{}, state.GitSnapshot{}, "", time.Time{}, err
	}
	return plan, snapshot, executionID, startedAt, nil
}

func prepareObservationExecutionPlan(cfg config.AppConfig, st *state.StateStore, token string) (observationExecutionPlan, error) {
	admission, err := parentcontinuation.CurrentObservationCapabilityAdmission(st)
	if err != nil {
		return observationExecutionPlan{}, err
	}
	payload, err := parentaction.Consume(cfg.RepoRoot, string(parentaction.ActionObservationExecute), token)
	if err != nil {
		return observationExecutionPlan{}, err
	}
	request, err := observationexec.ParseRequest(payload)
	if err != nil {
		return observationExecutionPlan{}, err
	}
	if err := st.RecoverStaleObservationExecutions(time.Now().UTC()); err != nil {
		return observationExecutionPlan{}, err
	}
	if err := rejectDuplicateObservationExecution(st, request, admission.Lifecycle.Round); err != nil {
		return observationExecutionPlan{}, err
	}
	referenceAbs, err := observationexec.ValidateReferenceLocator(st.ArtifactDir(admission.Lifecycle.TaskID), request.Reference)
	if err != nil {
		return observationExecutionPlan{}, err
	}
	moduleDir := ""
	if request.Operation.IsGoTest() {
		moduleDir, err = observationexec.ValidateGoTestWorkingDir(cfg.RepoRoot, request.WorkingDir)
		if err != nil {
			return observationExecutionPlan{}, err
		}
	}
	return observationExecutionPlan{admission: admission, request: request, referenceAbs: referenceAbs, moduleDir: moduleDir}, nil
}

func rejectDuplicateObservationExecution(st *state.StateStore, request observationexec.Request, round int) error {
	duplicate, err := st.HasObservationExecution(string(request.Operation), request.Digest(), round)
	if err != nil {
		return err
	}
	if duplicate {
		return fmt.Errorf("同一decision round内の同一operation・parameter再実行はmachineが拒否します(%s)", request.Operation)
	}
	return nil
}

func newObservationExecutionInFlightRecord(
	plan observationExecutionPlan,
	executionID string,
	snapshot state.GitSnapshot,
	startedAt time.Time,
) state.ObservationExecutionRecord {
	return state.ObservationExecutionRecord{
		ExecutionID:      executionID,
		TaskID:           plan.admission.Lifecycle.TaskID,
		Operation:        string(plan.request.Operation),
		ParamsDigest:     plan.request.Digest(),
		Status:           state.ObservationExecutionStatusInFlight,
		DeadlineMS:       plan.request.ResolvedDeadlineMS(),
		Head:             snapshot.Head,
		IndexDigest:      snapshot.IndexDigest,
		WorktreeDigest:   snapshot.WorktreeDigest,
		DecisionRound:    plan.admission.Lifecycle.Round,
		StartedAtRFC3339: startedAt.Format(time.RFC3339Nano),
	}
}

func newObservationExecutionRecord(
	plan observationExecutionPlan,
	outcome observationExecutionOutcome,
	executionID string,
	snapshot state.GitSnapshot,
	startedAt time.Time,
	durationMS int64,
) state.ObservationExecutionRecord {
	return state.ObservationExecutionRecord{
		ExecutionID:        executionID,
		TaskID:             plan.admission.Lifecycle.TaskID,
		Operation:          string(plan.request.Operation),
		ParamsDigest:       plan.request.Digest(),
		Status:             outcome.Status,
		Detail:             boundedObservationDetail(outcome.Detail),
		Artifacts:          outcome.Artifacts,
		ExitCode:           outcome.ExitCode,
		ExitSource:         outcome.ExitSource,
		DurationMS:         durationMS,
		DeadlineMS:         plan.request.ResolvedDeadlineMS(),
		Head:               snapshot.Head,
		IndexDigest:        snapshot.IndexDigest,
		WorktreeDigest:     snapshot.WorktreeDigest,
		DecisionRound:      plan.admission.Lifecycle.Round,
		StartedAtRFC3339:   startedAt.Format(time.RFC3339Nano),
		CompletedAtRFC3339: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func encodeObservationExecuteOutput(stdout io.Writer, record state.ObservationExecutionRecord) error {
	return json.NewEncoder(stdout).Encode(observationExecuteOutput{
		Status:        "executed",
		Result:        record.Status,
		ExecutionID:   record.ExecutionID,
		Operation:     record.Operation,
		ParamsDigest:  record.ParamsDigest,
		Detail:        record.Detail,
		Artifacts:     record.Artifacts,
		ExitCode:      record.ExitCode,
		ExitSource:    record.ExitSource,
		DurationMS:    record.DurationMS,
		DecisionRound: record.DecisionRound,
	})
}

func observationExecutionContext(st *state.StateStore, admission parentcontinuation.ObservationCapabilityAdmission) (context.Context, context.CancelFunc) {
	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	ctx, cancel := context.WithCancel(signalCtx)
	go watchObservationExecutionBoundary(ctx, cancel, st, admission)
	return ctx, func() {
		cancel()
		stopSignals()
	}
}

func watchObservationExecutionBoundary(
	ctx context.Context,
	cancel context.CancelFunc,
	st *state.StateStore,
	admission parentcontinuation.ObservationCapabilityAdmission,
) {
	ticker := time.NewTicker(observationStatePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lock, err := repolock.Acquire(st.LockPath())
			if errors.Is(err, repolock.ErrRepoLockHeld) {
				continue
			}
			if err != nil {
				cancel()
				return
			}
			verifyErr := verifyObservationExecutionBoundary(st, admission)
			closeErr := lock.Close()
			if verifyErr != nil || closeErr != nil {
				cancel()
				return
			}
		}
	}
}

func verifyObservationExecutionBoundary(st *state.StateStore, expected parentcontinuation.ObservationCapabilityAdmission) error {
	if err := parentcontinuation.ValidateObservationCapabilityAdmission(st, expected); err != nil {
		return fmt.Errorf("observation execution boundary changed: %w", err)
	}
	return nil
}

func dispatchObservationExecution(
	ctx context.Context,
	cfg config.AppConfig,
	st *state.StateStore,
	plan observationExecutionPlan,
	executionID string,
) observationExecutionOutcome {
	if plan.request.Operation == observationexec.OperationShadowEval {
		return runObservationShadowEval(ctx, cfg, plan.admission.Lifecycle.TaskID, plan.referenceAbs, plan.request)
	}
	outcome := observationexec.RunIsolatedGoTestContext(ctx, observationexec.GoTestInput{
		ModuleDir:   plan.moduleDir,
		ArtifactDir: st.ArtifactDir(plan.admission.Lifecycle.TaskID),
		ExecutionID: executionID,
		Race:        plan.request.Operation == observationexec.OperationGoTestRace,
		DeadlineMS:  plan.request.ResolvedDeadlineMS(),
	})
	return observationExecutionOutcome{
		Status:     outcome.Status,
		Detail:     outcome.Detail,
		Artifacts:  observationGoTestArtifacts(outcome.LogPath),
		ExitCode:   outcome.ExitCode,
		ExitSource: outcome.ExitSource,
	}
}

func observationGoTestArtifacts(logPath string) []string {
	if logPath == "" {
		return nil
	}
	return []string{logPath}
}

func runObservationShadowEval(
	ctx context.Context,
	cfg config.AppConfig,
	taskID string,
	referenceAbs string,
	request observationexec.Request,
) observationExecutionOutcome {
	worker, err := resolveObservationShadowEvalWorker()
	if err != nil {
		return observationShadowEvalWrapperFailure(err.Error())
	}
	decoded, failureDetail, result := runShadowEvalWorkerChild(ctx, cfg, worker, taskID, referenceAbs, request)
	if result.Err != nil {
		source := "wrapper"
		if result.ExitSource == observationexec.BoundedCommandExitCancelled || result.ExitSource == observationexec.BoundedCommandExitDeadline {
			source = result.ExitSource
		}
		return observationShadowEvalFailureWithSource(failureDetail, result.ExitCode, source)
	}
	return shadowEvalOutcomeFromOutput(decoded)
}

func runShadowEvalWorkerChild(
	ctx context.Context,
	cfg config.AppConfig,
	worker string,
	taskID string,
	referenceAbs string,
	request observationexec.Request,
) (observationShadowEvalOutput, string, observationexec.BoundedCommandResult) {
	args := []string{"--shadow-eval", taskID}
	if referenceAbs != "" {
		args = append(args, "--reference", referenceAbs)
	}
	result := observationexec.RunBoundedCommand(
		ctx,
		cfg.RepoRoot,
		worker,
		args,
		time.Duration(request.ResolvedDeadlineMS())*time.Millisecond,
		observationShadowOutputLimit,
	)
	if result.Err != nil {
		detail := "shadow-eval machine実行が失敗しました"
		if stderr := strings.TrimSpace(string(result.Stderr)); stderr != "" {
			detail += ": " + stderr
		}
		if result.StderrTruncated {
			detail += fmt.Sprintf("; stderr truncated(total=%d)", result.StderrTotal)
		}
		return observationShadowEvalOutput{}, detail, result
	}
	if result.StdoutTruncated {
		result.Err = fmt.Errorf("shadow-eval machine output exceeded bounded capture(total=%d)", result.StdoutTotal)
		result.ExitCode = 1
		result.ExitSource = observationexec.BoundedCommandExitWrapper
		return observationShadowEvalOutput{}, result.Err.Error(), result
	}
	var decoded observationShadowEvalOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(result.Stdout))), &decoded); err != nil {
		result.Err = err
		result.ExitCode = 1
		result.ExitSource = observationexec.BoundedCommandExitWrapper
		return observationShadowEvalOutput{}, "shadow-eval machine出力をdecodeできません", result
	}
	return decoded, "", result
}

func observationShadowEvalWrapperFailure(detail string) observationExecutionOutcome {
	return observationShadowEvalFailureWithSource(detail, 1, "wrapper")
}

func observationShadowEvalFailureWithSource(detail string, exitCode int, source string) observationExecutionOutcome {
	return observationExecutionOutcome{
		Status:     observationexec.StatusFail,
		Detail:     boundedObservationDetail(detail),
		ExitCode:   exitCode,
		ExitSource: source,
	}
}

func shadowEvalOutcomeFromOutput(decoded observationShadowEvalOutput) observationExecutionOutcome {
	artifacts := shadowEvalArtifactPaths(decoded.Artifacts)
	switch {
	case decoded.ShadowFailure != nil:
		return observationExecutionOutcome{
			Status:     observationexec.StatusFail,
			Detail:     boundedObservationDetail("shadow failure(" + decoded.ShadowFailure.Kind + "): " + decoded.ShadowFailure.Detail),
			Artifacts:  artifacts,
			ExitCode:   1,
			ExitSource: "target",
		}
	case !decoded.TypedSchemaValid || len(decoded.ValidationErrors) > 0:
		return observationExecutionOutcome{
			Status:     observationexec.StatusFail,
			Detail:     boundedObservationDetail("typed schema検証失敗: " + strings.Join(decoded.ValidationErrors, "; ")),
			Artifacts:  artifacts,
			ExitCode:   1,
			ExitSource: "target",
		}
	default:
		return observationExecutionOutcome{
			Status:     observationexec.StatusPass,
			Artifacts:  artifacts,
			ExitSource: "target",
		}
	}
}

func shadowEvalArtifactPaths(artifacts observationShadowEvalArtifacts) []string {
	paths := make([]string, 0, 3)
	for _, candidate := range []string{artifacts.Input, artifacts.Decisions, artifacts.Comparison} {
		if candidate != "" {
			paths = append(paths, filepath.Clean(candidate))
		}
	}
	return paths
}

func boundedObservationDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	if len(detail) > observationExecuteDetailLimit {
		return detail[:observationExecuteDetailLimit]
	}
	return detail
}
