package workflow

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type ModelRunner interface {
	Run(role state.SessionRole, phase string, model string, readOnly bool, effort string, prompt string, outputPath string) (runner.RunResult, error)
	Probe(model string) (runner.ProbeResult, error)
}

type Workflow struct {
	config                  config.AppConfig
	state                   *state.StateStore
	runner                  ModelRunner
	output                  io.Writer
	temp                    string
	captureSnapshot         func(repoRoot string) (state.GitSnapshot, error)
	captureBoundarySnapshot func(repoRoot string) (state.GitSnapshot, error)
	collectChangedPaths     func(repoRoot, baselineHead string) ([]string, error)
	now                     func() time.Time
	sleep                   func(time.Duration)
	jitter                  func(base time.Duration) time.Duration
	qualityGate             func(root string) (harnesslint.Report, error)
	captureQualitySurface   func(root string) (string, error)
	repoSearch              repoSearchFunc

	stop *runner.StopController

	pendingSnapshot *state.SnapshotDiagnostic

	currentResumeSource string

	pendingRetry *callRetryContext
	lastCallID   string

	lastProducer             state.ParentReviewProducer
	observedInstructionReads map[string]struct{}

	modelCallAttempts int
}

type callDiagnostics struct {
	reportedRisk           string
	providerClassification string
}

type callRetryContext struct {
	callID string
	reason string
}

type modelCallExecution struct {
	runResult     runner.RunResult
	startedAt     time.Time
	completedAt   time.Time
	runErr        error
	recoveryFatal bool
}

type WorkerError struct {
	Phase    string
	ExitCode int
	Tail     string
	Message  string
	cause    error
}

type effectiveRisk struct {
	high   bool
	source string
}

type snapshotEndCheck struct {
	stage          state.SnapshotStage
	loadStart      func() (state.GitSnapshot, error)
	failClosed     func(state.SnapshotStage, state.GitSnapshot, state.GitSnapshot, string, error) error
	loadReason     string
	captureReason  string
	saveReason     string
	mismatchReason string
}

const highRiskValue = "HIGH"

const providerUnavailableDeadline = 3 * time.Hour

const maxTransientProbes = 4

const resultCorrectionPhaseSuffix = "-result-correct"

var transientBackoffSchedule = []time.Duration{
	5 * time.Minute,
	15 * time.Minute,
	45 * time.Minute,
	90 * time.Minute,
}

func NewWorkflow(cfg config.AppConfig, st *state.StateStore, r ModelRunner, output io.Writer) *Workflow {
	return &Workflow{
		config:                  cfg,
		state:                   st,
		runner:                  r,
		output:                  output,
		captureSnapshot:         state.CaptureGitSnapshot,
		captureBoundarySnapshot: state.CaptureRepositoryBoundarySnapshot,
		collectChangedPaths: func(repoRoot, _ string) ([]string, error) {
			return collectTaskChangedPaths(repoRoot, st)
		},
		now:                   time.Now,
		sleep:                 time.Sleep,
		jitter:                boundedBackoffJitter,
		qualityGate:           runRepositoryQualityGate,
		captureQualitySurface: captureQualitySurfaceDigest,
	}
}

func (w *Workflow) AttachStopController(stop *runner.StopController) {
	w.stop = stop
}

func (w *Workflow) stopRequested() bool {
	return w.stop != nil && w.stop.StopRequested()
}

func boundedBackoffJitter(base time.Duration) time.Duration {
	if base <= 0 {
		return base
	}
	return base + time.Duration(rand.Int63n(int64(base)/4+1))
}

func (w *Workflow) withTemp(fn func() error) error {
	temp, err := os.MkdirTemp("", "glm-worker-*")
	if err != nil {
		return err
	}
	w.temp = temp
	defer func() { _ = os.RemoveAll(temp) }()
	return fn()
}

func (e *WorkerError) Error() string {
	if e.Message == "" {
		return "worker error"
	}
	return e.Message
}

func (e *WorkerError) Unwrap() error {
	return e.cause
}

