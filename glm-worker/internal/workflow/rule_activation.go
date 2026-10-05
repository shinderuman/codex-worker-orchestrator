package workflow

import (
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) currentRequiredWorkerRules() ([]workerRule, error) {
	if w.config.CodexConfigDir == "" {
		return nil, nil
	}
	baselineHead := w.state.ReadOr("baseline-head", "")
	paths, err := w.collectChangedPaths(w.config.RepoRoot, baselineHead)
	if err != nil {
		return nil, fmt.Errorf("deterministic rule changes: %w", err)
	}
	return requiredWorkerRules(w.config.RepoRoot, paths), nil
}

func checkpointActivatedRules(checkpoint state.ResumeCheckpoint) map[workerRule]struct{} {
	result := make(map[workerRule]struct{})
	for _, file := range checkpoint.ActivatedRuleFiles {
		if rule, ok := workerRuleForFile(file); ok {
			result[rule] = struct{}{}
		}
	}
	return result
}

func setCheckpointActivatedRules(checkpoint *state.ResumeCheckpoint, activated map[workerRule]struct{}) {
	checkpoint.ActivatedRuleFiles = workerRuleFileNames(orderedWorkerRules(activated))
	if len(checkpoint.ActivatedRuleFiles) == 0 {
		checkpoint.ActivatedRuleFiles = nil
	}
}

func (w *Workflow) activateCheckpointRules(checkpoint state.ResumeCheckpoint) (state.ResumeCheckpoint, map[workerRule]struct{}, error) {
	checkpoint, err := w.activateDecisionBoundaryContext(checkpoint)
	if err != nil {
		return checkpoint, nil, err
	}
	required, err := w.currentRequiredWorkerRules()
	if err != nil {
		return checkpoint, nil, err
	}
	activated := checkpointActivatedRules(checkpoint)
	missing := missingWorkerRules(required, activated)
	checkpoint.Prompt, err = appendWorkerRuleContext(checkpoint.Prompt, w.config.CodexConfigDir, missing)
	if err != nil {
		return checkpoint, nil, err
	}
	if checkpoint.OriginalPrompt != "" {
		checkpoint.OriginalPrompt, err = appendWorkerRuleContext(checkpoint.OriginalPrompt, w.config.CodexConfigDir, missing)
		if err != nil {
			return checkpoint, nil, err
		}
	}
	for _, rule := range missing {
		activated[rule] = struct{}{}
	}
	setCheckpointActivatedRules(&checkpoint, activated)
	return checkpoint, activated, nil
}

func (w *Workflow) withCurrentRuleContext(prompt string) (string, error) {
	required, err := w.currentRequiredWorkerRules()
	if err != nil {
		return "", err
	}
	prompt, err = appendWorkerRuleContext(prompt, w.config.CodexConfigDir, required)
	if err != nil {
		return "", err
	}
	boundary, err := w.reviewerDecisionBoundaryContext(w.readActiveTaskState())
	if err != nil {
		return "", err
	}
	if boundary == "" {
		return prompt, nil
	}
	return strings.TrimRight(prompt, "\n") + boundary, nil
}

func (w *Workflow) resetInstructionReadObservation() {
	w.observedInstructionReads = make(map[string]struct{})
}

func (w *Workflow) observeInstructionReads(files []string) {
	if len(files) == 0 {
		return
	}
	if w.observedInstructionReads == nil {
		w.observedInstructionReads = make(map[string]struct{})
	}
	for _, file := range files {
		w.observedInstructionReads[file] = struct{}{}
	}
}

func (w *Workflow) observedWorkerRules() map[workerRule]struct{} {
	result := make(map[workerRule]struct{})
	for file := range w.observedInstructionReads {
		if rule, ok := workerRuleForFile(file); ok {
			result[rule] = struct{}{}
		}
	}
	return result
}

func mergeWorkerRuleSets(target map[workerRule]struct{}, source map[workerRule]struct{}) {
	for rule := range source {
		target[rule] = struct{}{}
	}
}

func missingWorkerRules(required []workerRule, activated map[workerRule]struct{}) []workerRule {
	var missing []workerRule
	for _, rule := range required {
		if _, ok := activated[rule]; !ok {
			missing = append(missing, rule)
		}
	}
	return missing
}

func (w *Workflow) runWorkerModelWithRuleActivation(checkpoint state.ResumeCheckpoint) (packet.Result, error) {
	if checkpoint.ReportOnly {
		w.resetInstructionReadObservation()
		return w.runModel(checkpoint)
	}
	prepared, activated, err := w.activateCheckpointRules(checkpoint)
	if err != nil {
		return packet.Result{}, err
	}
	w.resetInstructionReadObservation()
	result, err := w.runModel(prepared)
	if err != nil {
		return packet.Result{}, err
	}
	mergeWorkerRuleSets(activated, w.observedWorkerRules())
	return w.convergeWorkerRuleActivation(prepared, result, activated)
}

