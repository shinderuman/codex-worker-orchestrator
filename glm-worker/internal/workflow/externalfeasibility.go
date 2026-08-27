package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type externalFeasibility struct {
	status         string
	assumption     string
	evidenceSource string
	evidence       string
	goDecision     string
}

type externalFeasibilityRejectKind int

type externalFeasibilityParseError struct {
	kind   externalFeasibilityRejectKind
	reason string
}

const externalFeasibilitySectionHeading = "## External feasibility"

const (
	externalFeasibilityStatusNotApplicable  = "not-applicable"
	externalFeasibilityStatusPoC            = "poc"
	externalFeasibilityStatusObservation    = "observation"
	externalFeasibilityStatusImplementation = "implementation"
)

const externalFeasibilityEvidenceProducer = "producer"

const (
	externalFeasibilityRejectMissing externalFeasibilityRejectKind = iota + 1
	externalFeasibilityRejectMalformed
	externalFeasibilityRejectUnverified
)

var externalFeasibilityFieldKeys = []string{"status", "assumption", "evidence-source", "evidence", "go"}

var externalFeasibilityGuardSurface = guardSurface{
	label:         "external feasibility宣言",
	files:         "ACTIVE task fileの`## External feasibility`節",
	eventSuffix:   "external-feasibility-check",
	outcomePrefix: "external_feasibility",
	invariants:    "ACTIVE task fileは`## External feasibility`節へstatus(not-applicable/poc/observation/implementation)を宣言し、implementationはevidence-source: producer・evidence・go(親Go判断)を伴う。poc/observationのworkerはread-only capabilityと開始前後snapshot同一性でproduction diffを禁止する。宣言内容の真偽は機械検証しない",
	targets:       "ACTIVE task fileの`## External feasibility`節と現在の宣言status",
}

func (f externalFeasibility) pocStage() bool {
	return f.status == externalFeasibilityStatusPoC || f.status == externalFeasibilityStatusObservation
}

func (e *externalFeasibilityParseError) Error() string { return e.reason }

func leadingBackticks(line string) int {
	count := 0
	for count < len(line) && line[count] == '`' {
		count++
	}
	return count
}

func parseExternalFeasibilityDeclaration(content []byte) (externalFeasibility, error) {
	var decl externalFeasibility
	lines := strings.Split(string(content), "\n")
	headingAt, err := findExternalFeasibilitySection(lines)
	if err != nil {
		return decl, err
	}
	values, err := parseExternalFeasibilityValues(lines, headingAt)
	if err != nil {
		return decl, err
	}
	return validateExternalFeasibilityFields(values)
}

func findExternalFeasibilitySection(lines []string) (int, error) {
	headingAt := -1
	sections := 0
	fence := 0
	for i, line := range lines {
		if !externalFeasibilityLineOutsideFence(line, &fence) {
			continue
		}
		if strings.HasPrefix(line, "## ") && strings.TrimSpace(line) == externalFeasibilitySectionHeading {
			sections++
			if headingAt < 0 {
				headingAt = i
			}
		}
	}
	switch {
	case sections == 0:
		return -1, &externalFeasibilityParseError{
			kind:   externalFeasibilityRejectMissing,
			reason: "External feasibility節(" + externalFeasibilitySectionHeading + ")がありません",
		}
	case sections > 1:
		return -1, &externalFeasibilityParseError{
			kind:   externalFeasibilityRejectMalformed,
			reason: fmt.Sprintf("External feasibility節が複数あります(%d節)", sections),
		}
	default:
		return headingAt, nil
	}
}

func externalFeasibilityLineOutsideFence(line string, fence *int) bool {
	backticks := leadingBackticks(line)
	if *fence > 0 {
		if backticks >= *fence {
			*fence = 0
		}
		return false
	}
	if backticks >= 3 {
		*fence = backticks
		return false
	}
	return true
}

