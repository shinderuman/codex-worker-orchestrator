package workflow

import (
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

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
	handled, err := w.handleRepositoryQualityViolation(request, workerResult, reviewNumber, autoFixes, workerPhase)
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
	workerPhase string,
) (bool, error) {
	qualityReport, err := w.qualityGate(w.config.RepoRoot)
	if err != nil {
		return true, w.saveQualityGateStop(request, workerResult, reviewNumber, autoFixes, workerPhase, err)
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
	if err := w.state.FinishReview(status); err != nil {
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
	harnessActive, err := w.repositoryHarnessActive()
	if err != nil {
		return selfProtectionDecision{High: true, Source: "classify-error", HitPath: err.Error()}, qualityEvidenceDecision{}
	}
	if !harnessActive {
		return selfProtectionDecision{}, qualityEvidenceDecision{}
	}
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
		if err := w.state.WaitForDecision(); err != nil {
			return err
		}
		return w.emitResult(fixResult)

	case packet.StatusImplemented:
		if err := w.state.ContinueAfterWorkerResult(); err != nil {
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