func (w *Workflow) ExecuteNewTask(request string) error {
	return quietWhenParentFileGuardStopped(w.withTemp(func() error {
		if err := w.validateNewTaskStart(); err != nil {
			return err
		}
		activeTaskPath, err := w.initializeNewTask(request)
		if err != nil {
			return err
		}

		decl, err := w.gateExternalFeasibility("worker-new", false)
		if err != nil {
			return err
		}
		pocStage := decl.pocStage()

		prompt := w.newWorkerTaskPrompt(request, activeTaskPath)
		exhaustiveContext, err := w.exhaustiveSearchContext(request, activeTaskPath, state.WorkerRole, 1)
		if err != nil {
			return err
		}
		prompt += exhaustiveContext
		checkpoint := state.ResumeCheckpoint{
			Stage:          state.ResumeStageWorker,
			Phase:          "worker-new",
			Role:           state.WorkerRole,
			Model:          w.config.WorkerModel,
			ReadOnly:       pocStage,
			Effort:         w.config.RoutineEffort,
			Prompt:         prompt,
			OriginalPrompt: prompt,
			Request:        request,
		}
		return w.executeWorkerCheckpoint(request, checkpoint, pocStage)
	}))
}

func (w *Workflow) validateNewTaskStart() error {
	return w.admitNewTask()
}

func (w *Workflow) initializeNewTask(request string) (string, error) {
	claimID := os.Getenv(state.SessionRotationClaimIDEnv)
	threadID := os.Getenv(state.ParentActionCodexThreadIDEnv)
	if err := w.initializeNewTaskState(threadID, claimID); err != nil {
		return "", err
	}
	if err := w.persistParentActionCodexIdentity(); err != nil {
		return "", err
	}
	if err := state.CaptureGitBaseline(w.config, w.state); err != nil {
		return "", err
	}
	if err := w.captureQualitySurfaceBaseline(); err != nil {
		return "", err
	}
	w.recordBaselineRound()
	if err := w.state.Write("last-request", request); err != nil {
		return "", err
	}
	if err := w.state.Remove(repositoryharness.ActivationStateKey); err != nil {
		return "", err
	}

	if _, err := w.pinRepositoryHarnessActivation(); err != nil {
		return "", w.failClosedRepositoryHarness("worker-new", repositoryHarnessGuardSurface.unavailableOutcome(), "repository harness適用境界を評価できません", err)
	}
	activeTaskPath, err := w.resolveAndPinActiveTask()
	if err != nil {
		return "", w.failClosedActiveTaskResolution("worker-new", err)
	}
	return activeTaskPath, nil
}

func (w *Workflow) initializeNewTaskState(threadID, claimID string) error {
	if claimID == "" {
		_, err := w.state.StartNewTask()
		return err
	}
	_, err := w.state.StartSessionRotationTask(threadID, claimID)
	return err
}

func (w *Workflow) ExecuteDecision(decision string) error {
	return quietWhenParentFileGuardStopped(w.withTemp(func() error {
		if err := w.admitParentAction(state.ParentActionDecision); err != nil {
			return err
		}

		request, err := w.state.Read("last-request")
		if err != nil {
			return &WorkerError{Message: "original request is missing"}
		}

		activeTaskPath, err := w.gateDecisionActiveTask()
		if err != nil {
			return err
		}

		decl, err := w.gateExternalFeasibility("worker-decision", true)
		if err != nil {
			return err
		}
		pocStage := decl.pocStage()
		if err := w.replaceAcceptedScopeWithDecision(decision); err != nil {
			return err
		}
		rollback, err := w.state.BeginParentDecision()
		if err != nil {
			return err
		}

		prompt := decisionPrompt(request, decision, activeTaskPath)
		checkpoint := state.ResumeCheckpoint{
			Stage:          state.ResumeStageWorker,
			Phase:          "worker-decision",
			Role:           state.WorkerRole,
			Model:          w.config.WorkerModel,
			ReadOnly:       pocStage,
			Effort:         w.config.EscalatedEffort,
			Prompt:         prompt,
			OriginalPrompt: prompt,
			Request:        request,
			Decision:       decision,
		}
		return w.rollbackWhenPreCallGuardFailure(
			rollback,
			w.executeWorkerCheckpointWithExhaustiveContext(request, activeTaskPath, checkpoint, pocStage),
		)
	}))
}

func (w *Workflow) rollbackWhenPreCallGuardFailure(rollback state.ParentActionRollback, err error) error {
	if err == nil || w.modelCallAttempts != 1 || !runner.IsPreCallGuardFailure(err) {
		return err
	}
	return w.state.RollbackParentAction(rollback, err)
}