func parseExternalFeasibilityValues(lines []string, headingAt int) (map[string]string, error) {
	values := map[string]string{}
	fence := 0
	for i := headingAt + 1; i < len(lines); i++ {
		line := lines[i]
		if !externalFeasibilityLineOutsideFence(line, &fence) {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if err := addExternalFeasibilityValue(values, trimmed, i+1); err != nil {
			return nil, err
		}
	}
	return values, nil
}

func addExternalFeasibilityValue(values map[string]string, line string, lineNumber int) error {
	key, value, ok := strings.Cut(line, ":")
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if !ok || !externalFeasibilityKnownKey(key) {
		return &externalFeasibilityParseError{
			kind:   externalFeasibilityRejectMalformed,
			reason: fmt.Sprintf("External feasibility節の%d行目 %qがkey: value形式ではありません(使えるkey: %s)", lineNumber, line, strings.Join(externalFeasibilityFieldKeys, ", ")),
		}
	}
	if _, dup := values[key]; dup {
		return &externalFeasibilityParseError{
			kind:   externalFeasibilityRejectMalformed,
			reason: "External feasibility節のkey \"" + key + "\"が重複しています",
		}
	}
	if value == "" {
		return &externalFeasibilityParseError{
			kind:   externalFeasibilityRejectMalformed,
			reason: "External feasibility節のkey \"" + key + "\"のvalueが空です",
		}
	}
	values[key] = value
	return nil
}

func externalFeasibilityKnownKey(key string) bool {
	for _, known := range externalFeasibilityFieldKeys {
		if key == known {
			return true
		}
	}
	return false
}

func validateExternalFeasibilityFields(values map[string]string) (externalFeasibility, error) {
	var decl externalFeasibility
	status := values["status"]
	if err := validateExternalFeasibilityStatus(status, values); err != nil {
		return decl, err
	}
	return externalFeasibility{
		status:         status,
		assumption:     values["assumption"],
		evidenceSource: values["evidence-source"],
		evidence:       values["evidence"],
		goDecision:     values["go"],
	}, nil
}

func validateExternalFeasibilityStatus(status string, values map[string]string) error {
	switch status {
	case "":
		return externalFeasibilityMalformed("External feasibility節にstatusがありません(not-applicable/poc/observation/implementation)")
	case externalFeasibilityStatusNotApplicable:
		return validateExternalFeasibilityNotApplicable(values)
	case externalFeasibilityStatusPoC, externalFeasibilityStatusObservation:
		return validateExternalFeasibilityExploration(status, values)
	case externalFeasibilityStatusImplementation:
		return validateExternalFeasibilityImplementation(values)
	default:
		return externalFeasibilityMalformed(fmt.Sprintf("External feasibilityのstatus %qは未知です(not-applicable/poc/observation/implementation)", status))
	}
}

func validateExternalFeasibilityNotApplicable(values map[string]string) error {
	for _, key := range externalFeasibilityFieldKeys[1:] {
		if values[key] != "" {
			return externalFeasibilityMalformed("not-applicableではstatus以外のkey(" + key + ")を書けません。外部前提がある場合はpoc/observation/implementationを宣言してください")
		}
	}
	return nil
}

func validateExternalFeasibilityExploration(status string, values map[string]string) error {
	if values["assumption"] == "" {
		return externalFeasibilityMalformed(status + "では未検証外部前提を表すassumptionが必須です")
	}
	for _, key := range []string{"evidence-source", "evidence", "go"} {
		if values[key] != "" {
			return externalFeasibilityMalformed(status + "では" + key + "を書けません。implementation昇格は親Codexが宣言全体を書き換えて行います")
		}
	}
	return nil
}

func validateExternalFeasibilityImplementation(values map[string]string) error {
	if values["assumption"] == "" {
		return externalFeasibilityMalformed("implementationでは前提とした外部成立性のassumptionが必須です")
	}
	if values["evidence-source"] == "" || values["evidence"] == "" || values["go"] == "" {
		return externalFeasibilityUnverified("implementationはevidence-source・evidence・go(親Go判断)の全てが必須です。PoC結果をGLMだけでGoへ昇格させない")
	}
	if values["evidence-source"] != externalFeasibilityEvidenceProducer {
		return externalFeasibilityUnverified("implementationのevidence-sourceは実producer由来の \"" + externalFeasibilityEvidenceProducer + "\" だけです(人工fixture・scripted packet・worker/reviewer PASSは不可)")
	}
	return nil
}

func externalFeasibilityMalformed(reason string) error {
	return &externalFeasibilityParseError{kind: externalFeasibilityRejectMalformed, reason: reason}
}

func externalFeasibilityUnverified(reason string) error {
	return &externalFeasibilityParseError{kind: externalFeasibilityRejectUnverified, reason: reason}
}

func (s guardSurface) unverifiedOutcome() string { return s.outcomePrefix + "_unverified" }

func (w *Workflow) gateExternalFeasibility(phase string, keepTaskStatus bool) (externalFeasibility, error) {
	activeTaskPath := w.readActiveTaskState()
	if activeTaskPath == "" {
		return externalFeasibility{}, nil
	}

	if !activeTaskFileExists(w.config.RepoRoot, activeTaskPath) {
		return externalFeasibility{}, nil
	}
	content, err := os.ReadFile(filepath.Join(w.config.RepoRoot, filepath.FromSlash(activeTaskPath)))
	if err != nil {
		return externalFeasibility{}, w.failClosedExternalFeasibility(phase, externalFeasibilityGuardSurface.unavailableOutcome(), "ACTIVE task file "+activeTaskPath+"のExternal feasibility宣言を読めません", err, !keepTaskStatus)
	}
	decl, err := parseExternalFeasibilityDeclaration(content)
	if err == nil {
		return decl, nil
	}
	var reject *externalFeasibilityParseError
	outcome := externalFeasibilityGuardSurface.malformedOutcome()
	if errors.As(err, &reject) {
		switch reject.kind {
		case externalFeasibilityRejectMissing:
			outcome = externalFeasibilityGuardSurface.missingOutcome()
		case externalFeasibilityRejectUnverified:
			outcome = externalFeasibilityGuardSurface.unverifiedOutcome()
		}
	}
	return externalFeasibility{}, w.failClosedExternalFeasibility(phase, outcome, err.Error(), nil, !keepTaskStatus)
}

func (w *Workflow) failClosedExternalFeasibility(phase string, outcome string, reason string, cause error, moveToWaitingSolReview bool) error {
	w.recordParentFileEvent(phase, externalFeasibilityGuardSurface, outcome, reason, cause)
	if moveToWaitingSolReview {
		if err := w.state.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
			return err
		}
	}
	if cause != nil {
		reason = fmt.Sprintf("%s: %v", reason, cause)
	}
	if err := w.emitResult(externalFeasibilityFailClosedResult(phase, reason)); err != nil {
		return err
	}
	return errParentFileGuardStopped
}