func (w *Workflow) convergeWorkerRuleActivation(
	checkpoint state.ResumeCheckpoint,
	result packet.Result,
	activated map[workerRule]struct{},
) (packet.Result, error) {
	stopped, err := w.stopForQualitySurfaceApproval(checkpoint, result)
	if err != nil || stopped {
		return result, err
	}
	return w.convergeApprovedWorkerRules(checkpoint, result, activated, 1)
}

func (w *Workflow) convergeApprovedWorkerRules(
	checkpoint state.ResumeCheckpoint,
	result packet.Result,
	activated map[workerRule]struct{},
	round int,
) (packet.Result, error) {
	if result.Status != packet.StatusImplemented {
		return result, nil
	}
	required, err := w.currentRequiredWorkerRules()
	if err != nil {
		return packet.Result{}, err
	}
	missing := missingWorkerRules(required, activated)
	if len(missing) == 0 {
		return w.finishWorkerRuleConvergence(checkpoint, result)
	}
	return w.runWorkerRuleCorrection(checkpoint, result, activated, missing, round)
}

func (w *Workflow) finishWorkerRuleConvergence(
	checkpoint state.ResumeCheckpoint,
	result packet.Result,
) (packet.Result, error) {
	result, err := applyCheckpointParentValidation(checkpoint, result)
	if err != nil {
		return packet.Result{}, err
	}
	return w.convergeParentValidation(checkpoint, result)
}

func (w *Workflow) runWorkerRuleCorrection(
	checkpoint state.ResumeCheckpoint,
	_ packet.Result,
	activated map[workerRule]struct{},
	missing []workerRule,
	round int,
) (packet.Result, error) {
	correction, err := w.ruleActivationCorrectionCheckpoint(checkpoint, missing, round)
	if err != nil {
		return packet.Result{}, err
	}
	for _, rule := range missing {
		activated[rule] = struct{}{}
	}
	w.resetInstructionReadObservation()
	result, err := w.runModel(correction)
	if err != nil {
		return packet.Result{}, err
	}
	mergeWorkerRuleSets(activated, w.observedWorkerRules())
	stopped, err := w.stopForQualitySurfaceApproval(correction, result)
	if err != nil || stopped {
		return result, err
	}
	return w.convergeApprovedWorkerRules(correction, result, activated, round+1)
}

func (w *Workflow) ruleActivationCorrectionCheckpoint(
	parent state.ResumeCheckpoint,
	rules []workerRule,
	round int,
) (state.ResumeCheckpoint, error) {
	block, err := workerRuleContextBlock(w.config.CodexConfigDir, rules)
	if err != nil {
		return state.ResumeCheckpoint{}, err
	}
	activeTaskPath := w.readActiveTaskState()
	primaryAuthority := activeTaskPromptBlock(activeTaskPath)
	requestAuthority := modelRequestAuthorityBlock("ORIGINAL_USER_REQUEST", parent.Request, activeTaskPath)
	prompt := fmt.Sprintf(`MODE: APPLY_DETERMINISTIC_RULES

%sPREVIOUS_SOL_DECISION:
%s

%s%s
実diffに対してwrapperが必要contractの未適用を検出しました。
上記contract本文を現在のworking treeへ適用し、違反があれば修正してください。
タスク範囲を広げず、必要なtest/lint/buildと自己確認を行い、通常のworker結果を返してください。
`, requestAuthority, parent.Decision, primaryAuthority, block)
	correction := parent
	activated := checkpointActivatedRules(correction)
	for _, rule := range rules {
		activated[rule] = struct{}{}
	}
	setCheckpointActivatedRules(&correction, activated)
	correction.Phase = fmt.Sprintf("%s-rule-activation-%d", parent.Phase, round)
	correction.Prompt = prompt
	correction.OriginalPrompt = prompt
	correction.DecisionBoundaryApplied = false
	correction, err = w.activateDecisionBoundaryContext(correction)
	if err != nil {
		return state.ResumeCheckpoint{}, err
	}
	return correction, nil
}

func (w *Workflow) activatedRulesForCheckpoint(checkpoint state.ResumeCheckpoint) map[workerRule]struct{} {
	result := checkpointActivatedRules(checkpoint)
	mergeWorkerRuleSets(result, w.observedWorkerRules())
	return result
}
