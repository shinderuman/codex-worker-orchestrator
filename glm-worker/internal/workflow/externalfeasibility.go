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

// externalFeasibilitySectionHeadingはACTIVE task file内の外部成立性宣言節の見出し。
// 未検証external runtime assumptionをimplementation前提へ進ませない機械gateの
// 唯一の入力であり、runtime(test含む)はこのparserだけを使い別parserを置かない。
const externalFeasibilitySectionHeading = "## External feasibility"

// 宣言statusの受理集合。unknown値はfail closedする。
const (
	externalFeasibilityStatusNotApplicable  = "not-applicable"
	externalFeasibilityStatusPoC            = "poc"
	externalFeasibilityStatusObservation    = "observation"
	externalFeasibilityStatusImplementation = "implementation"
)

// externalFeasibilityEvidenceProducerはimplementation statusのevidence-source受理値。
// 実producer由来以外(人工fixture・scripted packet・worker/reviewer PASS等)を
// implementation許可のevidenceとして受理しない。
const externalFeasibilityEvidenceProducer = "producer"

// externalFeasibilityFieldKeysは宣言節へ書けるkeyの全集合。status以外は必須時だけ使う。
var externalFeasibilityFieldKeys = []string{"status", "assumption", "evidence-source", "evidence", "go"}

// externalFeasibilityはACTIVE task fileの外部成立性宣言。statusは
// not-applicable(非該当)・poc/observation(PoC・観測段階でproduction実装不可)・
// implementation(実producer evidenceと親Go判断済み)の4値だけを取る。
type externalFeasibility struct {
	status         string
	assumption     string
	evidenceSource string
	evidence       string
	goDecision     string
}

// pocStageはPoC・観測段階の宣言か。production diffを残せない境界でworkerを実行する。
func (f externalFeasibility) pocStage() bool {
	return f.status == externalFeasibilityStatusPoC || f.status == externalFeasibilityStatusObservation
}

// externalFeasibilityRejectKindは宣言拒否理由のtelemetry分類。
type externalFeasibilityRejectKind int

const (
	externalFeasibilityRejectMissing externalFeasibilityRejectKind = iota + 1
	externalFeasibilityRejectMalformed
	externalFeasibilityRejectUnverified
)

// externalFeasibilityParseErrorは宣言の解析・検証失敗。kindでmissing・malformed・
// unverifiedを区別し、gateがoutcomeへ反映する。
type externalFeasibilityParseError struct {
	kind   externalFeasibilityRejectKind
	reason string
}

func (e *externalFeasibilityParseError) Error() string { return e.reason }

// leadingBackticksは行頭のbacktick連続数を返す。fence境界の判定だけに使う。
func leadingBackticks(line string) int {
	count := 0
	for count < len(line) && line[count] == '`' {
		count++
	}
	return count
}