func externalFeasibilityFailClosedResult(phase string, reason string) packet.Result {
	return packet.Result{
		Status:              packet.StatusNeedsSolReview,
		Risk:                packet.RiskHigh,
		Summary:             fmt.Sprintf("ACTIVE task fileのexternal feasibility宣言を受理できないためworker/reviewer model呼出0回でfail closedしました(呼出: %s)。停止済みresume checkpointとpending decisionは保持しています", phase),
		RequirementCoverage: "未検証external feasibilityをimplementation前提へ進ませない機械gateで停止したため要求の実施は未着手",
		Invariants:          externalFeasibilityGuardSurface.invariants,
		TestEvidence:        "宣言parserと全dispatch entrypoint(new/decision/fix/reviewer/auto-fix/resume)の0 model call fail closed検証、拒否後のstate保持と同一entrypoint再実行検証、poc/observationのread-only・snapshot同一性検証",
		Issues:              reason,
		ResidualRisk:        "not-applicable等の宣言内容の真偽とsemantic適用判断は親Codexの責務であり機械検証しない",
		Targets:             []string{"ACTIVE task fileの## External feasibility節(宣言の追加・status修正・evidence/go記載)"},
		SolQuestion:         "親Codexが宣言を追加・修正する(not-applicable/poc/observation)、または実producer evidenceと親Go判断をgo: へ記載してimplementationとして再委譲する。現在のtask status・resume checkpoint・pending decisionは保持されるため、修復後に同じentrypoint(new/decision/fix/resume)を再実行する",
	}
}

func (w *Workflow) savePoCStartSnapshot() (bool, error) {
	start, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		return true, w.failClosedPoCSnapshot(state.SnapshotStagePoCStart, start, state.GitSnapshot{}, "PoC開始前snapshot取得失敗", err)
	}
	if err := w.state.SavePoCStartSnapshot(start); err != nil {
		return true, w.failClosedPoCSnapshot(state.SnapshotStagePoCStart, start, state.GitSnapshot{}, "PoC開始前snapshot保存失敗", err)
	}
	return false, nil
}

func (w *Workflow) gatePoCResumeSnapshot() (bool, error) {
	if _, err := w.state.LoadPoCStartSnapshot(); err != nil {
		return true, w.failClosedPoCSnapshot(
			state.SnapshotStagePoCStart,
			state.GitSnapshot{},
			state.GitSnapshot{},
			"resume再開前にPoC開始前snapshotが欠損しているため不変性の基準を確認できません",
			err,
		)
	}
	return false, nil
}