func (w *Workflow) replaceAcceptedScopeWithDecision(decision string) error {
	if err := w.state.Remove(acceptedFixScopeStateFile); err != nil {
		return err
	}
	return w.state.Write("last-decision", decision)
}

func (w *Workflow) ExecuteExplicitFix(instruction, origin, cause string) error {
	return w.ExecuteExplicitFixWithScope(instruction, origin, cause, "")
}

func (w *Workflow) ExecuteExplicitFixWithScope(instruction, origin, cause, acceptedScope string) error {
	return quietWhenParentFileGuardStopped(w.withTemp(func() error {
		if err := w.admitParentAction(state.ParentActionFix); err != nil {
			return err
		}

		request, err := w.state.Read("last-request")
		if err != nil {
			return &WorkerError{Message: "no previous task for this repository"}
		}
		w.prepareAcceptedFixScope(acceptedScope)

		decision := w.state.ReadOr("last-decision", "none")
		review := w.state.ReadOr("last-review", "none")
		rollback, err := w.state.BeginParentFix(origin, cause)
		if err != nil {
			return err
		}

		activeTaskPath, err := w.ensureActiveTaskPath("worker-explicit-fix")
		if err != nil {
			return err
		}

		decl, err := w.gateExternalFeasibility("worker-explicit-fix", false)
		if err != nil {
			return err
		}
		pocStage := decl.pocStage()
		prompt := explicitFixPrompt(request, decision, review, instruction, activeTaskPath)
		checkpoint := state.ResumeCheckpoint{
			Stage:          state.ResumeStageWorker,
			Phase:          "worker-explicit-fix",
			Role:           state.WorkerRole,
			Model:          w.config.WorkerModel,
			ReadOnly:       pocStage,
			Effort:         w.config.EscalatedEffort,
			Prompt:         prompt,
			OriginalPrompt: prompt,
			Request:        request,
			Decision:       decision,
		}
		return w.rollbackWhenPreCallGuardFailure(
			rollback,
			w.executeWorkerCheckpointWithExhaustiveContext(request, activeTaskPath, checkpoint, pocStage),
		)
	}))
}

func (w *Workflow) executeWorkerCheckpoint(request string, checkpoint state.ResumeCheckpoint, pocStage bool) error {
	if pocStage {
		stopped, err := w.savePoCStartSnapshot()
		if err != nil || stopped {
			return err
		}
	}

	workerResult, err := w.runWorkerModelWithRuleActivation(checkpoint)
	if err != nil {
		return err
	}
	if pocStage {
		stopped, err := w.verifyPoCEndSnapshot()
		if err != nil || stopped {
			return err
		}
		if workerResult.Status == packet.StatusImplemented {
			return w.routePoCWorkerResult(workerResult)
		}
	}
	return w.handleWorkerResult(request, workerResult, checkpoint.Phase)
}

func (w *Workflow) handleWorkerResult(request string, workerResult packet.Result, workerPhase string) error {
	if stopped, err := w.verifyQualitySurfaceBaseline(workerPhase); err != nil || stopped {
		return err
	}
	switch workerResult.Status {
	case packet.StatusNeedsSolDecision:
		if err := w.state.WaitForDecision(); err != nil {
			return err
		}
		return w.emitResult(workerResult)
	case "IMPLEMENTED":
		if err := w.state.ContinueAfterWorkerResult(); err != nil {
			return err
		}
		return w.reviewUntilStable(request, workerResult, 1, 0, workerPhase)
	default:
		return &WorkerError{Phase: "worker-format", Message: "worker did not return a valid STATUS"}
	}
}

func boundedText(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	prefix := "[前方を省略] "
	start := len(value) - (maxBytes - len(prefix))
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return prefix + value[start:]
}

func workerError(phase string, outputPath string, runErr error) *WorkerError {
	exitCode := 1
	if value, ok := runErr.(interface{ ExitCode() int }); ok {
		exitCode = value.ExitCode()
	}

	return &WorkerError{
		Phase:    phase,
		ExitCode: exitCode,
		Tail:     packet.Tail(outputPath, 30),
		Message:  runErr.Error(),
	}
}