// parseExternalFeasibilityDeclarationはtask file本文から`## External feasibility`節を
// 解析する。Original instruction等のfenced code block内の同見出しは文書構造ではないため、
// 3backtick以上の行で開き同数以上のbacktickで閉じるfence境界の外だけを節構造として数える。
// 節の無いtask・複数節・key: value形式以外の行・未知key・重複key・空value・
// status別の必須field欠落・evidence-source非producerは全てerror(fail closed)を返す。
// 宣言内容の真偽とsemantic適用判断は機械検証せず親Codexの責務に残す。
func parseExternalFeasibilityDeclaration(content []byte) (externalFeasibility, error) {
	var decl externalFeasibility
	lines := strings.Split(string(content), "\n")
	headingAt := -1
	sections := 0
	fence := 0
	for i, line := range lines {
		backticks := leadingBackticks(line)
		if fence > 0 {
			if backticks >= fence {
				fence = 0
			}
			continue
		}
		if backticks >= 3 {
			fence = backticks
			continue
		}
		if strings.HasPrefix(line, "## ") && strings.TrimSpace(line) == externalFeasibilitySectionHeading {
			sections++
			if headingAt < 0 {
				headingAt = i
			}
		}
	}
	if sections == 0 {
		return decl, &externalFeasibilityParseError{
			kind:   externalFeasibilityRejectMissing,
			reason: "External feasibility節(" + externalFeasibilitySectionHeading + ")がありません",
		}
	}
	if sections > 1 {
		return decl, &externalFeasibilityParseError{
			kind:   externalFeasibilityRejectMalformed,
			reason: fmt.Sprintf("External feasibility節が複数あります(%d節)", sections),
		}
	}
	values := map[string]string{}
	fence = 0
	for i := headingAt + 1; i < len(lines); i++ {
		line := lines[i]
		backticks := leadingBackticks(line)
		if fence > 0 {
			if backticks >= fence {
				fence = 0
			}
			continue
		}
		if backticks >= 3 {
			fence = backticks
			continue
		}
		if strings.HasPrefix(line, "## ") {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || !externalFeasibilityKnownKey(key) {
			return decl, &externalFeasibilityParseError{
				kind:   externalFeasibilityRejectMalformed,
				reason: fmt.Sprintf("External feasibility節の%d行目 %qがkey: value形式ではありません(使えるkey: %s)", i+1, trimmed, strings.Join(externalFeasibilityFieldKeys, ", ")),
			}
		}
		if _, dup := values[key]; dup {
			return decl, &externalFeasibilityParseError{
				kind:   externalFeasibilityRejectMalformed,
				reason: "External feasibility節のkey \"" + key + "\"が重複しています",
			}
		}
		if value == "" {
			return decl, &externalFeasibilityParseError{
				kind:   externalFeasibilityRejectMalformed,
				reason: "External feasibility節のkey \"" + key + "\"のvalueが空です",
			}
		}
		values[key] = value
	}
	return validateExternalFeasibilityFields(values)
}

func externalFeasibilityKnownKey(key string) bool {
	for _, known := range externalFeasibilityFieldKeys {
		if key == known {
			return true
		}
	}
	return false
}

// validateExternalFeasibilityFieldsはstatus別の必須・禁止fieldを検証する。
// poc/observationはstatus+assumptionだけ、implementationは親Go判断と実producer evidenceの
// fieldを必須とし、not-applicableはstatus以外を書かせない。
func validateExternalFeasibilityFields(values map[string]string) (externalFeasibility, error) {
	var decl externalFeasibility
	status := values["status"]
	switch status {
	case "":
		return decl, &externalFeasibilityParseError{
			kind:   externalFeasibilityRejectMalformed,
			reason: "External feasibility節にstatusがありません(not-applicable/poc/observation/implementation)",
		}
	case externalFeasibilityStatusNotApplicable:
		for _, key := range externalFeasibilityFieldKeys[1:] {
			if values[key] != "" {
				return decl, &externalFeasibilityParseError{
					kind:   externalFeasibilityRejectMalformed,
					reason: "not-applicableではstatus以外のkey(" + key + ")を書けません。外部前提がある場合はpoc/observation/implementationを宣言してください",
				}
			}
		}
	case externalFeasibilityStatusPoC, externalFeasibilityStatusObservation:
		if values["assumption"] == "" {
			return decl, &externalFeasibilityParseError{
				kind:   externalFeasibilityRejectMalformed,
				reason: status + "では未検証外部前提を表すassumptionが必須です",
			}
		}
		for _, key := range []string{"evidence-source", "evidence", "go"} {
			if values[key] != "" {
				return decl, &externalFeasibilityParseError{
					kind:   externalFeasibilityRejectMalformed,
					reason: status + "では" + key + "を書けません。implementation昇格は親Codexが宣言全体を書き換えて行います",
				}
			}
		}
	case externalFeasibilityStatusImplementation:
		if values["assumption"] == "" {
			return decl, &externalFeasibilityParseError{
				kind:   externalFeasibilityRejectMalformed,
				reason: "implementationでは前提とした外部成立性のassumptionが必須です",
			}
		}
		if values["evidence-source"] == "" || values["evidence"] == "" || values["go"] == "" {
			return decl, &externalFeasibilityParseError{
				kind:   externalFeasibilityRejectUnverified,
				reason: "implementationはevidence-source・evidence・go(親Go判断)の全てが必須です。PoC結果をGLMだけでGoへ昇格させない",
			}
		}
		if values["evidence-source"] != externalFeasibilityEvidenceProducer {
			return decl, &externalFeasibilityParseError{
				kind:   externalFeasibilityRejectUnverified,
				reason: "implementationのevidence-sourceは実producer由来の \"" + externalFeasibilityEvidenceProducer + "\" だけです(人工fixture・scripted packet・worker/reviewer PASSは不可)",
			}
		}
	default:
		return decl, &externalFeasibilityParseError{
			kind:   externalFeasibilityRejectMalformed,
			reason: fmt.Sprintf("External feasibilityのstatus %qは未知です(not-applicable/poc/observation/implementation)", status),
		}
	}
	return externalFeasibility{
		status:         status,
		assumption:     values["assumption"],
		evidenceSource: values["evidence-source"],
		evidence:       values["evidence"],
		goDecision:     values["go"],
	}, nil
}

// externalFeasibilityGuardSurfaceは外部成立性宣言gateの設定。親管理metadata guardと同じ
// 停止semanticsへ載せ、outcome接頭辞だけ分離して集計する。
var externalFeasibilityGuardSurface = guardSurface{
	label:         "external feasibility宣言",
	files:         "ACTIVE task fileの`## External feasibility`節",
	eventSuffix:   "external-feasibility-check",
	outcomePrefix: "external_feasibility",
	invariants:    "ACTIVE task fileは`## External feasibility`節へstatus(not-applicable/poc/observation/implementation)を宣言し、implementationはevidence-source: producer・evidence・go(親Go判断)を伴う。poc/observationのworkerはread-only capabilityと開始前後snapshot同一性でproduction diffを禁止する。宣言内容の真偽は機械検証しない",
	targets:       "ACTIVE task fileの`## External feasibility`節と現在の宣言status",
}

func (s guardSurface) unverifiedOutcome() string { return s.outcomePrefix + "_unverified" }

// gateExternalFeasibilityは現在taskのACTIVE task fileから宣言を解析し、受理できない
// 宣言をmodel呼出前にfail closedする。planの無いrepository(配線なし)は何も強制しない。
// 全worker/reviewer dispatch entrypointがこの同じ受理集合を通る。keepTaskStatusは
// --decision・--resumeのように拒否時に現在のtask status(waiting-decisionや停止理由)を
// 保持すべき呼出でtrueにする。resume checkpoint・session・pending decisionは常にもつ
// ため、親Codexが宣言を修復すれば同じentrypointを再実行できる。
func (w *Workflow) gateExternalFeasibility(phase string, keepTaskStatus bool) (externalFeasibility, error) {
	activeTaskPath := w.readActiveTaskState()
	if activeTaskPath == "" {
		return externalFeasibility{}, nil
	}
	// 固定済みtask fileの欠損・差し替えは宣言gateではなく既存の親管理metadata guardが
	// 担当する。ここで停止すると欠損理由のoutcomeが二系統に分かれるため、読まずに下流の
	// 実在確認へ任せる。
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

// failClosedExternalFeasibilityは宣言gate失敗の停止semantics。resume checkpoint・
// session・pending decisionは消さず保持し、moveToWaitingSolReview=trueの呼出
// (new task・fix・reviewer・auto-fix)だけtask statusをWaitingSolReviewへ移す。
// --decision・--resumeはstatusも変えず、親Codexが宣言を修復すれば同じentrypointを
// そのまま再実行できる。
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

// externalFeasibilityFailClosedResultは宣言gateのfail closed packet。worker/reviewer
// model呼出0回で止まったこと・停止済みstateの保持・親Codexの回復操作(宣言の追加・
// migration・Go判断)を明示する。
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

// savePoCStartSnapshotはPoC/観測taskのworker開始直前のHEAD/index/worktreeを基準として
// 保存する。基準を確保できないときはworkerを実行せずfail closedする。resumeはこの保存済み
// snapshotを基準に再利用し、再撮影して停止期間中の変化を隠さない。
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

// gatePoCResumeSnapshotはPoC worker resumeを実行前に基準snapshotの存在だけを確認する。
// 基準が無ければprobeもworker呼出も1件も行わずfail closedする。
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

// verifyPoCEndSnapshotはPoC/観測worker終了後、開始直前の保存snapshotへ現在状態を再照合する。
// 通常reviewへ進める前に強制し、1軸でも変化すればfail closedする。rate-limit・provider障害の
// resume後も同じ基準を使うため、停止期間中の変化も検出から逃れない。
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

// failClosedPoCSnapshotはPoC前後同一性確認失敗をreport-onlyと同じ停止semanticsへ載せる。
// 検出主体はworkerの前後invariantのためevent roleはWorkerRoleのままにする。
func (w *Workflow) failClosedPoCSnapshot(stage state.SnapshotStage, start, current state.GitSnapshot, reason string, cause error) error {
	w.recordSnapshotEvent(state.WorkerRole, stage, start, current, reason, cause)
	return w.failClosedStopped(stage, reason, cause, poCSnapshotFailClosedResult)
}

// poCSnapshotFailClosedResultはPoC/観測taskの不変性確認失敗時のSol確認結果。
// production diff禁止の機械強制境界であることをSolへ区別可能にする。
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

// routePoCWorkerResultはPoC/観測taskのIMPLEMENTED結果を親Go/No-Go待ちへ変換する。
// reviewer PASSで完了させず、implementation昇格を親Codexの宣言書き換えに限定する。
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

// pocGoNoGoResultはPoC/観測結果をNEEDS_SOL_DECISIONへ包む。workerの観測報告
// (summary/tests/unverified/artifacts)をevidence欄へそのまま載せ、昇格手段を
// 宣言migrationだけに限定したdecision fieldsを機械的に固定する。
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