func (w *Workflow) verifyPoCEndSnapshot() (bool, error) {
	start, err := w.state.LoadPoCStartSnapshot()
	if err != nil {
		return true, w.failClosedPoCSnapshot(state.SnapshotStagePoCEnd, state.GitSnapshot{}, state.GitSnapshot{}, "PoC開始前snapshot読込失敗", err)
	}
	current, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		return true, w.failClosedPoCSnapshot(state.SnapshotStagePoCEnd, start, state.GitSnapshot{}, "PoC終了後snapshot取得失敗", err)
	}
	comparison := state.CompareGitSnapshot(start, current, state.SnapshotStagePoCEnd, "")
	if err := w.state.SaveSnapshotComparison(comparison); err != nil {
		return true, w.failClosedPoCSnapshot(state.SnapshotStagePoCEnd, start, current, "snapshot comparison保存失敗", err)
	}
	if !comparison.Matched {
		return true, w.failClosedPoCSnapshot(state.SnapshotStagePoCEnd, start, current, "PoC/観測worker開始前から終了後までの間にrepository状態が変化しています(production diff禁止違反)", nil)
	}
	return false, nil
}

func (w *Workflow) failClosedPoCSnapshot(stage state.SnapshotStage, start, current state.GitSnapshot, reason string, cause error) error {
	w.recordSnapshotEvent(state.WorkerRole, stage, start, current, reason, cause)
	return w.failClosedStopped(stage, reason, cause, poCSnapshotFailClosedResult)
}

func poCSnapshotFailClosedResult(stage state.SnapshotStage, reason string) packet.Result {
	return packet.Result{
		Status:              packet.StatusNeedsSolReview,
		Risk:                packet.RiskHigh,
		Summary:             fmt.Sprintf("PoC/観測専用taskの開始前後でHEAD/index/worktree同一性を確認できず(%s)、通常reviewへ進めずSol確認へ昇格", stage),
		RequirementCoverage: "PoC/観測taskのproduction diff無しpostconditionを機械強制できなかったためSolが直接確認する必要あり",
		Invariants:          "wrapperはpoc/observation宣言taskのworkerをread-only capabilityで実行し、開始前snapshotと終了後状態の3軸一致を確認するまで通常reviewへ進まない",
		TestEvidence:        "開始前保存snapshotと終了後snapshotの比較で不一致または取得失敗を検出",
		Issues:              reason,
		ResidualRisk:        "PoC/観測workerがrepositoryを変更した可能性とその意図を排除できなかった。変更内容はbaselineへ巻き戻さず現物のまま残している",
		Targets:             []string{"repository HEAD/index/worktreeの現在状態とPoC開始前snapshot・telemetry記録"},
		SolQuestion:         "PoC/観測workerによる変更の意図有無と追跡・修正方針をSolが判断する",
	}
}

func (w *Workflow) routePoCWorkerResult(workerResult packet.Result) error {
	result := pocGoNoGoResult(workerResult)
	if err := packet.ValidateWorkerResult(result); err != nil {
		return err
	}
	if err := w.state.Touch("pending-decision"); err != nil {
		return err
	}
	if err := w.state.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		return err
	}
	return w.emitResult(result)
}

func pocGoNoGoResult(workerResult packet.Result) packet.Result {
	targets := workerResult.Targets
	if len(targets) == 0 {
		targets = []string{"none"}
	}
	observed := make([]string, 0, 3)
	for _, part := range []string{workerResult.Summary, workerResult.Tests, workerResult.Unverified} {
		if part != "" {
			observed = append(observed, part)
		}
	}
	testObligations := workerResult.Tests
	if testObligations == "" {
		testObligations = "Go判断は実producer由来の観測evidenceを前提とする"
	}
	return packet.Result{
		Status:          packet.StatusNeedsSolDecision,
		Risk:            packet.RiskHigh,
		Summary:         boundedText("PoC/観測専用task(production diff無し)の結果を親Go/No-Goへ返します: "+workerResult.Summary, packet.MaxFieldBytes),
		Decision:        "実producer観測結果のGo/No-Go。implementation昇格は親Codexがtask fileのExternal feasibility宣言をstatus: implementation + evidence-source: producer + evidence + go へ書き換えてから行う",
		Evidence:        boundedText("観測結果: "+strings.Join(observed, "; "), packet.MaxFieldBytes),
		Options:         "Go: 親Codexが宣言をimplementationへmigrationして実装taskとして再委譲; No-Go: 撤退; 観測継続: 宣言をpoc/observationのまま再実行",
		Recommendation:  "PoC結果だけではGLM側でimplementationへ昇格しないため、親Solが実producer evidenceで判断する",
		TestObligations: boundedText(testObligations, packet.MaxFieldBytes),
		Targets:         targets,
		Artifacts:       workerResult.Artifacts,
	}
}