func machineReport(value packet.Result) (string, error) {
	data, err := value.MachineJSON()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (w *Workflow) writeLastReview(value packet.Result) error {
	report, err := machineReport(value)
	if err != nil {
		return err
	}
	return w.state.Write("last-review", report)
}

func (w *Workflow) emitResult(value packet.Result) error {
	report, err := machineReport(value)
	if err != nil {
		return err
	}
	if err := w.state.RecordSolResult(value, w.lastProducer); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w.output, report)
	return err
}

func (w *Workflow) emitReviewResult(value packet.Result) error {
	reviewStart, err := w.state.LoadReviewStartSnapshot()
	if err != nil {
		return fmt.Errorf("parent review bindingのreview-start snapshotを読めません: %w", err)
	}
	report, err := machineReport(value)
	if err != nil {
		return err
	}
	digest := state.SnapshotDigest{Head: reviewStart.Head, IndexDigest: reviewStart.IndexDigest, WorktreeDigest: reviewStart.WorktreeDigest}
	if err := w.state.RecordSolResultWithReviewSnapshot(value, w.lastProducer, digest); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w.output, report)
	return err
}

func (w *Workflow) enforceRiskFloor(
	request string,
	workerResult packet.Result,
	reviewNumber int,
	autoFixes int,
	decision string,
	effectiveHigh bool,
	reviewResult packet.Result,
) (packet.Result, bool, error) {
	if !effectiveHigh || reviewResult.Status != packet.StatusPass {
		return reviewResult, false, nil
	}
	if w.acceptedFixScopeCoversCurrent() {
		return reviewResult, false, nil
	}
	reemitResult, stopped, err := w.riskFloorReemit(request, workerResult, reviewNumber, autoFixes, decision)
	if err != nil || stopped {
		return packet.Result{}, stopped, err
	}
	return reemitResult, false, nil
}

func (w *Workflow) riskFloorReemit(
	request string,
	workerResult packet.Result,
	reviewNumber int,
	autoFixes int,
	decision string,
) (packet.Result, bool, error) {
	prompt := riskFloorReemitPrompt()
	checkpoint := state.ResumeCheckpoint{
		Stage:           state.ResumeStageReview,
		Phase:           fmt.Sprintf("reviewer-%d-risk-floor", reviewNumber),
		Role:            state.ReviewerRole,
		Model:           w.config.HighRiskReviewerModel,
		ReadOnly:        true,
		Effort:          w.config.RoutineEffort,
		Prompt:          prompt,
		OriginalPrompt:  prompt,
		Request:         request,
		Decision:        decision,
		WorkerResult:    &workerResult,
		ReviewNumber:    reviewNumber,
		AutoFixes:       autoFixes,
		RiskFloorReemit: true,
	}
	reemitResult, err := w.runModel(checkpoint)
	if err != nil {
		return packet.Result{}, false, err
	}
	if stopped, err := w.verifyReviewEndSnapshot(); err != nil {
		return packet.Result{}, false, err
	} else if stopped {
		return packet.Result{}, true, nil
	}
	return resolveRiskFloorReemit(reemitResult), false, nil
}

func resolveRiskFloorReemit(reemitResult packet.Result) packet.Result {
	if reemitResult.Status == packet.StatusNeedsSolReview {
		return reemitResult
	}
	return riskFloorFailClosedResult(reemitResult)
}

func riskFloorFailClosedResult(reemitResult packet.Result) packet.Result {
	return packet.Result{
		Status:              packet.StatusNeedsSolReview,
		Risk:                packet.RiskHigh,
		Summary:             fmt.Sprintf("reviewerがrisk floor再出力要求へ従わず%sを返したためSol確認へ昇格", reemitResult.Status),
		RequirementCoverage: "reviewer再出力が非準拠のためSolが直接確認する必要あり",
		Invariants:          "wrapper risk floorはHIGH RISK経路のreviewer PASSを許容しない",
		TestEvidence:        "reviewer同一sessionへNEEDS_SOL_REVIEW/HIGH再出力を依頼済み",
		Issues:              fmt.Sprintf("reviewer再出力が非許容STATUS(%s)を返却", reemitResult.Status),
		ResidualRisk:        "reviewer判断だけでHIGH RISK経路を完了扱いできない",
		Targets:             []string{"直近reviewer出力と最終diff"},
		Artifacts:           append([]string(nil), reemitResult.Artifacts...),
		SolQuestion:         "reviewer非準拠時の最終確認・修正方針をSolが判断する",
	}
}

