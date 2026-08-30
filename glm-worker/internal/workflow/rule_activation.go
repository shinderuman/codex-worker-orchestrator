package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type workerRule string

const (
	ruleTesting          workerRule = "testing"
	ruleStateTransitions workerRule = "state-transitions"
	ruleCLI              workerRule = "cli"
	ruleGo               workerRule = "go"
	ruleJavaScript       workerRule = "javascript"
	rulePHP              workerRule = "php"
	ruleESLint           workerRule = "eslint"

	deterministicRuleMarker      = "DETERMINISTIC_RULE_ACTIVATION:"
	deterministicRuleFilesMarker = "RULE_FILES:"
)

var workerRuleOrder = []workerRule{
	ruleTesting,
	ruleStateTransitions,
	ruleCLI,
	ruleGo,
	ruleJavaScript,
	rulePHP,
	ruleESLint,
}

var workerRuleFiles = map[workerRule]string{
	ruleTesting:          "testing.md",
	ruleStateTransitions: "state-transitions.md",
	ruleCLI:              "cli.md",
	ruleGo:               "go.md",
	ruleJavaScript:       "javascript.md",
	rulePHP:              "php.md",
	ruleESLint:           "eslint.md",
}

var stateRuleTokens = map[string]struct{}{
	"state": {}, "states": {}, "config": {}, "configs": {}, "configuration": {},
	"setting": {}, "settings": {}, "cache": {}, "caches": {},
	"migration": {}, "migrations": {}, "upgrade": {}, "upgrades": {},
	"manifest": {}, "manifests": {}, "sidecar": {}, "sidecars": {},
	"persistent": {}, "persistence": {}, "storage": {}, "store": {},
	"database": {}, "databases": {}, "db": {},
}

var cliRuleTokens = map[string]struct{}{
	"cmd": {}, "cli": {}, "bin": {}, "command": {}, "commands": {},
	"flag": {}, "flags": {}, "arg": {}, "args": {}, "argv": {},
	"option": {}, "options": {}, "subcommand": {}, "subcommands": {},
}

func requiredWorkerRules(repoRoot string, paths []string) []workerRule {
	required := make(map[workerRule]struct{})
	javaScriptChanged := false
	for _, raw := range paths {
		path := filepath.ToSlash(raw)
		isTest := isTestingRulePath(path)
		if isTest {
			required[ruleTesting] = struct{}{}
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".go":
			required[ruleGo] = struct{}{}
		case ".js", ".mjs", ".cjs", ".jsx":
			required[ruleJavaScript] = struct{}{}
			javaScriptChanged = true
		case ".php":
			required[rulePHP] = struct{}{}
		}
		if isTest {
			continue
		}
		if pathHasRuleToken(path, stateRuleTokens) {
			required[ruleStateTransitions] = struct{}{}
		}
		if pathHasRuleToken(path, cliRuleTokens) {
			required[ruleCLI] = struct{}{}
		}
	}
	if javaScriptChanged && repositoryUsesESLint(repoRoot) {
		required[ruleESLint] = struct{}{}
	}
	return orderedWorkerRules(required)
}

func isTestingRulePath(path string) bool {
	_, category := IsCriticalPath(path)
	switch category {
	case "test", testFixturePathCategory, testHarnessPathCategory:
		return true
	}
	lower := strings.ToLower(filepath.ToSlash(path))
	base := filepath.Base(lower)
	for _, suffix := range []string{
		"_test.go", ".test.js", ".spec.js", ".test.mjs", ".spec.mjs",
		".test.cjs", ".spec.cjs", ".test.jsx", ".spec.jsx", ".test.php", ".spec.php",
	} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	for _, segment := range strings.Split(lower, "/") {
		switch segment {
		case "test", "tests", "testdata", "__tests__", "spec", "specs":
			return true
		}
	}
	return false
}

func pathHasRuleToken(path string, tokens map[string]struct{}) bool {
	for _, segment := range strings.Split(strings.ToLower(filepath.ToSlash(path)), "/") {
		stem := strings.TrimSuffix(segment, filepath.Ext(segment))
		for _, token := range strings.FieldsFunc(stem, func(r rune) bool {
			return r == '_' || r == '-' || r == '.'
		}) {
			if _, ok := tokens[token]; ok {
				return true
			}
		}
	}
	return false
}

