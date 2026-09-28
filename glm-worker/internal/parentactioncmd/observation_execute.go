package parentactioncmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/observationexec"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
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
	admission    state.ObservationExecutionAdmission
	request      observationexec.Request
	referenceAbs string
	moduleDir    string
}

const observationExecuteUsage = "usage: glm-parent-action observation-execute <token>"

const observationExecuteDetailLimit = 2048

var resolveObservationShadowEvalWorker = resolveGLMWorker

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
	_, err = st.ObservationExecuteAdmission()
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
	defer func() { _ = lock.Close() }()
	plan, err := prepareObservationExecutionPlan(cfg, st, args[1])
	if err != nil {
		return err
	}
	snapshot, err := state.CaptureGitSnapshot(cfg.RepoRoot)
	if err != nil {
		return fmt.Errorf("observation実行前のrepository snapshotを取得できません: %w", err)
	}
	executionID, err := state.NewUUID()
	if err != nil {
		return err
	}
	started := time.Now()
	outcome := dispatchObservationExecution(cfg, st, plan, executionID)
	record := newObservationExecutionRecord(plan, outcome, executionID, snapshot, time.Since(started).Milliseconds())
	if err := st.AppendObservationExecution(record); err != nil {
		return err
	}
	return encodeObservationExecuteOutput(stdout, record)
}

func prepareObservationExecutionPlan(cfg config.AppConfig, st *state.StateStore, token string) (observationExecutionPlan, error) {
	admission, err := st.ObservationExecuteAdmission()
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
	if err := rejectDuplicateObservationExecution(st, request, admission.Round); err != nil {
		return observationExecutionPlan{}, err
	}
	referenceAbs, err := observationexec.ValidateReferenceLocator(st.ArtifactDir(admission.TaskID), request.Reference)
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

func newObservationExecutionRecord(
	plan observationExecutionPlan,
	outcome observationExecutionOutcome,
	executionID string,
	snapshot state.GitSnapshot,
	durationMS int64,
) state.ObservationExecutionRecord {
	return state.ObservationExecutionRecord{
		ExecutionID:        executionID,
		Operation:          string(plan.request.Operation),
		ParamsDigest:       plan.request.Digest(),
		Status:             outcome.Status,
		Detail:             boundedObservationDetail(outcome.Detail),
		Artifacts:          outcome.Artifacts,
		ExitCode:           outcome.ExitCode,
		ExitSource:         outcome.ExitSource,
		DurationMS:         durationMS,
		Head:               snapshot.Head,
		IndexDigest:        snapshot.IndexDigest,
		WorktreeDigest:     snapshot.WorktreeDigest,
		DecisionRound:      plan.admission.Round,
		CompletedAtRFC3339: time.Now().UTC().Format(time.RFC3339),
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

func dispatchObservationExecution(
	cfg config.AppConfig,
	st *state.StateStore,
	plan observationExecutionPlan,
	executionID string,
) observationExecutionOutcome {
	if plan.request.Operation == observationexec.OperationShadowEval {
		return runObservationShadowEval(cfg, plan.admission.TaskID, plan.referenceAbs, plan.request)
	}
	outcome := observationexec.RunIsolatedGoTest(observationexec.GoTestInput{
		ModuleDir:   plan.moduleDir,
		ArtifactDir: st.ArtifactDir(plan.admission.TaskID),
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
	cfg config.AppConfig,
	taskID string,
	referenceAbs string,
	request observationexec.Request,
) observationExecutionOutcome {
	worker, err := resolveObservationShadowEvalWorker()
	if err != nil {
		return observationShadowEvalWrapperFailure(err.Error())
	}
	decoded, failureDetail, runErr := runShadowEvalWorkerChild(cfg, worker, taskID, referenceAbs, request)
	if runErr != nil {
		return observationShadowEvalWrapperFailureWithCode(failureDetail, shadowEvalExitCode(runErr))
	}
	return shadowEvalOutcomeFromOutput(decoded)
}

func runShadowEvalWorkerChild(
	cfg config.AppConfig,
	worker string,
	taskID string,
	referenceAbs string,
	request observationexec.Request,
) (observationShadowEvalOutput, string, error) {
	args := []string{"--shadow-eval", taskID}
	if referenceAbs != "" {
		args = append(args, "--reference", referenceAbs)
	}
	var commandOut bytes.Buffer
	var commandErr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(request.ResolvedDeadlineMS())*time.Millisecond)
	defer cancel()
	runErr := runResolvedWorkerWithContext(ctx, worker, cfg.RepoRoot, args, nil, &commandOut, &commandErr)
	if runErr != nil {
		return observationShadowEvalOutput{}, "shadow-eval machine実行が失敗しました: " + strings.TrimSpace(commandErr.String()), runErr
	}
	var decoded observationShadowEvalOutput
	if err := json.Unmarshal(bytes.TrimSpace(commandOut.Bytes()), &decoded); err != nil {
		return observationShadowEvalOutput{}, "shadow-eval machine出力をdecodeできません", err
	}
	return decoded, "", nil
}

func shadowEvalExitCode(runErr error) int {
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return childExitCode(exitErr)
	}
	return 1
}

func observationShadowEvalWrapperFailure(detail string) observationExecutionOutcome {
	return observationShadowEvalWrapperFailureWithCode(detail, 1)
}

func observationShadowEvalWrapperFailureWithCode(detail string, exitCode int) observationExecutionOutcome {
	return observationExecutionOutcome{
		Status:     observationexec.StatusFail,
		Detail:     boundedObservationDetail(detail),
		ExitCode:   exitCode,
		ExitSource: "wrapper",
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

func runResolvedWorkerWithContext(
	ctx context.Context,
	worker, workingDir string,
	args []string,
	stdin io.Reader,
	stdout, stderr io.Writer,
) error {
	command := exec.CommandContext(ctx, worker, args...)
	command.Dir = workingDir
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}