func (w *Workflow) captureWorkerEndSnapshot() (state.GitSnapshot, bool, error) {
	workerEnd, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		return workerEnd, true, w.failClosedSnapshot(state.SnapshotStageWorkerEnd, workerEnd, state.GitSnapshot{}, "worker-end snapshot取得失敗", err)
	}
	if err := w.state.SaveWorkerEndSnapshot(workerEnd); err != nil {
		return workerEnd, true, w.failClosedSnapshot(state.SnapshotStageWorkerEnd, workerEnd, state.GitSnapshot{}, "worker-end snapshot保存失敗", err)
	}
	return workerEnd, false, nil
}

func (w *Workflow) saveReportOnlyStartSnapshot() (bool, error) {
	start, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		return true, w.failClosedReportOnlySnapshot(state.SnapshotStageReportOnlyStart, start, state.GitSnapshot{}, "report-only開始前snapshot取得失敗", err)
	}
	if err := w.state.SaveReportOnlyStartSnapshot(start); err != nil {
		return true, w.failClosedReportOnlySnapshot(state.SnapshotStageReportOnlyStart, start, state.GitSnapshot{}, "report-only開始前snapshot保存失敗", err)
	}
	return false, nil
}

func (w *Workflow) gateReportOnlyResumeSnapshot() (bool, error) {
	if _, err := w.state.LoadReportOnlyStartSnapshot(); err != nil {
		return true, w.failClosedReportOnlySnapshot(
			state.SnapshotStageReportOnlyStart,
			state.GitSnapshot{},
			state.GitSnapshot{},
			"resume再開前にreport-only開始前snapshotが欠損しているため不変性の基準を確認できません",
			err,
		)
	}
	return false, nil
}

func (w *Workflow) verifyReportOnlyEndSnapshot() (bool, error) {
	return w.verifyEndSnapshot(snapshotEndCheck{
		stage:          state.SnapshotStageReportOnlyEnd,
		loadStart:      w.state.LoadReportOnlyStartSnapshot,
		failClosed:     w.failClosedReportOnlySnapshot,
		loadReason:     "report-only開始前snapshot読込失敗",
		captureReason:  "report-only終了後snapshot取得失敗",
		saveReason:     "snapshot comparison保存失敗",
		mismatchReason: "report-only worker開始前から終了後までの間にrepository状態が変化しています",
	})
}

func (w *Workflow) recordConvergenceRound(reviewNumber int, autoFixes int, workerPhase string, snap state.GitSnapshot) {
	record := state.RoundRecord{
		TaskID:       w.state.ReadOr("task.id", "unknown"),
		ReviewNumber: reviewNumber,
		AutoFixes:    autoFixes,
		WorkerPhase:  workerPhase,
		CapturedAt:   w.now().UTC(),
		Snapshot:     state.SnapshotDigest{Head: snap.Head, IndexDigest: snap.IndexDigest, WorktreeDigest: snap.WorktreeDigest},
	}
	record.Paths, record.CaptureError = w.classifyRoundPaths()
	_ = w.state.AppendRoundRecord(record)
}

func (w *Workflow) recordBaselineRound() {
	record := state.RoundRecord{
		TaskID:      w.state.ReadOr("task.id", "unknown"),
		WorkerPhase: state.RoundWorkerPhaseBaseline,
		CapturedAt:  w.now().UTC(),
	}
	snap, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		record.CaptureError = boundedText(err.Error(), packet.MaxDiagnosticBytes)
	} else {
		record.Snapshot = state.SnapshotDigest{Head: snap.Head, IndexDigest: snap.IndexDigest, WorktreeDigest: snap.WorktreeDigest}
	}
	paths, classErr := w.classifyRoundPaths()
	if record.CaptureError == "" {
		record.CaptureError = classErr
	}
	record.Paths = paths
	_ = w.state.AppendRoundRecord(record)
}

func (w *Workflow) classifyRoundPaths() ([]state.RoundPathState, string) {
	baselineHead, _ := w.state.Read("baseline-head")
	paths, err := w.collectChangedPaths(w.config.RepoRoot, baselineHead)
	if err != nil {
		return nil, boundedText(err.Error(), packet.MaxDiagnosticBytes)
	}
	return state.ClassifyRoundPaths(w.config.RepoRoot, paths), ""
}