func repositoryUsesESLint(repoRoot string) bool {
	for _, name := range []string{
		"eslint.config.js", "eslint.config.mjs", "eslint.config.cjs",
		".eslintrc", ".eslintrc.js", ".eslintrc.cjs", ".eslintrc.json", ".eslintrc.yml", ".eslintrc.yaml",
	} {
		if _, err := os.Stat(filepath.Join(repoRoot, name)); err == nil {
			return true
		}
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, "package.json"))
	if err != nil {
		return false
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		return false
	}
	if _, ok := manifest["eslintConfig"]; ok {
		return true
	}
	for _, section := range []string{"dependencies", "devDependencies"} {
		deps, ok := manifest[section].(map[string]any)
		if ok {
			if _, exists := deps["eslint"]; exists {
				return true
			}
		}
	}
	return false
}

func orderedWorkerRules(set map[workerRule]struct{}) []workerRule {
	result := make([]workerRule, 0, len(set))
	for _, rule := range workerRuleOrder {
		if _, ok := set[rule]; ok {
			result = append(result, rule)
		}
	}
	return result
}

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

func (w *Workflow) workerRuleContextBlock(rules []workerRule) (string, error) {
	if len(rules) == 0 {
		return "", nil
	}
	var block strings.Builder
	block.WriteString("\n\n")
	block.WriteString(deterministicRuleMarker)
	block.WriteString("\n")
	block.WriteString(deterministicRuleFilesMarker)
	block.WriteString(" ")
	files := make([]string, 0, len(rules))
	for _, rule := range rules {
		files = append(files, workerRuleFiles[rule])
	}
	block.WriteString(strings.Join(files, ","))
	block.WriteString("\n")
	block.WriteString(renderInstructionConflictBoundary(defaultInstructionConflictBoundary()))
	block.WriteString("wrapperが実diffから決定論的に選択したcontractです。以下の本文を今回の作業・reviewへ適用してください。\n")
	for _, rule := range rules {
		fileName := workerRuleFiles[rule]
		path := filepath.Join(w.config.CodexConfigDir, "instructions", "worker", fileName)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("required worker ruleがありません: %s: %w", path, err)
		}
		block.WriteString("\n--- " + fileName + " ---\n")
		block.WriteString(strings.TrimRight(string(data), "\n"))
	}
	block.WriteString("\n")
	return block.String(), nil
}

func workerRuleForFile(fileName string) (workerRule, bool) {
	for rule, name := range workerRuleFiles {
		if name == fileName {
			return rule, true
		}
	}
	return "", false
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
	checkpoint.ActivatedRuleFiles = checkpoint.ActivatedRuleFiles[:0]
	for _, rule := range workerRuleOrder {
		if _, ok := activated[rule]; ok {
			checkpoint.ActivatedRuleFiles = append(checkpoint.ActivatedRuleFiles, workerRuleFiles[rule])
		}
	}
	if len(checkpoint.ActivatedRuleFiles) == 0 {
		checkpoint.ActivatedRuleFiles = nil
	}
}

func (w *Workflow) appendRuleContext(prompt string, rules []workerRule) (string, error) {
	if len(rules) == 0 {
		return prompt, nil
	}
	block, err := w.workerRuleContextBlock(rules)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(prompt, "\n") + block, nil
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
	checkpoint.Prompt, err = w.appendRuleContext(checkpoint.Prompt, missing)
	if err != nil {
		return checkpoint, nil, err
	}
	if checkpoint.OriginalPrompt != "" {
		checkpoint.OriginalPrompt, err = w.appendRuleContext(checkpoint.OriginalPrompt, missing)
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
	prompt, err = w.appendRuleContext(prompt, required)
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
	block, err := w.workerRuleContextBlock(rules)
	if err != nil {
		return state.ResumeCheckpoint{}, err
	}
	primaryAuthority := activeTaskPromptBlock(w.readActiveTaskState())
	prompt := fmt.Sprintf(`MODE: APPLY_DETERMINISTIC_RULES

ORIGINAL_USER_REQUEST:
%s

PREVIOUS_SOL_DECISION:
%s

%s%s
実diffに対してwrapperが必要contractの未適用を検出しました。
上記contract本文を現在のworking treeへ適用し、違反があれば修正してください。
タスク範囲を広げず、必要なtest/lint/buildと自己確認を行い、通常のworker結果を返してください。
`, parent.Request, parent.Decision, primaryAuthority, block)
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
