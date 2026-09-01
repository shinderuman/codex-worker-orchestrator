package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
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
	if w.state.Exists("pending-decision") {
		return &WorkerError{Message: "previous task is waiting for Sol decision; use --decision or --reset"}
	}
	if open := w.state.OpenParentReviewLabel(); open != "none" {
		return &WorkerError{Message: fmt.Sprintf("previous task has unresolved parent review (%s); resolve it explicitly with --accept (or --fix when rework is required) before starting a new task", open)}
	}
	checkpoint, err := w.state.LoadResumeCheckpoint()
	if err != nil {
		return nil
	}
	switch checkpoint.StopKind {
	case state.ResumeStopRateLimited:
		return &WorkerError{Message: "previous task is rate-limited; use --resume or --reset"}
	case state.ResumeStopProviderUnavailable:
		return &WorkerError{Message: "previous task is provider-unavailable; use --resume or --reset"}
	case state.ResumeStopInterrupted:
		return &WorkerError{Message: "previous task is interrupted; use --resume or --reset"}
	case state.ResumeStopGuardRecoverable:
		return &WorkerError{Message: "previous task stopped on a recoverable guard failure; repair the guard then use --resume or --reset"}
	default:
		return nil
	}
}

func (w *Workflow) initializeNewTask(request string) (string, error) {
	if _, err := w.state.StartNewTask(); err != nil {
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
	if err := w.state.Remove("last-decision", "last-review", activeTaskStateKey, acceptedFixScopeStateFile); err != nil {
		return "", err
	}

	activeTaskPath, wired, err := resolveActiveTaskPath(w.config.RepoRoot)
	if err != nil {
		return "", w.failClosedActiveTaskResolution("worker-new", err)
	}
	if !wired {
		activeTaskPath = ""
	}
	if err := w.state.Write(activeTaskStateKey, activeTaskPath); err != nil {
		return "", err
	}
	return activeTaskPath, nil
}

func (w *Workflow) ExecuteDecision(decision string) error {
	return quietWhenParentFileGuardStopped(w.withTemp(func() error {
		if w.state.TaskStatus() != state.TaskStatusWaitingDecision || !w.state.Exists("pending-decision") {
			return &WorkerError{Message: "no pending Sol decision for this repository"}
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
		if err := w.state.SetTaskStatus(state.TaskStatusActive); err != nil {
			return err
		}
		w.state.RecordDecision()

		if _, err := w.state.RecordParentOutcome(state.ParentOutcomeDecision, ""); err != nil {
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
		return w.executeWorkerCheckpointWithExhaustiveContext(request, activeTaskPath, checkpoint, pocStage)
	}))
}

func (w *Workflow) replaceAcceptedScopeWithDecision(decision string) error {
	if err := w.state.Remove(acceptedFixScopeStateFile); err != nil {
		return err
	}
	return w.state.Write("last-decision", decision)
}

func (w *Workflow) ExecuteExplicitFix(instruction, origin string) error {
	return w.ExecuteExplicitFixWithScope(instruction, origin, "")
}

func (w *Workflow) ExecuteExplicitFixWithScope(instruction, origin, acceptedScope string) error {
	return quietWhenParentFileGuardStopped(w.withTemp(func() error {
		if w.state.Exists("pending-decision") {
			return &WorkerError{Message: "task is waiting for Sol decision; resolve it before --fix"}
		}
		if w.state.TaskStatus() != state.TaskStatusWaitingSolReview {
			return &WorkerError{Message: "--fix is only available after NEEDS_SOL_REVIEW; start a new task after PASS"}
		}

		request, err := w.state.Read("last-request")
		if err != nil {
			return &WorkerError{Message: "no previous task for this repository"}
		}
		w.prepareAcceptedFixScope(acceptedScope)

		decision := w.state.ReadOr("last-decision", "none")
		review := w.state.ReadOr("last-review", "none")
		if err := w.state.SetTaskStatus(state.TaskStatusActive); err != nil {
			return err
		}
		w.state.RecordFix()

		if _, err := w.state.RecordParentOutcome(state.ParentOutcomeFix, origin); err != nil {
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
		return w.executeWorkerCheckpointWithExhaustiveContext(request, activeTaskPath, checkpoint, pocStage)
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

func (w *Workflow) ExecuteResume() error {
	return quietWhenParentFileGuardStopped(w.withTemp(w.executeResume))
}

func (w *Workflow) executeResume() error {
	checkpoint, decl, pocResume, err := w.loadResumeCheckpoint()
	if err != nil {
		return err
	}
	reuseCompletedResult, err := w.prepareGuardRecovery(checkpoint)
	if err != nil {
		return err
	}
	previousCheckpoint := checkpoint
	completedResult := checkpoint.CompletedResult
	checkpoint, stopped, err := w.prepareResumeCheckpoint(checkpoint, decl, pocResume)
	if err != nil || stopped {
		return err
	}
	checkpoint.ClearStop()
	if reuseCompletedResult && completedResult != nil {
		if err := w.state.ClearResumeCheckpoint(); err != nil {
			return err
		}
		return w.routeResumeResult(checkpoint, decl, *completedResult)
	}

	w.resetInstructionReadObservation()
	result, err := w.runModel(checkpoint)
	if err != nil {
		return w.handleResumeRunError(checkpoint, previousCheckpoint, err)
	}
	return w.routeResumeResult(checkpoint, decl, result)
}

func (w *Workflow) loadResumeCheckpoint() (state.ResumeCheckpoint, externalFeasibility, bool, error) {
	checkpoint, err := w.state.LoadResumeCheckpoint()
	if err != nil {
		return state.ResumeCheckpoint{}, externalFeasibility{}, false, err
	}
	if !checkpoint.IsStopped() {
		return state.ResumeCheckpoint{}, externalFeasibility{}, false, &WorkerError{Message: "saved task is not stopped by Z.ai 5h limit, provider unavailability, user interruption or a recoverable guard failure"}
	}
	if !isKnownResumeStage(checkpoint.Stage) {
		return state.ResumeCheckpoint{}, externalFeasibility{}, false, &WorkerError{Message: fmt.Sprintf("unknown resume stage: %s", checkpoint.Stage)}
	}
	decl, err := w.gateExternalFeasibility(checkpoint.Phase, true)
	if err != nil {
		return state.ResumeCheckpoint{}, externalFeasibility{}, false, err
	}
	return checkpoint, decl, checkpoint.Stage == state.ResumeStageWorker && decl.pocStage(), nil
}

func (w *Workflow) prepareResumeCheckpoint(
	checkpoint state.ResumeCheckpoint,
	decl externalFeasibility,
	pocResume bool,
) (state.ResumeCheckpoint, bool, error) {
	if checkpoint.StopKind == state.ResumeStopInterrupted {
		if err := w.verifyInterruptedRetention(checkpoint); err != nil {
			return checkpoint, false, err
		}
	}
	if err := w.activateResume(checkpoint); err != nil {
		return checkpoint, false, err
	}
	if stopped, err := w.gateResumeSnapshots(checkpoint, pocResume); err != nil || stopped {
		return checkpoint, stopped, err
	}
	if err := w.gateResumeProvider(checkpoint); err != nil {
		return checkpoint, false, err
	}
	if checkpoint.Stage == state.ResumeStageReview {
		if stopped, err := w.verifyReviewResumeSnapshot(checkpoint); err != nil || stopped {
			return checkpoint, stopped, err
		}
	}
	checkpoint.Prompt = resumePrompt(checkpoint)
	activatedCheckpoint, activationErr := w.activateResumeRuleContext(checkpoint)
	if activationErr != nil {
		return checkpoint, false, activationErr
	}
	checkpoint = activatedCheckpoint
	if checkpoint.Stage == state.ResumeStageWorker {
		checkpoint.ReadOnly = decl.pocStage()
	}
	return checkpoint, false, nil
}

func (w *Workflow) activateResumeRuleContext(checkpoint state.ResumeCheckpoint) (state.ResumeCheckpoint, error) {
	if checkpoint.Role != state.WorkerRole || checkpoint.ReportOnly {
		return checkpoint, nil
	}
	activated, _, err := w.activateCheckpointRules(checkpoint)
	if err != nil {
		return checkpoint, err
	}
	return activated, nil
}

func (w *Workflow) gateResumeProvider(checkpoint state.ResumeCheckpoint) error {
	if checkpoint.StopKind != state.ResumeStopProviderUnavailable {
		return nil
	}
	if err := w.gateResumeOnProbe(checkpoint); err != nil {
		return w.handleResumeProbeError(checkpoint, err)
	}
	return nil
}

func (w *Workflow) activateResume(checkpoint state.ResumeCheckpoint) error {
	if err := w.state.SetTaskStatus(state.TaskStatusActive); err != nil {
		return err
	}
	w.state.RecordResume()
	w.currentResumeSource = checkpoint.StopKind.ResumeSource()
	return nil
}

func (w *Workflow) gateResumeSnapshots(checkpoint state.ResumeCheckpoint, pocResume bool) (bool, error) {
	if checkpoint.Stage == state.ResumeStageAutoFix && checkpoint.ReportOnly {
		if stopped, err := w.gateReportOnlyResumeSnapshot(); err != nil || stopped {
			return stopped, err
		}
	}
	if pocResume {
		if stopped, err := w.gatePoCResumeSnapshot(); err != nil || stopped {
			return stopped, err
		}
	}
	return false, nil
}

func (w *Workflow) handleResumeProbeError(checkpoint state.ResumeCheckpoint, err error) error {
	var interrupted *runner.InterruptedCallError
	if errors.As(err, &interrupted) {
		return w.interruptBetweenCalls(checkpoint)
	}
	var providerUnavailable *runner.ProviderUnavailableError
	if errors.As(err, &providerUnavailable) {
		return err
	}
	var limitErr runner.ZaiRateLimitError
	if errors.As(err, &limitErr) {
		return err
	}
	_ = w.state.ClearResumeCheckpoint()
	_ = w.state.RemoveUnreadySession(checkpoint.Role)
	return &WorkerError{Phase: checkpoint.Phase, Message: err.Error()}
}

func (w *Workflow) handleResumeRunError(_ state.ResumeCheckpoint, previous state.ResumeCheckpoint, runErr error) error {
	if isResumeStopError(runErr) {
		return runErr
	}
	saved, loadErr := w.state.LoadResumeCheckpoint()
	if loadErr != nil || saved.StopKind.TaskStatus() == state.TaskStatusActive {
		_ = w.attachStopRepositoryBoundary(&previous)
		_ = w.state.SaveResumeCheckpoint(previous)
	}

	restoredStatus := state.TaskStatusActive
	if loadErr == nil {
		restoredStatus = saved.StopKind.TaskStatus()
	}
	if restoredStatus == state.TaskStatusActive {
		restoredStatus = previous.StopKind.TaskStatus()
	}
	_ = w.state.SetTaskStatus(restoredStatus)
	return runErr
}

func isResumeStopError(err error) bool {
	if errors.Is(err, errParentFileGuardStopped) {
		return true
	}
	var interrupted *runner.InterruptedCallError
	if errors.As(err, &interrupted) {
		return true
	}
	var providerUnavailable *runner.ProviderUnavailableError
	if errors.As(err, &providerUnavailable) {
		return true
	}
	var guardRecoverable *GuardRecoverableError
	if errors.As(err, &guardRecoverable) {
		return true
	}
	var limitErr runner.ZaiRateLimitError
	return errors.As(err, &limitErr)
}

func (w *Workflow) routeResumeResult(
	checkpoint state.ResumeCheckpoint,
	decl externalFeasibility,
	result packet.Result,
) error {
	switch checkpoint.Stage {
	case state.ResumeStageWorker:
		return w.routeWorkerResumeResult(checkpoint, decl, result)
	case state.ResumeStageReview:
		return w.routeReviewResumeResult(checkpoint, result)
	case state.ResumeStageAutoFix:
		return w.routeAutoFixResumeResult(checkpoint, result)
	default:
		return &WorkerError{Phase: checkpoint.Phase, Message: fmt.Sprintf("unknown resume stage: %s", checkpoint.Stage)}
	}
}

func (w *Workflow) routeWorkerResumeResult(
	checkpoint state.ResumeCheckpoint,
	decl externalFeasibility,
	result packet.Result,
) error {
	if decl.pocStage() {
		if stopped, err := w.verifyPoCEndSnapshot(); err != nil || stopped {
			return err
		}
		if result.Status == packet.StatusImplemented {
			return w.routePoCWorkerResult(result)
		}
	}
	result, err := w.convergeWorkerRuleActivation(checkpoint, result, w.activatedRulesForCheckpoint(checkpoint))
	if err != nil {
		return err
	}
	return w.handleWorkerResult(checkpoint.Request, result, checkpoint.Phase)
}

func (w *Workflow) routeReviewResumeResult(checkpoint state.ResumeCheckpoint, result packet.Result) error {
	if checkpoint.WorkerResult == nil {
		return &WorkerError{Phase: checkpoint.Phase, Message: "resume checkpoint has no worker result"}
	}
	if stopped, err := w.verifyReviewEndSnapshot(); err != nil || stopped {
		return err
	}
	workerResult := *checkpoint.WorkerResult
	reviewResult, stopped, err := w.resolveResumedReviewResult(checkpoint, workerResult, result)
	if err != nil || stopped {
		return err
	}
	if err := w.writeLastReview(reviewResult); err != nil {
		return err
	}
	return w.handleReviewResult(
		checkpoint.Request,
		workerResult,
		reviewResult,
		checkpoint.ReviewNumber,
		checkpoint.AutoFixes,
	)
}

func (w *Workflow) resolveResumedReviewResult(
	checkpoint state.ResumeCheckpoint,
	workerResult packet.Result,
	result packet.Result,
) (packet.Result, bool, error) {
	if checkpoint.RiskFloorReemit {
		return resolveRiskFloorReemit(result), false, nil
	}
	decision := w.state.ReadOr("last-decision", "none")
	highRiskFloor := w.resolveReviewResumeRisk(workerResult, checkpoint).high
	return w.enforceRiskFloor(
		checkpoint.Request,
		workerResult,
		checkpoint.ReviewNumber,
		checkpoint.AutoFixes,
		decision,
		highRiskFloor,
		result,
	)
}

func (w *Workflow) routeAutoFixResumeResult(checkpoint state.ResumeCheckpoint, result packet.Result) error {
	if checkpoint.ReportOnly {
		if stopped, err := w.verifyReportOnlyEndSnapshot(); err != nil || stopped {
			return err
		}
	}
	if !checkpoint.ReportOnly {
		var err error
		result, err = w.convergeWorkerRuleActivation(checkpoint, result, w.activatedRulesForCheckpoint(checkpoint))
		if err != nil {
			return err
		}
	}
	return w.handleAutoFixResult(
		checkpoint.Request,
		result,
		checkpoint.ReviewNumber,
		checkpoint.AutoFixes,
		checkpoint.Phase,
	)
}

func isKnownResumeStage(stage state.ResumeStage) bool {
	switch stage {
	case state.ResumeStageWorker, state.ResumeStageReview, state.ResumeStageAutoFix:
		return true
	default:
		return false
	}
}

func (w *Workflow) handleWorkerResult(request string, workerResult packet.Result, workerPhase string) error {
	if stopped, err := w.verifyQualitySurfaceBaseline(workerPhase); err != nil || stopped {
		return err
	}
	switch workerResult.Status {
	case packet.StatusNeedsSolDecision:
		if err := w.state.Touch("pending-decision"); err != nil {
			return err
		}
		if err := w.state.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
			return err
		}
		return w.emitResult(workerResult)
	case "IMPLEMENTED":
		if err := w.state.Remove("pending-decision"); err != nil {
			return err
		}
		if err := w.state.SetTaskStatus(state.TaskStatusActive); err != nil {
			return err
		}
		return w.reviewUntilStable(request, workerResult, 1, 0, workerPhase)
	default:
		return &WorkerError{Phase: "worker-format", Message: "worker did not return a valid STATUS"}
	}
}

func (w *Workflow) reviewUntilStable(
	request string,
	workerResult packet.Result,
	reviewNumber int,
	autoFixes int,
	workerPhase string,
) error {
	workerEnd, stopped, err := w.captureWorkerEndSnapshot()
	if err != nil || stopped {
		return err
	}
	handled, err := w.handleRepositoryQualityViolation(request, workerResult, reviewNumber, autoFixes)
	if err != nil || handled {
		return err
	}
	w.recordConvergenceRound(reviewNumber, autoFixes, workerPhase, workerEnd)

	checkpoint, decision, highRisk, err := w.buildReviewCheckpoint(request, workerResult, reviewNumber, autoFixes)
	if err != nil {
		return err
	}
	reviewResult, stopped, err := w.runReviewModel(checkpoint)
	if err != nil || stopped {
		return err
	}
	reviewResult, reemitStopped, err := w.enforceRiskFloor(
		request,
		workerResult,
		reviewNumber,
		autoFixes,
		decision,
		highRisk,
		reviewResult,
	)
	if err != nil || reemitStopped {
		return err
	}
	if err := w.writeLastReview(reviewResult); err != nil {
		return err
	}
	return w.handleReviewResult(request, workerResult, reviewResult, reviewNumber, autoFixes)
}

func (w *Workflow) buildReviewCheckpoint(
	request string,
	workerResult packet.Result,
	reviewNumber int,
	autoFixes int,
) (state.ResumeCheckpoint, string, bool, error) {
	decision := w.state.ReadOr("last-decision", "none")
	risk := w.computeEffectiveRisk(
		workerResult,
		autoFixes,
		w.state.Exists("last-decision"),
		w.state.Exists("last-review"),
	)
	phase, floorPrompt, floorActive := w.reviewerRiskFloorContext(reviewNumber, risk)
	activeTaskPath, err := w.ensureActiveTaskPath(phase)
	if err != nil {
		return state.ResumeCheckpoint{}, "", false, err
	}
	if _, err := w.gateExternalFeasibility(phase, false); err != nil {
		return state.ResumeCheckpoint{}, "", false, err
	}
	workerReport, err := machineReport(workerResult)
	if err != nil {
		return state.ResumeCheckpoint{}, "", false, err
	}
	reviewNavigation, err := w.reviewerNavigationContext(request, activeTaskPath, reviewNumber)
	if err != nil {
		return state.ResumeCheckpoint{}, "", false, err
	}
	prompt := reviewerPrompt(
		request,
		decision,
		workerReport,
		reviewNumber,
		w.state.BaselineDescription(),
		reviewNavigation,
		activeTaskPath,
	) + floorPrompt
	prompt, err = w.withCurrentRuleContext(prompt)
	if err != nil {
		return state.ResumeCheckpoint{}, "", false, err
	}
	return state.ResumeCheckpoint{
		Stage:               state.ResumeStageReview,
		Phase:               phase,
		Role:                state.ReviewerRole,
		Model:               w.reviewerModel(risk),
		ReadOnly:            true,
		Effort:              w.config.RoutineEffort,
		Prompt:              prompt,
		OriginalPrompt:      prompt,
		Request:             request,
		Decision:            decision,
		WorkerResult:        &workerResult,
		ReviewNumber:        reviewNumber,
		AutoFixes:           autoFixes,
		EffectiveRisk:       riskLabel(risk.high),
		EffectiveRiskSource: risk.source,
	}, decision, floorActive, nil
}

func (w *Workflow) reviewerRiskFloorContext(reviewNumber int, risk effectiveRisk) (string, string, bool) {
	floorActive := risk.high && !w.acceptedFixScopeCoversCurrent()
	phase := fmt.Sprintf("reviewer-%d", reviewNumber)
	if !floorActive {
		return phase, "", false
	}
	return phase + "-high-floor", reviewerHighRiskFloorPrompt(risk.source), true
}

func (w *Workflow) runReviewModel(checkpoint state.ResumeCheckpoint) (packet.Result, bool, error) {
	if stopped, err := w.verifyReviewStartSnapshot(); err != nil || stopped {
		return packet.Result{}, stopped, err
	}
	reviewResult, err := w.runModel(checkpoint)
	if err != nil {
		return packet.Result{}, false, err
	}
	if stopped, err := w.verifyReviewEndSnapshot(); err != nil || stopped {
		return packet.Result{}, stopped, err
	}
	return reviewResult, false, nil
}

func (w *Workflow) handleRepositoryQualityViolation(
	request string,
	workerResult packet.Result,
	reviewNumber int,
	autoFixes int,
) (bool, error) {
	qualityReport, err := w.qualityGate(w.config.RepoRoot)
	if err != nil {
		return true, &WorkerError{Phase: "harnesslint", Message: fmt.Sprintf("harnesslint failed: %v", err)}
	}
	if !harnesslint.IsViolation(qualityReport) {
		return false, nil
	}
	result := qualityGateFixResult(qualityReport)
	if err := w.writeLastReview(result); err != nil {
		return true, err
	}
	return true, w.handleReviewResult(request, workerResult, result, reviewNumber, autoFixes)
}

func (w *Workflow) handleReviewResult(
	request string,
	workerResult packet.Result,
	reviewResult packet.Result,
	reviewNumber int,
	autoFixes int,
) error {
	switch reviewResult.Status {
	case packet.StatusNeedsSolDecision:
		return w.finishReviewerDecision(reviewResult)
	case packet.StatusPass:
		return w.finishReview(state.TaskStatusComplete, reviewResult)
	case packet.StatusNeedsSolReview:
		return w.finishReview(state.TaskStatusWaitingSolReview, reviewResult)
	case packet.StatusFixRequired:
		return w.handleFixRequiredReview(request, workerResult, reviewResult, reviewNumber, autoFixes)
	default:
		return &WorkerError{Phase: "reviewer-format", Message: "reviewer did not return a valid STATUS"}
	}
}

func (w *Workflow) finishReview(status state.TaskStatus, result packet.Result) error {
	if err := w.state.SetTaskStatus(status); err != nil {
		return err
	}
	return w.emitResult(result)
}

func (w *Workflow) handleFixRequiredReview(
	request string,
	_ packet.Result,
	reviewResult packet.Result,
	reviewNumber int,
	autoFixes int,
) error {
	if autoFixes >= w.config.MaxAutoFixRounds {
		return w.finishReview(state.TaskStatusWaitingSolReview, nonConvergedResult(reviewResult))
	}

	checkpoint, err := w.prepareAutoFixCheckpoint(request, reviewResult, reviewNumber, autoFixes+1)
	if err != nil {
		return err
	}
	w.state.RecordAutoFix()

	fixResult, stopped, err := w.runAutoFixCheckpoint(checkpoint)
	if err != nil || stopped {
		return err
	}
	return w.handleAutoFixResult(request, fixResult, reviewNumber, checkpoint.AutoFixes, checkpoint.Phase)
}

func (w *Workflow) prepareAutoFixCheckpoint(
	request string,
	reviewResult packet.Result,
	reviewNumber int,
	nextAutoFixes int,
) (state.ResumeCheckpoint, error) {
	decision := w.state.ReadOr("last-decision", "none")
	phase := fmt.Sprintf("worker-auto-fix-%d", nextAutoFixes)
	activeTaskPath, err := w.ensureActiveTaskPath(phase)
	if err != nil {
		return state.ResumeCheckpoint{}, err
	}
	if _, err := w.gateExternalFeasibility(phase, false); err != nil {
		return state.ResumeCheckpoint{}, err
	}
	reviewReport, err := machineReport(reviewResult)
	if err != nil {
		return state.ResumeCheckpoint{}, err
	}

	reportOnly := packet.IsReportOnlyFix(reviewResult)
	prompt := automaticFixPrompt(request, decision, reviewReport, activeTaskPath)
	if reportOnly {
		prompt = reportOnlyFixPrompt(request, decision, reviewReport, activeTaskPath)
		phase = fmt.Sprintf("worker-report-only-%d", nextAutoFixes)
	}
	exhaustiveContext, err := w.exhaustiveSearchContext(request, activeTaskPath, state.WorkerRole, nextAutoFixes)
	if err != nil {
		return state.ResumeCheckpoint{}, err
	}
	prompt += exhaustiveContext
	return state.ResumeCheckpoint{
		Stage:          state.ResumeStageAutoFix,
		Phase:          phase,
		Role:           state.WorkerRole,
		Model:          w.config.WorkerModel,
		ReadOnly:       reportOnly,
		ReportOnly:     reportOnly,
		Effort:         w.config.RoutineEffort,
		Prompt:         prompt,
		OriginalPrompt: prompt,
		Request:        request,
		Decision:       decision,
		ReviewNumber:   reviewNumber,
		AutoFixes:      nextAutoFixes,
	}, nil
}

func (w *Workflow) runAutoFixCheckpoint(checkpoint state.ResumeCheckpoint) (packet.Result, bool, error) {
	if checkpoint.ReportOnly {
		stopped, err := w.saveReportOnlyStartSnapshot()
		if err != nil || stopped {
			return packet.Result{}, stopped, err
		}
	}

	fixResult, err := w.runWorkerModelWithRuleActivation(checkpoint)
	if err != nil {
		return packet.Result{}, false, err
	}
	if checkpoint.ReportOnly {
		stopped, err := w.verifyReportOnlyEndSnapshot()
		if err != nil || stopped {
			return packet.Result{}, stopped, err
		}
	}
	return fixResult, false, nil
}

func reviewNeedsHighRiskFloor(workerResult packet.Result, autoFixes int, hasDecision bool, hasPriorReview bool) bool {
	return workerResult.Risk == packet.RiskHigh || autoFixes > 0 || hasDecision || hasPriorReview
}

func riskLabel(high bool) string {
	if high {
		return highRiskValue
	}
	return "LOW"
}

func (w *Workflow) computeEffectiveRisk(workerResult packet.Result, autoFixes int, hasDecision bool, hasPriorReview bool) effectiveRisk {
	sp, qe := w.riskSurfaceDecisions()
	if !reviewNeedsHighRiskFloor(workerResult, autoFixes, hasDecision, hasPriorReview) && !sp.High && !qe.High {
		return effectiveRisk{high: false}
	}
	var sources []string
	if workerResult.Risk == packet.RiskHigh {
		sources = append(sources, "worker-declared")
	}
	if autoFixes > 0 {
		sources = append(sources, "auto-fix")
	}
	if hasDecision {
		sources = append(sources, "decision")
	}
	if hasPriorReview {
		sources = append(sources, "prior-review")
	}
	if sp.High {
		sources = append(sources, "self-protection:"+sp.Source)
	}
	if qe.High {
		sources = append(sources, "quality-evidence:"+qe.Source)
	}
	return effectiveRisk{high: true, source: strings.Join(sources, ";")}
}

func (w *Workflow) riskSurfaceDecisions() (selfProtectionDecision, qualityEvidenceDecision) {
	baselineHead, _ := w.state.Read("baseline-head")
	paths, err := w.collectChangedPaths(w.config.RepoRoot, baselineHead)
	if err != nil {
		return selfProtectionDecision{High: true, Source: "classify-error", HitPath: err.Error()}, qualityEvidenceDecision{}
	}
	sp := classifySelfProtection(paths)
	qe, err := classifyQualityEvidence(w.config.RepoRoot, baselineHead, paths)
	if err != nil {
		qe = qualityEvidenceDecision{High: true, Source: "classify-error", HitPath: err.Error()}
	}
	return sp, qe
}

func (w *Workflow) resolveReviewResumeRisk(workerResult packet.Result, checkpoint state.ResumeCheckpoint) effectiveRisk {
	if checkpoint.EffectiveRisk == highRiskValue {
		return effectiveRisk{high: true, source: checkpoint.EffectiveRiskSource}
	}
	hasDecision := w.state.Exists("last-decision")
	return w.computeEffectiveRisk(workerResult, checkpoint.AutoFixes, hasDecision, w.state.Exists("last-review"))
}

func (w *Workflow) reviewerModel(risk effectiveRisk) string {
	if risk.high {
		return w.config.HighRiskReviewerModel
	}
	return w.config.ReviewerModel
}

func (w *Workflow) handleAutoFixResult(
	request string,
	fixResult packet.Result,
	reviewNumber int,
	autoFixes int,
	fixPhase string,
) error {
	if stopped, err := w.verifyQualitySurfaceBaseline(fixPhase); err != nil || stopped {
		return err
	}
	switch fixResult.Status {
	case packet.StatusNeedsSolDecision:
		if err := w.state.Touch("pending-decision"); err != nil {
			return err
		}
		if err := w.state.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
			return err
		}
		return w.emitResult(fixResult)

	case packet.StatusImplemented:
		if err := w.state.Remove("pending-decision"); err != nil {
			return err
		}
		if err := w.state.SetTaskStatus(state.TaskStatusActive); err != nil {
			return err
		}
		return w.reviewUntilStable(
			request,
			fixResult,
			reviewNumber+1,
			autoFixes,
			fixPhase,
		)

	default:
		return &WorkerError{Phase: "auto-fix-format", Message: "worker did not return a valid STATUS after review fix"}
	}
}

func (w *Workflow) runModel(checkpoint state.ResumeCheckpoint) (packet.Result, error) {
	checkpoint, outputPath, guardBefore, err := w.prepareModelCall(checkpoint)
	if err != nil {
		return packet.Result{}, err
	}
	execution, err := w.invokeModelCall(checkpoint, outputPath, guardBefore)
	if err != nil {
		return packet.Result{}, err
	}
	execution, err = w.resolveModelCallFailure(checkpoint, outputPath, execution)
	if err != nil {
		return packet.Result{}, err
	}
	w.observeInstructionReads(execution.runResult.InstructionReads)
	if err := w.finalizeModelCallState(checkpoint, outputPath, execution); err != nil {
		return packet.Result{}, err
	}

	result, err := w.parseModelCallResult(checkpoint, execution.runResult)
	if err != nil {
		return w.handleInvalidModelResult(checkpoint, outputPath, execution, err)
	}
	taskID, err := w.state.TaskID()
	if err != nil {
		w.recordModelCall(checkpoint, execution.runResult, execution.startedAt, execution.completedAt, "state_error", "", err, outputPath, callDiagnostics{})
		return packet.Result{}, err
	}
	if err := packet.ValidateArtifacts(result.Artifacts, w.state.ArtifactDir(taskID)); err != nil {
		return w.handleInvalidModelResult(checkpoint, outputPath, execution, err)
	}
	w.recordModelCall(checkpoint, execution.runResult, execution.startedAt, execution.completedAt, "success", string(result.Status), nil, outputPath, callDiagnostics{reportedRisk: string(result.Risk)})
	w.lastProducer = state.ParentReviewProducer{Role: string(checkpoint.Role), Model: checkpoint.Model}
	return result, nil
}

func (w *Workflow) prepareModelCall(checkpoint state.ResumeCheckpoint) (state.ResumeCheckpoint, string, parentFileGuard, error) {
	outputPath := filepath.Join(w.temp, checkpoint.Phase+".log")
	if err := w.applyModelArtifactContext(&checkpoint); err != nil {
		return checkpoint, outputPath, parentFileGuard{}, err
	}
	if checkpoint.OriginalPrompt == "" {
		checkpoint.OriginalPrompt = checkpoint.Prompt
	}
	if checkpoint.Model == "" {
		return checkpoint, outputPath, parentFileGuard{}, &WorkerError{Phase: checkpoint.Phase, Message: "checkpoint model is missing"}
	}
	if checkpoint.Effort == "" {
		checkpoint.Effort = w.config.RoutineEffort
	}

	guardBefore, stopped, err := w.captureParentFileGuard(checkpoint.Role)
	if stopped {
		return checkpoint, outputPath, guardBefore, err
	}
	if checkpoint.Stage == state.ResumeStageReview {
		checkpoint.StopParentFiles = nil
	}
	if err := w.state.SaveResumeCheckpoint(checkpoint); err != nil {
		return checkpoint, outputPath, guardBefore, err
	}
	if w.stopRequested() {
		return checkpoint, outputPath, guardBefore, w.interruptBetweenCalls(checkpoint)
	}
	w.state.RecordModelCall(checkpoint.Role, checkpoint.Model)
	return checkpoint, outputPath, guardBefore, nil
}

func (w *Workflow) applyModelArtifactContext(checkpoint *state.ResumeCheckpoint) error {
	switch checkpoint.Role {
	case state.WorkerRole:
		artifactDir, err := w.state.PrepareArtifactDir()
		if err != nil {
			return err
		}
		checkpoint.Prompt = withArtifactContext(checkpoint.Prompt, artifactDir)
		if checkpoint.OriginalPrompt != "" {
			checkpoint.OriginalPrompt = withArtifactContext(checkpoint.OriginalPrompt, artifactDir)
		}
	case state.ReviewerRole:
		taskID, err := w.state.TaskID()
		if err != nil {
			return err
		}
		artifactDir := w.state.ArtifactDir(taskID)
		checkpoint.Prompt = withReviewerArtifactContext(checkpoint.Prompt, artifactDir)
		if checkpoint.OriginalPrompt != "" {
			checkpoint.OriginalPrompt = withReviewerArtifactContext(checkpoint.OriginalPrompt, artifactDir)
		}
	}
	return nil
}

func (w *Workflow) invokeModelCall(
	checkpoint state.ResumeCheckpoint,
	outputPath string,
	guardBefore parentFileGuard,
) (modelCallExecution, error) {
	execution := modelCallExecution{startedAt: w.now().UTC()}
	execution.runResult, execution.runErr = w.runner.Run(
		checkpoint.Role,
		checkpoint.Phase,
		checkpoint.Model,
		checkpoint.ReadOnly,
		checkpoint.Effort,
		checkpoint.Prompt,
		outputPath,
	)
	execution.completedAt = w.now().UTC()
	w.state.RecordModelDuration(checkpoint.Model, execution.completedAt.Sub(execution.startedAt))
	if stopped, err := w.verifyParentFileAfterCall(
		checkpoint,
		guardBefore,
		execution.runResult,
		execution.startedAt,
		execution.completedAt,
		execution.runErr,
		outputPath,
	); stopped {
		return execution, err
	}
	return execution, nil
}

func (w *Workflow) resolveModelCallFailure(
	checkpoint state.ResumeCheckpoint,
	outputPath string,
	execution modelCallExecution,
) (modelCallExecution, error) {
	if execution.runErr == nil {
		return execution, nil
	}
	var interrupted *runner.InterruptedCallError
	if errors.As(execution.runErr, &interrupted) {
		return execution, w.interruptFromCall(checkpoint, execution.runResult, execution.startedAt, execution.completedAt, execution.runErr, outputPath)
	}
	if runner.IsRecoverableGuardFailure(execution.runErr) {
		return execution, w.saveGuardRecoverableState(checkpoint, execution, outputPath)
	}

	failureClass := mergePlainFailureClass(
		runner.ClassifyProviderFailureText(runner.ReadTransientSignal(outputPath)),
		execution.runResult.PlainFailure,
	)
	if failureClass.Kind == runner.ProviderFailureZaiFiveHour {
		return execution, w.saveRateLimitedState(checkpoint, failureClass.FiveHourLimit, execution.runResult, execution.startedAt, execution.completedAt, execution.runErr, outputPath)
	}
	if err := w.handleStructuredModelFailure(checkpoint, outputPath, execution); err != nil {
		return execution, err
	}
	if failureClass.Kind != runner.ProviderFailureTransient {
		return execution, nil
	}
	return w.resolveTransientModelFailure(checkpoint, outputPath, failureClass.Detail, execution)
}

func (w *Workflow) handleStructuredModelFailure(
	checkpoint state.ResumeCheckpoint,
	outputPath string,
	execution modelCallExecution,
) error {
	var structuredErr *runner.StructuredOutputError
	if !errors.As(execution.runErr, &structuredErr) {
		return nil
	}
	if structuredErr.RetryExhausted() {
		w.state.RecordStructuredRetryExhausted()
	}
	w.recordModelCall(checkpoint, execution.runResult, execution.startedAt, execution.completedAt, "invalid_packet", "", execution.runErr, outputPath, callDiagnostics{})
	_ = w.state.ClearResumeCheckpoint()
	_ = w.state.RemoveUnreadySession(checkpoint.Role)
	return &WorkerError{
		Phase:   checkpoint.Phase + "-structured-output",
		Message: execution.runErr.Error(),
		Tail:    packet.Tail(outputPath, 20),
	}
}

func (w *Workflow) resolveTransientModelFailure(
	checkpoint state.ResumeCheckpoint,
	outputPath string,
	classification string,
	execution modelCallExecution,
) (modelCallExecution, error) {
	recovered, resumeResult, resumeStartedAt, resumeCompletedAt, recErr := w.recoverTransient(
		checkpoint,
		outputPath,
		classification,
		execution.runResult,
		execution.startedAt,
		execution.completedAt,
	)
	if recovered {
		execution.runResult = resumeResult
		execution.startedAt = resumeStartedAt
		execution.completedAt = resumeCompletedAt
		execution.runErr = nil
		return execution, nil
	}
	if errors.Is(recErr, errParentFileGuardStopped) {
		return execution, recErr
	}
	var interrupted *runner.InterruptedCallError
	if errors.As(recErr, &interrupted) {
		if resumeStartedAt.IsZero() {
			return execution, w.interruptBetweenCalls(checkpoint)
		}
		return execution, w.interruptFromCall(checkpoint, resumeResult, resumeStartedAt, resumeCompletedAt, recErr, outputPath)
	}
	var providerUnavailable *runner.ProviderUnavailableError
	if errors.As(recErr, &providerUnavailable) {
		_ = w.state.SecureArtifactDir()
		w.state.RecordProviderUnavailable(checkpoint.Model)
		return execution, recErr
	}
	var guardRecoverable *GuardRecoverableError
	if errors.As(recErr, &guardRecoverable) {
		return execution, recErr
	}
	var limitErr runner.ZaiRateLimitError
	if errors.As(recErr, &limitErr) {
		return execution, recErr
	}

	execution.runResult = resumeResult
	execution.startedAt = resumeStartedAt
	execution.completedAt = resumeCompletedAt
	execution.runErr = recErr
	execution.recoveryFatal = true
	return execution, nil
}

func (w *Workflow) finalizeModelCallState(
	checkpoint state.ResumeCheckpoint,
	outputPath string,
	execution modelCallExecution,
) error {
	if err := w.state.SecureArtifactDir(); err != nil {
		if !execution.recoveryFatal {
			w.recordModelCall(checkpoint, execution.runResult, execution.startedAt, execution.completedAt, "state_error", "", err, outputPath, callDiagnostics{})
		}
		return err
	}
	if execution.runErr != nil {
		if !execution.recoveryFatal {
			w.recordModelCall(checkpoint, execution.runResult, execution.startedAt, execution.completedAt, "error", "", execution.runErr, outputPath, callDiagnostics{})
		}
		_ = w.state.ClearResumeCheckpoint()
		_ = w.state.RemoveUnreadySession(checkpoint.Role)
		return workerError(checkpoint.Phase, outputPath, execution.runErr)
	}
	if err := w.state.ClearResumeCheckpoint(); err != nil {
		w.recordModelCall(checkpoint, execution.runResult, execution.startedAt, execution.completedAt, "state_error", "", err, outputPath, callDiagnostics{})
		return err
	}
	return nil
}

func (w *Workflow) parseModelCallResult(checkpoint state.ResumeCheckpoint, runResult runner.RunResult) (packet.Result, error) {
	result, err := packet.ParseStructured(runResult.StructuredOutput)
	if err != nil {
		return packet.Result{}, err
	}
	if checkpoint.Role == state.ReviewerRole {
		err = packet.ValidateReviewerResult(result)
		if err == nil && result.Status == packet.StatusNeedsSolDecision {
			err = w.validateReviewerDecisionBoundary()
		}
	} else {
		err = packet.ValidateWorkerResult(result)
	}
	return result, err
}

func (w *Workflow) handleInvalidModelResult(
	checkpoint state.ResumeCheckpoint,
	outputPath string,
	execution modelCallExecution,
	resultErr error,
) (packet.Result, error) {
	w.recordModelCall(checkpoint, execution.runResult, execution.startedAt, execution.completedAt, "invalid_packet", "", resultErr, outputPath, callDiagnostics{})
	if packet.IsConstraintError(resultErr) && !checkpoint.ResultCorrection {
		w.state.RecordResultCorrection()
		correctCheckpoint := checkpoint
		correctCheckpoint.Phase += resultCorrectionPhaseSuffix
		correctPrompt := resultCorrectionPrompt(resultErr.Error())
		correctCheckpoint.Prompt = correctPrompt
		correctCheckpoint.OriginalPrompt = correctPrompt
		correctCheckpoint.ResultCorrection = true
		w.pendingRetry = &callRetryContext{callID: w.lastCallID, reason: "invalid-packet-result-correction"}
		return w.runModel(correctCheckpoint)
	}
	return packet.Result{}, &WorkerError{
		Phase:   checkpoint.Phase + "-format",
		Message: resultErr.Error(),
		Tail:    packet.Tail(outputPath, 20),
	}
}

func (w *Workflow) saveRateLimitedState(
	checkpoint state.ResumeCheckpoint,
	limit runner.ZaiFiveHourLimit,
	runResult runner.RunResult,
	startedAt time.Time,
	completedAt time.Time,
	runErr error,
	outputPath string,
) error {
	if err := w.state.MarkReady(checkpoint.Role); err != nil {
		w.recordModelCall(checkpoint, runResult, startedAt, completedAt, "state_error", "", err, outputPath, callDiagnostics{})
		return err
	}
	taskID, err := w.persistRateLimitedStop(checkpoint, limit)
	if err != nil {
		w.recordModelCall(checkpoint, runResult, startedAt, completedAt, "state_error", "", err, outputPath, callDiagnostics{})
		return err
	}

	artifactErr := w.state.SecureArtifactDir()
	telemetryErr := runErr
	artifactWarning := ""
	if artifactErr != nil {
		artifactWarning = artifactErr.Error()
		telemetryErr = fmt.Errorf("%w; %w", runErr, artifactErr)
	}
	w.recordModelCall(checkpoint, runResult, startedAt, completedAt, "rate_limited", "", telemetryErr, outputPath, callDiagnostics{})
	return runner.ZaiRateLimitError{
		Phase:           checkpoint.Phase,
		Limit:           limit,
		TaskID:          taskID,
		RepoRoot:        w.config.RepoRoot,
		RepoShort:       w.config.RepoShort,
		ArtifactWarning: artifactWarning,
	}
}

func (w *Workflow) persistRateLimitedStop(checkpoint state.ResumeCheckpoint, limit runner.ZaiFiveHourLimit) (string, error) {
	checkpoint.SetStopKind(state.ResumeStopRateLimited)
	checkpoint.ResetAtCST = limit.ResetAtCST
	checkpoint.ResetAtRFC3339 = limit.ResetAtRFC3339

	_ = w.attachStopRepositoryBoundary(&checkpoint)
	if err := w.state.SaveResumeCheckpoint(checkpoint); err != nil {
		return "", err
	}
	if err := w.state.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		return "", err
	}
	w.state.RecordRateLimit(checkpoint.Model)
	taskID, err := w.state.TaskID()
	if err != nil {
		return "", err
	}
	return taskID, nil
}

func (w *Workflow) saveProbeRateLimited(checkpoint state.ResumeCheckpoint, limit runner.ZaiFiveHourLimit) error {
	taskID, err := w.persistRateLimitedStop(checkpoint, limit)
	if err != nil {
		return err
	}
	_ = w.state.SecureArtifactDir()
	return runner.ZaiRateLimitError{
		Phase:     checkpoint.Phase,
		Limit:     limit,
		TaskID:    taskID,
		RepoRoot:  w.config.RepoRoot,
		RepoShort: w.config.RepoShort,
	}
}

func (w *Workflow) recoverTransient(
	checkpoint state.ResumeCheckpoint,
	outputPath string,
	classification string,
	initialResult runner.RunResult,
	initialStartedAt time.Time,
	initialCompletedAt time.Time,
) (bool, runner.RunResult, time.Time, time.Time, error) {
	w.recordModelCall(checkpoint, initialResult, initialStartedAt, initialCompletedAt, "transient_error", "", fmt.Errorf("transient provider failure: %s", classification), outputPath, callDiagnostics{providerClassification: classification})
	if err := w.state.MarkReady(checkpoint.Role); err != nil {
		return false, runner.RunResult{}, time.Time{}, time.Time{}, err
	}
	return w.recoveryLoop(checkpoint, classification, false, func() (bool, runner.RunResult, time.Time, time.Time, error) {
		w.pendingRetry = &callRetryContext{callID: w.lastCallID, reason: transientRetryReason(classification)}
		return w.runResumedTask(checkpoint, outputPath)
	})
}

func transientRetryReason(classification string) string {
	return "transient-provider-failure:" + classification
}

func (w *Workflow) gateResumeOnProbe(checkpoint state.ResumeCheckpoint) error {
	_, _, _, _, err := w.recoveryLoop(checkpoint, checkpoint.ProviderUnavailableClassification, true, func() (bool, runner.RunResult, time.Time, time.Time, error) {
		return true, runner.RunResult{}, time.Time{}, time.Time{}, nil
	})
	return err
}

func (w *Workflow) runResumedTask(checkpoint state.ResumeCheckpoint, outputPath string) (bool, runner.RunResult, time.Time, time.Time, error) {

	guardBefore, stopped, err := w.captureParentFileGuard(checkpoint.Role)
	if stopped {
		return false, runner.RunResult{}, time.Time{}, time.Time{}, err
	}
	startedAt := w.now().UTC()

	w.state.RecordTransientRetry()
	w.state.RecordModelCall(checkpoint.Role, checkpoint.Model)
	result, runErr := w.runner.Run(
		checkpoint.Role,
		checkpoint.Phase,
		checkpoint.Model,
		checkpoint.ReadOnly,
		checkpoint.Effort,
		checkpoint.Prompt,
		outputPath,
	)
	completedAt := w.now().UTC()
	w.state.RecordModelDuration(checkpoint.Model, completedAt.Sub(startedAt))
	if stopped, err := w.verifyParentFileAfterCall(checkpoint, guardBefore, result, startedAt, completedAt, runErr, outputPath); stopped {
		return false, result, startedAt, completedAt, err
	}

	var interrupted *runner.InterruptedCallError
	if runErr != nil && errors.As(runErr, &interrupted) {
		return false, result, startedAt, completedAt, runErr
	}
	if runErr != nil && runner.IsRecoverableGuardFailure(runErr) {
		execution := modelCallExecution{
			runResult:   result,
			startedAt:   startedAt,
			completedAt: completedAt,
			runErr:      runErr,
		}
		return false, result, startedAt, completedAt, w.saveGuardRecoverableState(checkpoint, execution, outputPath)
	}
	if runErr == nil {
		return true, result, startedAt, completedAt, nil
	}
	class := mergePlainFailureClass(
		runner.ClassifyProviderFailureText(runner.ReadTransientSignal(outputPath)),
		result.PlainFailure,
	)
	if class.Kind == runner.ProviderFailureZaiFiveHour {
		err := w.saveRateLimitedState(checkpoint, class.FiveHourLimit, result, startedAt, completedAt, runErr, outputPath)
		return false, result, startedAt, completedAt, err
	}
	if class.Kind != runner.ProviderFailureTransient {

		w.recordModelCall(checkpoint, result, startedAt, completedAt, "error", "", runErr, outputPath, callDiagnostics{})
		return false, result, startedAt, completedAt, runErr
	}
	w.recordModelCall(checkpoint, result, startedAt, completedAt, "transient_error", "", runErr, outputPath, callDiagnostics{providerClassification: class.Detail})
	return false, runner.RunResult{}, startedAt, completedAt, nil
}

func mergePlainFailureClass(base runner.ProviderFailureClass, plain runner.ProviderFailureClass) runner.ProviderFailureClass {
	switch {
	case plain.Kind == runner.ProviderFailureZaiFiveHour:
		return plain
	case base.Kind == runner.ProviderFailureZaiFiveHour:
		return base
	case base.Kind == runner.ProviderFailureTransient:
		return base
	case plain.Kind == runner.ProviderFailureTransient:
		return plain
	default:
		return base
	}
}

func (w *Workflow) recoveryLoop(
	checkpoint state.ResumeCheckpoint,
	classification string,
	firstProbeImmediate bool,
	onProbeSuccess func() (bool, runner.RunResult, time.Time, time.Time, error),
) (bool, runner.RunResult, time.Time, time.Time, error) {
	recoveryStart := w.now().UTC()
	deadline := recoveryStart.Add(providerUnavailableDeadline)
	probes := 0
	sleeps := 0
	exhaustClassification := classification

	for probes < maxTransientProbes {
		nextSleeps, proceed, err := w.waitForRecoveryProbe(checkpoint, firstProbeImmediate, probes, sleeps, deadline)
		if err != nil {
			return false, runner.RunResult{}, time.Time{}, time.Time{}, err
		}
		if !proceed {
			break
		}
		sleeps = nextSleeps

		probes++
		done, recovered, result, startedAt, completedAt, nextClassification, err := w.runRecoveryAttempt(checkpoint, probes, onProbeSuccess)
		if nextClassification != "" {
			exhaustClassification = nextClassification
		}
		if err != nil {
			return false, result, startedAt, completedAt, err
		}
		if done {
			return recovered, result, startedAt, completedAt, nil
		}
	}

	pErr, saveErr := w.saveProviderUnavailable(checkpoint, exhaustClassification, probes, recoveryStart)
	if saveErr != nil {
		return false, runner.RunResult{}, time.Time{}, time.Time{}, saveErr
	}
	return false, runner.RunResult{}, time.Time{}, time.Time{}, pErr
}

func (w *Workflow) waitForRecoveryProbe(
	checkpoint state.ResumeCheckpoint,
	firstProbeImmediate bool,
	probes int,
	sleeps int,
	deadline time.Time,
) (int, bool, error) {
	if firstProbeImmediate && probes == 0 {
		return sleeps, true, nil
	}
	wait, ok := w.backoffWait(sleeps, deadline)
	if !ok {
		return sleeps, false, nil
	}
	if w.sleepInterruptible(wait) {
		return sleeps, false, &runner.InterruptedCallError{Phase: checkpoint.Phase}
	}
	sleeps++
	return sleeps, !w.now().After(deadline), nil
}

func (w *Workflow) runRecoveryAttempt(
	checkpoint state.ResumeCheckpoint,
	attempt int,
	onProbeSuccess func() (bool, runner.RunResult, time.Time, time.Time, error),
) (bool, bool, runner.RunResult, time.Time, time.Time, string, error) {
	success, classification, startedAt, completedAt, err := w.runRecoveryProbe(checkpoint, attempt)
	if err != nil || !success {
		return false, false, runner.RunResult{}, startedAt, completedAt, classification, err
	}
	recovered, result, startedAt, completedAt, err := onProbeSuccess()
	return recovered || err != nil, recovered, result, startedAt, completedAt, classification, err
}

func (w *Workflow) runRecoveryProbe(checkpoint state.ResumeCheckpoint, attempt int) (bool, string, time.Time, time.Time, error) {
	startedAt := w.now().UTC()
	probeResult, probeErr := w.runner.Probe(checkpoint.Model)
	completedAt := w.now().UTC()
	if probeErr == nil {
		if contractErr := runner.ValidateProbeResult(probeResult); contractErr != nil {
			probeErr = &runner.ProbeInvalidResponseError{Model: checkpoint.Model, Reason: contractErr}
		}
	}
	w.recordProbeCall(checkpoint, probeResult, attempt, startedAt, completedAt, probeErr)
	if probeErr == nil {
		return true, "", startedAt, completedAt, nil
	}

	class := runner.ClassifyProviderFailureText(probeErr.Error())
	if class.Kind == runner.ProviderFailureZaiFiveHour {
		return false, "", startedAt, completedAt, w.saveProbeRateLimited(checkpoint, class.FiveHourLimit)
	}
	if class.Kind == runner.ProviderFailureTransient {
		return false, "", startedAt, completedAt, nil
	}
	var probeInvalid *runner.ProbeInvalidResponseError
	if errors.As(probeErr, &probeInvalid) && !runner.DetectProbeFatalSignal(probeErr.Error()) {
		return false, runner.ProbeContractFailure, startedAt, completedAt, nil
	}
	return false, "", startedAt, completedAt, probeErr
}

func (w *Workflow) backoffWait(sleeps int, deadline time.Time) (time.Duration, bool) {
	if sleeps >= len(transientBackoffSchedule) {
		return 0, false
	}
	remaining := deadline.Sub(w.now())
	if remaining <= 0 {
		return 0, false
	}
	wait := w.jitter(transientBackoffSchedule[sleeps])
	if wait > remaining {
		wait = remaining
	}
	return wait, true
}

func (w *Workflow) saveProviderUnavailable(checkpoint state.ResumeCheckpoint, classification string, probes int, recoveryStart time.Time) (*runner.ProviderUnavailableError, error) {
	checkpoint.SetStopKind(state.ResumeStopProviderUnavailable)
	checkpoint.ProviderUnavailableClassification = classification
	checkpoint.ProviderUnavailableProbes = probes
	checkpoint.ProviderUnavailableStartedAt = recoveryStart

	_ = w.attachStopRepositoryBoundary(&checkpoint)
	if err := w.state.SaveResumeCheckpoint(checkpoint); err != nil {
		return nil, err
	}
	if err := w.state.SetTaskStatus(state.TaskStatusProviderUnavailable); err != nil {
		return nil, err
	}
	elapsed := w.now().Sub(recoveryStart)
	w.recordProviderUnavailableEvent(checkpoint, classification, probes, elapsed)
	taskID, _ := w.state.TaskID()
	return &runner.ProviderUnavailableError{
		Phase:          checkpoint.Phase,
		Classification: classification,
		Probes:         probes,
		Elapsed:        elapsed,
		TaskID:         taskID,
		RepoRoot:       w.config.RepoRoot,
		RepoShort:      w.config.RepoShort,
	}, nil
}

func (w *Workflow) interruptFromCall(
	checkpoint state.ResumeCheckpoint,
	runResult runner.RunResult,
	startedAt time.Time,
	completedAt time.Time,
	runErr error,
	outputPath string,
) error {
	w.recordModelCall(checkpoint, runResult, startedAt, completedAt, "interrupted", "", runErr, outputPath, callDiagnostics{})
	return w.persistInterruptedStop(checkpoint, runErr)
}

func (w *Workflow) interruptBetweenCalls(checkpoint state.ResumeCheckpoint) error {
	w.recordInterruptedEvent(checkpoint)
	return w.persistInterruptedStop(checkpoint, nil)
}

func (w *Workflow) persistInterruptedStop(checkpoint state.ResumeCheckpoint, cause error) error {
	checkpoint.SetStopKind(state.ResumeStopInterrupted)

	_ = w.attachStopRepositoryBoundary(&checkpoint)

	if files, filesErr := state.CaptureStopDirtyFiles(w.config.RepoRoot); filesErr == nil {
		checkpoint.StopDirtyFiles = files
	}

	_ = state.CaptureStopPatches(w.config, w.state)

	if w.state.ReadOr(string(checkpoint.Role)+".id", "") != "" {
		if err := w.state.MarkReady(checkpoint.Role); err != nil {
			return err
		}
	}
	if err := w.state.SaveResumeCheckpoint(checkpoint); err != nil {
		return err
	}
	if err := w.state.SetTaskStatus(state.TaskStatusInterrupted); err != nil {
		return err
	}
	_ = w.state.SecureArtifactDir()
	taskID, _ := w.state.TaskID()

	cleanupWarning := ""
	if cause != nil {
		var interrupted *runner.InterruptedCallError
		if errors.As(cause, &interrupted) {
			cleanupWarning = interrupted.CleanupWarning
		}
	}
	if w.stop != nil {
		w.stop.NotifyInterrupted(taskID, cleanupWarning)
	}
	stopped := &runner.InterruptedCallError{
		Phase:          checkpoint.Phase,
		TaskID:         taskID,
		RepoRoot:       w.config.RepoRoot,
		CleanupWarning: cleanupWarning,
	}
	return stopped
}

func (w *Workflow) recordInterruptedEvent(checkpoint state.ResumeCheckpoint) {
	now := w.now().UTC()
	w.state.RecordModelCallLog(state.ModelCallLog{
		TaskID:      w.state.ReadOr("task.id", "unknown"),
		CallType:    state.CallTypeEvent,
		StartedAt:   now,
		CompletedAt: now,
		Phase:       checkpoint.Phase + "-user-interrupted",
		Role:        checkpoint.Role,
		ModelAlias:  checkpoint.Model,
		Outcome:     "user_interrupted",
	})
}

func (w *Workflow) sleepInterruptible(duration time.Duration) bool {
	if w.stop == nil {
		w.sleep(duration)
		return false
	}
	done := make(chan struct{})
	go func() {
		w.sleep(duration)
		close(done)
	}()
	select {
	case <-done:
		return false
	case <-w.stop.Requested():
		return true
	}
}

func (w *Workflow) recordProviderUnavailableEvent(checkpoint state.ResumeCheckpoint, classification string, probes int, elapsed time.Duration) {
	now := w.now().UTC()
	w.state.RecordModelCallLog(state.ModelCallLog{
		TaskID:                 w.state.ReadOr("task.id", "unknown"),
		CallType:               state.CallTypeEvent,
		StartedAt:              now,
		CompletedAt:            now,
		Phase:                  checkpoint.Phase + "-provider-unavailable",
		Role:                   checkpoint.Role,
		ModelAlias:             checkpoint.Model,
		Outcome:                "provider_unavailable",
		ProviderClassification: classification,
		ProbeAttempt:           probes,
		RetryElapsedMS:         elapsed.Milliseconds(),
	})
}

func (w *Workflow) recordProbeCall(
	checkpoint state.ResumeCheckpoint,
	probe runner.ProbeResult,
	attempt int,
	startedAt time.Time,
	completedAt time.Time,
	probeErr error,
) {
	outcome := "probe_success"
	errorText := ""
	if probeErr != nil {
		outcome = "probe_failure"
		errorText = boundedText(probeErr.Error(), packet.MaxDiagnosticBytes)
	}
	w.state.RecordProbeOutcome(outcome)
	promptHash := sha256.Sum256([]byte(runner.ProbePrompt))
	response := probe.Response
	if !w.config.TelemetryContent {
		response = ""
	}
	resolvedUsage := make(map[string]state.ResolvedModelUsage, len(probe.ModelUsage))
	for model, usage := range probe.ModelUsage {
		resolvedUsage[model] = state.ResolvedModelUsage{
			InputTokens:              usage.InputTokens,
			CacheCreationInputTokens: usage.CacheCreationInputTokens,
			CacheReadInputTokens:     usage.CacheReadInputTokens,
			OutputTokens:             usage.OutputTokens,
			CostUSD:                  usage.CostUSD,
		}
	}
	w.state.RecordModelCallLog(state.ModelCallLog{
		TaskID:              w.state.ReadOr("task.id", "unknown"),
		CallType:            state.CallTypeProbe,
		SessionID:           "none",
		StartedAt:           startedAt,
		CompletedAt:         completedAt,
		Phase:               fmt.Sprintf("%s-probe-%d", checkpoint.Phase, attempt),
		Role:                checkpoint.Role,
		ModelAlias:          checkpoint.Model,
		ResolvedModelUsage:  resolvedUsage,
		Effort:              "low",
		ReadOnly:            true,
		Outcome:             outcome,
		ProbeAttempt:        attempt,
		PromptBytes:         len(runner.ProbePrompt),
		PromptSHA256:        hex.EncodeToString(promptHash[:]),
		Response:            response,
		ResponseBytes:       len(probe.Response),
		Error:               errorText,
		TopLevelUsage:       state.TokenUsage(probe.Usage),
		WallDurationMS:      completedAt.Sub(startedAt).Milliseconds(),
		ClaudeDurationMS:    probe.DurationMS,
		ClaudeAPIDurationMS: probe.DurationAPIMS,
		TotalCostUSD:        probe.TotalCostUSD,
	})
}

func (w *Workflow) recordModelCall(
	checkpoint state.ResumeCheckpoint,
	runResult runner.RunResult,
	startedAt time.Time,
	completedAt time.Time,
	outcome string,
	packetStatus string,
	callErr error,
	outputPath string,
	diag callDiagnostics,
) {
	entry := w.buildModelCallLog(checkpoint, runResult, startedAt, completedAt, outcome, packetStatus, callErr, outputPath)
	if entry.CallID == "" {
		if callID, err := state.NewUUID(); err == nil {
			entry.CallID = callID
		}
	}
	w.applyCallDiagnostics(&entry, checkpoint, outcome, callErr, diag)
	w.state.RecordModelCallLog(entry)
	w.lastCallID = entry.CallID
}

func (w *Workflow) buildModelCallLog(
	checkpoint state.ResumeCheckpoint,
	runResult runner.RunResult,
	startedAt time.Time,
	completedAt time.Time,
	outcome string,
	packetStatus string,
	callErr error,
	outputPath string,
) state.ModelCallLog {
	response := runResult.Response
	if response == "" {
		response = packet.Tail(outputPath, packet.MaxDiagnosticBytes)
	}
	promptHash := sha256.Sum256([]byte(checkpoint.Prompt))
	responseHash := sha256.Sum256([]byte(response))
	errorText := modelCallErrorText(callErr)
	promptContent, systemPromptContent, responseContent := w.telemetryContents(checkpoint.Prompt, runResult.SystemPrompt, response)
	return state.ModelCallLog{
		CallID:                            runResult.CallID,
		TaskID:                            w.state.ReadOr("task.id", "unknown"),
		CallType:                          state.CallTypeTask,
		SessionID:                         modelSessionID(w.state, checkpoint.Role, runResult.SessionID),
		StartedAt:                         startedAt,
		CompletedAt:                       completedAt,
		Phase:                             checkpoint.Phase,
		Role:                              checkpoint.Role,
		ModelAlias:                        checkpoint.Model,
		ResolvedModelID:                   runResult.ResolvedModelID,
		ConfiguredAutoCompactWindowTokens: runResult.ConfiguredAutoCompactWindowTokens,
		KnownModelContextWindowTokens:     runResult.KnownModelContextWindowTokens,
		DeclaredMaxContextWindowTokens:    runResult.DeclaredMaxContextWindowTokens,
		ContextWindowSource:               runResult.ContextWindowSource,
		ResolvedModelUsage:                resolvedModelUsage(runResult.ModelUsage),
		Effort:                            checkpoint.Effort,
		ReadOnly:                          checkpoint.ReadOnly,
		Resumed:                           runResult.Resumed,
		Outcome:                           outcome,
		PacketStatus:                      packetStatus,
		Prompt:                            promptContent,
		PromptBytes:                       len([]byte(checkpoint.Prompt)),
		PromptSHA256:                      hex.EncodeToString(promptHash[:]),
		SystemPromptBytes:                 runResult.SystemPromptBytes,
		SystemPromptSHA256:                runResult.SystemPromptSHA256,
		SystemPrompt:                      systemPromptContent,
		Response:                          responseContent,
		ResponseBytes:                     len([]byte(response)),
		ResponseSHA256:                    hex.EncodeToString(responseHash[:]),
		Error:                             errorText,
		TopLevelUsage:                     topLevelUsage(runResult.TopLevelUsage),
		Runtime:                           runResult.Runtime,
		WallDurationMS:                    completedAt.Sub(startedAt).Milliseconds(),
		ClaudeDurationMS:                  runResult.DurationMS,
		ClaudeAPIDurationMS:               runResult.DurationAPIMS,
		TopLevelTurns:                     runResult.TopLevelTurns,
		TotalCostUSD:                      runResult.TotalCostUSD,
	}
}

func topLevelUsage(usage runner.TokenUsage) state.TokenUsage {
	return state.TokenUsage{
		InputTokens:              usage.InputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		OutputTokens:             usage.OutputTokens,
	}
}

func modelCallErrorText(callErr error) string {
	if callErr == nil {
		return ""
	}
	return boundedText(callErr.Error(), packet.MaxDiagnosticBytes)
}

func resolvedModelUsage(usageByModel map[string]runner.ModelUsage) map[string]state.ResolvedModelUsage {
	resolved := make(map[string]state.ResolvedModelUsage, len(usageByModel))
	for model, usage := range usageByModel {
		resolved[model] = state.ResolvedModelUsage{
			InputTokens:              usage.InputTokens,
			CacheCreationInputTokens: usage.CacheCreationInputTokens,
			CacheReadInputTokens:     usage.CacheReadInputTokens,
			OutputTokens:             usage.OutputTokens,
			CostUSD:                  usage.CostUSD,
		}
	}
	return resolved
}

func (w *Workflow) telemetryContents(prompt string, systemPrompt string, response string) (string, string, string) {
	if !w.config.TelemetryContent {
		return "", "", ""
	}
	return prompt, systemPrompt, response
}

func (w *Workflow) applyCallDiagnostics(entry *state.ModelCallLog, checkpoint state.ResumeCheckpoint, outcome string, callErr error, diag callDiagnostics) {
	if diag.reportedRisk != "" {
		if checkpoint.Role == state.ReviewerRole {
			entry.ReviewerReportedRisk = diag.reportedRisk
		} else {
			entry.WorkerReportedRisk = diag.reportedRisk
		}
	}
	w.applyEffectiveRiskDiagnostic(entry, checkpoint)
	if diag.providerClassification != "" {
		entry.ProviderClassification = diag.providerClassification
	}
	if w.currentResumeSource != "" {
		entry.ResumeSource = w.currentResumeSource
		w.currentResumeSource = ""
	}
	if w.pendingRetry != nil {
		entry.RetryOf = w.pendingRetry.callID
		entry.RetryReason = w.pendingRetry.reason
		w.pendingRetry = nil
	}
	if outcome == "invalid_packet" && callErr != nil {
		category := packet.RejectCategory(callErr)
		if runner.IsStructuredOutputError(callErr) {
			category = "structured-output"
		}
		entry.PacketRejectReason = category
		w.state.RecordPacketReject(category)
	}
	if checkpoint.Role == state.ReviewerRole && outcome == "success" && w.pendingSnapshot != nil {
		entry.Snapshot = w.pendingSnapshot
		w.pendingSnapshot = nil
	}
}

func (w *Workflow) applyEffectiveRiskDiagnostic(entry *state.ModelCallLog, checkpoint state.ResumeCheckpoint) {
	if checkpoint.EffectiveRisk == "" {
		return
	}
	entry.EffectiveRisk = checkpoint.EffectiveRisk
	entry.RiskFloorSource = checkpoint.EffectiveRiskSource
	if checkpoint.Role != state.ReviewerRole || checkpoint.EffectiveRisk != highRiskValue {
		return
	}
	category := riskFloorCategory(checkpoint.EffectiveRiskSource)
	entry.RiskFloorCategory = category
	w.state.RecordRiskFloor(category)
}

func riskFloorCategory(source string) string {
	if source == "" {
		return ""
	}
	var categories []string
	for _, raw := range strings.Split(source, ";") {
		name := strings.SplitN(raw, ":", 2)[0]
		if name != "" {
			categories = append(categories, name)
		}
	}
	return strings.Join(categories, ",")
}

func modelSessionID(st *state.StateStore, role state.SessionRole, fromRunner string) string {
	if fromRunner != "" {
		return fromRunner
	}
	return st.ReadOr(string(role)+".id", "unknown")
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

func workerError(phase string, outputPath string, runErr error) error {
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
	w.state.RecordSolResult(value, w.lastProducer)
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
	if !reviewResumeParentBaselineMatches(saved, current) || saved.ParentFiles == nil || checkpoint.StopParentFiles == nil || current.ParentFiles == nil {
		return false
	}
	now := *current.ParentFiles
	changedDuringStop := false
	for _, path := range parentStatePaths(*saved.ParentFiles, *checkpoint.StopParentFiles, now) {
		reviewStart := state.FindParentFileState(*saved.ParentFiles, path)
		stop := state.FindParentFileState(*checkpoint.StopParentFiles, path)
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
	if err := w.state.ClearResumeCheckpoint(); err != nil {
		return err
	}
	if err := w.state.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
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