func (w *Workflow) verifyReviewStartSnapshot() (bool, error) {
	workerEnd, err := w.state.LoadWorkerEndSnapshot()
	if err != nil {
		return true, w.failClosedSnapshot(state.SnapshotStageReviewStart, state.GitSnapshot{}, state.GitSnapshot{}, "worker-end snapshot読込失敗", err)
	}
	reviewStart, err := w.captureRepositoryBoundary()
	if err != nil {
		return true, w.failClosedSnapshot(state.SnapshotStageReviewStart, workerEnd, state.GitSnapshot{}, "review-start snapshot取得失敗", err)
	}
	if err := w.state.SaveReviewStartSnapshot(reviewStart); err != nil {
		return true, w.failClosedSnapshot(state.SnapshotStageReviewStart, workerEnd, reviewStart, "review-start snapshot保存失敗", err)
	}
	comparison := state.CompareGitSnapshot(workerEnd, reviewStart, state.SnapshotStageReviewStart, "")
	if err := w.state.SaveSnapshotComparison(comparison); err != nil {
		return true, w.failClosedSnapshot(state.SnapshotStageReviewStart, workerEnd, reviewStart, "snapshot comparison保存失敗", err)
	}
	if !comparison.Matched {
		return true, w.failClosedSnapshot(state.SnapshotStageReviewStart, workerEnd, reviewStart, "worker終了状態とreview開始状態が一致しません", nil)
	}
	w.pendingSnapshot = snapshotDiagnosticPtr(state.BuildSnapshotDiagnostic(state.SnapshotStageReviewStart, workerEnd, reviewStart, comparison, ""))
	return false, nil
}

func (w *Workflow) verifyReviewResumeSnapshot(checkpoint state.ResumeCheckpoint) (bool, error) {
	saved, err := w.state.LoadReviewStartSnapshot()
	if err != nil {
		return true, w.failClosedSnapshot(state.SnapshotStageReviewResume, state.GitSnapshot{}, state.GitSnapshot{}, "review-start snapshot読込失敗", err)
	}
	current, err := w.captureRepositoryBoundary()
	if err != nil {
		return true, w.failClosedSnapshot(state.SnapshotStageReviewResume, saved, state.GitSnapshot{}, "resume時snapshot取得失敗", err)
	}
	comparison := state.CompareGitSnapshot(saved, current, state.SnapshotStageReviewResume, "")
	if !comparison.Matched && !acceptReviewResumeParentDelta(saved, current, checkpoint) {
		if err := w.state.SaveSnapshotComparison(comparison); err != nil {
			return true, w.failClosedSnapshot(state.SnapshotStageReviewResume, saved, current, "snapshot comparison保存失敗", err)
		}
		return true, w.failClosedSnapshot(state.SnapshotStageReviewResume, saved, current, "review開始時から状態が変化しています", nil)
	}
	if !comparison.Matched {
		comparison.ParentUpdateAccepted = true
		comparison.Reason = "停止期間中の承認済み親管理file更新のみのためreview基準を現状へ再固定"
	}
	if err := w.state.SaveSnapshotComparison(comparison); err != nil {
		return true, w.failClosedSnapshot(state.SnapshotStageReviewResume, saved, current, "snapshot comparison保存失敗", err)
	}
	if comparison.ParentUpdateAccepted {
		if err := w.state.SaveReviewStartSnapshot(current); err != nil {
			return true, w.failClosedSnapshot(state.SnapshotStageReviewResume, saved, current, "review-start snapshot再固定保存失敗", err)
		}
		w.recordSnapshotParentUpdateEvent(checkpoint)
	}
	w.pendingSnapshot = snapshotDiagnosticPtr(state.BuildSnapshotDiagnostic(state.SnapshotStageReviewResume, saved, current, comparison, comparison.Reason))
	return false, nil
}

func acceptReviewResumeParentDelta(saved, current state.GitSnapshot, checkpoint state.ResumeCheckpoint) bool {
	if !reviewResumeParentBaselineMatches(saved, current) || saved.ParentFiles == nil || checkpoint.StopGitSnapshot == nil || checkpoint.StopGitSnapshot.ParentFiles == nil || current.ParentFiles == nil {
		return false
	}
	stopParents := *checkpoint.StopGitSnapshot.ParentFiles
	now := *current.ParentFiles
	changedDuringStop := false
	for _, path := range parentStatePaths(*saved.ParentFiles, stopParents, now) {
		reviewStart := state.FindParentFileState(*saved.ParentFiles, path)
		stop := state.FindParentFileState(stopParents, path)
		currentState := state.FindParentFileState(now, path)

		if stop != reviewStart {
			return false
		}
		if stop.Exists && !currentState.Exists {
			return false
		}
		if currentState != stop {
			changedDuringStop = true
		}
	}
	return changedDuringStop
}

func reviewResumeParentBaselineMatches(saved, current state.GitSnapshot) bool {
	return saved.Head == current.Head &&
		saved.IndexDigest == current.IndexDigest &&
		saved.WorktreeDigestExcludingParent != "" &&
		saved.WorktreeDigestExcludingParent == current.WorktreeDigestExcludingParent
}

func parentStatePaths(groups ...state.ParentFileStates) []string {
	seen := make(map[string]struct{})
	for _, group := range groups {
		for _, s := range group {
			seen[s.Path] = struct{}{}
		}
	}
	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

func (w *Workflow) recordSnapshotParentUpdateEvent(checkpoint state.ResumeCheckpoint) {
	now := w.now().UTC()
	w.state.RecordModelCallLog(state.ModelCallLog{
		TaskID:      w.state.ReadOr("task.id", "unknown"),
		CallType:    state.CallTypeEvent,
		StartedAt:   now,
		CompletedAt: now,
		Phase:       checkpoint.Phase + "-review-resume-parent-update",
		Role:        state.ReviewerRole,
		Outcome:     "snapshot_parent_update",
	})
}

func (w *Workflow) verifyEndSnapshot(check snapshotEndCheck) (bool, error) {
	start, err := check.loadStart()
	if err != nil {
		return true, check.failClosed(check.stage, state.GitSnapshot{}, state.GitSnapshot{}, check.loadReason, err)
	}
	current, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		return true, check.failClosed(check.stage, start, state.GitSnapshot{}, check.captureReason, err)
	}
	comparison := state.CompareGitSnapshot(start, current, check.stage, "")
	if err := w.state.SaveSnapshotComparison(comparison); err != nil {
		return true, check.failClosed(check.stage, start, current, check.saveReason, err)
	}
	if !comparison.Matched {
		return true, check.failClosed(check.stage, start, current, check.mismatchReason, nil)
	}
	return false, nil
}

func (w *Workflow) verifyReviewEndSnapshot() (bool, error) {
	return w.verifyEndSnapshot(snapshotEndCheck{
		stage:          state.SnapshotStageReviewEnd,
		loadStart:      w.state.LoadReviewStartSnapshot,
		failClosed:     w.failClosedSnapshot,
		loadReason:     "review-start snapshot読込失敗",
		captureReason:  "review-end snapshot取得失敗",
		saveReason:     "snapshot comparison保存失敗",
		mismatchReason: "reviewer実行中にrepository状態が変化しています",
	})
}

func (w *Workflow) failClosedSnapshot(stage state.SnapshotStage, workerEnd, reviewStart state.GitSnapshot, reason string, cause error) error {
	w.recordSnapshotEvent(state.ReviewerRole, stage, workerEnd, reviewStart, reason, cause)
	return w.failClosedStopped(stage, reason, cause, snapshotFailClosedResult)
}

func (w *Workflow) failClosedReportOnlySnapshot(stage state.SnapshotStage, start, current state.GitSnapshot, reason string, cause error) error {
	w.recordSnapshotEvent(state.WorkerRole, stage, start, current, reason, cause)
	return w.failClosedStopped(stage, reason, cause, reportOnlySnapshotFailClosedResult)
}

func (w *Workflow) failClosedStopped(stage state.SnapshotStage, reason string, cause error, build func(state.SnapshotStage, string) packet.Result) error {
	if err := w.state.DiscardResumeAndWaitForSolReview(); err != nil {
		return err
	}
	if cause != nil {
		reason = fmt.Sprintf("%s: %v", reason, cause)
	}
	return w.emitResult(build(stage, reason))
}

func (w *Workflow) recordSnapshotEvent(role state.SessionRole, stage state.SnapshotStage, previous, current state.GitSnapshot, reason string, cause error) {
	comparison := state.CompareGitSnapshot(previous, current, stage, "")
	diag := state.BuildSnapshotDiagnostic(stage, previous, current, comparison, reason)
	outcome := "snapshot_unavailable"
	switch {
	case diag.Matched != nil && !*diag.Matched:
		outcome = "snapshot_mismatch"
		w.state.RecordSnapshotMismatch(diag.MismatchAxis)
	case diag.Matched != nil && *diag.Matched:
		outcome = "snapshot_save_failed"
	}
	now := w.now().UTC()
	entry := state.ModelCallLog{
		TaskID:      w.state.ReadOr("task.id", "unknown"),
		CallType:    state.CallTypeEvent,
		StartedAt:   now,
		CompletedAt: now,
		Phase:       fmt.Sprintf("%s-snapshot-check", stage),
		Role:        role,
		Outcome:     outcome,
		Snapshot:    snapshotDiagnosticPtr(diag),
	}
	if cause != nil {
		entry.Error = boundedText(cause.Error(), packet.MaxDiagnosticBytes)
	}
	w.state.RecordModelCallLog(entry)
}

func snapshotDiagnosticPtr(diag state.SnapshotDiagnostic) *state.SnapshotDiagnostic {
	return &diag
}

func snapshotFailClosedResult(stage state.SnapshotStage, reason string) packet.Result {
	return packet.Result{
		Status:              packet.StatusNeedsSolReview,
		Risk:                packet.RiskHigh,
		Summary:             fmt.Sprintf("worker終了状態とreview開始状態の同一性確認に失敗しreviewerを呼ばずSol確認へ昇格(%s)", stage),
		RequirementCoverage: "reviewerへ状態を引き渡す前にSolが直接確認する必要あり",
		Invariants:          "wrapperはworker-endとreview-start snapshotの3軸一致を確認するまでreviewerを呼ばない",
		TestEvidence:        "HEAD/index/worktree snapshotの比較・取得結果で不一致または失敗を検出",
		Issues:              reason,
		ResidualRisk:        "reviewerがworkerと別の状態をreviewする可能性を排除できなかった",
		Targets:             []string{"repository HEAD/index/worktreeの現在状態と保存済みsnapshot state file"},
		SolQuestion:         "worker終了状態とreview開始状態の差異・外部変更の有無をSolが判断する",
	}
}

func reportOnlySnapshotFailClosedResult(stage state.SnapshotStage, reason string) packet.Result {
	return packet.Result{
		Status:              packet.StatusNeedsSolReview,
		Risk:                packet.RiskHigh,
		Summary:             fmt.Sprintf("report-only PACKET再出力workerの開始前後でHEAD/index/worktree同一性を確認できず(%s)、通常reviewへ進めずSol確認へ昇格", stage),
		RequirementCoverage: "report-only workerのrepo不変postconditionを機械強制できなかったためSolが直接確認する必要あり",
		Invariants:          "wrapperはreport-only worker開始前snapshotと終了後状態の3軸一致を確認するまで通常reviewへ進まない",
		TestEvidence:        "開始前保存snapshotと終了後snapshotの比較で不一致または取得失敗を検出",
		Issues:              reason,
		ResidualRisk:        "report-only workerがrepositoryを変更した可能性とその意図を排除できなかった",
		Targets:             []string{"repository HEAD/index/worktreeの現在状態とreport-only開始前snapshot・telemetry記録"},
		SolQuestion:         "report-only workerによる変更の意図有無と追跡・修正方針をSolが判断する",
	}
}

func nonConvergedResult(reviewResult packet.Result) packet.Result {
	return packet.Result{
		Status:              packet.StatusNeedsSolReview,
		Risk:                packet.RiskHigh,
		Summary:             "GLM workerと独立reviewerの自動修正が規定回数内に収束しなかった",
		RequirementCoverage: "最終状態をSol Highで確認する必要あり",
		Invariants:          "未確定",
		TestEvidence:        "直近worker/reviewerで検証実施",
		Issues:              reviewResult.Issues,
		ResidualRisk:        "reviewer指摘が残っている可能性",
		Targets:             []string{"最終diffと直近reviewer指摘に限定"},
		Artifacts:           append([]string(nil), reviewResult.Artifacts...),
		SolQuestion:         "未解決問題の修正方針を判断する",
	}
}
