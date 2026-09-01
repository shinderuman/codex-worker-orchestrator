package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
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

type pocGoNoGoBoilerplate struct {
	decision       string
	options        string
	recommendation string
}

const externalFeasibilitySectionHeading = taskcontract.ExternalFeasibilityHeading

const (
	externalFeasibilityStatusNotApplicable  = taskcontract.StatusNotApplicable
	externalFeasibilityStatusPoC            = taskcontract.StatusPoC
	externalFeasibilityStatusObservation    = taskcontract.StatusObservation
	externalFeasibilityStatusImplementation = taskcontract.StatusImplementation
)

const (
	externalFeasibilityRejectMissing externalFeasibilityRejectKind = iota + 1
	externalFeasibilityRejectMalformed
	externalFeasibilityRejectUnverified
)

const pocGoNoGoObservationFloorBytes = 512

const pocGoNoGoFieldFloorBytes = 96

var externalFeasibilityGuardSurface = guardSurface{
	label:         "external feasibility宣言",
	files:         "ACTIVE task fileの`## External feasibility`節",
	eventSuffix:   "external-feasibility-check",
	outcomePrefix: "external_feasibility",
	invariants:    "ACTIVE task fileは`## External feasibility`節へstatus(not-applicable/poc/observation/implementation)を宣言し、implementationはevidence-source: producer・evidence・go(親Go判断)を伴う。poc/observationのworkerはread-only capabilityと開始前後snapshot同一性でproduction diffを禁止する。宣言内容の真偽は機械検証しない",
	targets:       "ACTIVE task fileの`## External feasibility`節と現在の宣言status",
}

var pocGoNoGoBoilerplateTiers = []pocGoNoGoBoilerplate{
	{
		decision:       "実producer観測結果のGo/No-Go。implementation昇格は親Codexがtask fileのExternal feasibility宣言をstatus: implementation + evidence-source: producer + evidence + go へ書き換えてから行う",
		options:        "Go: 親Codexが宣言をimplementationへmigrationして実装taskとして再委譲; No-Go: 撤退; 観測継続: 宣言をpoc/observationのまま再実行",
		recommendation: "PoC結果だけではGLM側でimplementationへ昇格しないため、親Solが実producer evidenceで判断する",
	},
	{
		decision:       "実producer観測結果のGo/No-Go。昇格は親が宣言をimplementation+producer evidence+goへ書き換えてから",
		options:        "Go: 親が宣言をimplementationへmigrationして再委譲; No-Go: 撤退; 観測継続: 宣言のまま再実行",
		recommendation: "GLM側では昇格せず親Solが実producer evidenceで判断",
	},
	{
		decision:       "親が実producer観測のGo/No-Goを判断; 昇格は宣言書換後に親が実施",
		options:        "Go: 宣言migration後に実装委譲; No-Go: 撤退; 観測継続: 再実行",
		recommendation: "親Solが実producer evidenceで判断",
	},
}

func (f externalFeasibility) pocStage() bool {
	return f.status == externalFeasibilityStatusPoC || f.status == externalFeasibilityStatusObservation
}

func (e *externalFeasibilityParseError) Error() string { return e.reason }

func parseExternalFeasibilityDeclaration(content []byte) (externalFeasibility, error) {
	declaration, err := taskcontract.ParseExternalFeasibility(content)
	if err != nil {
		var shared *taskcontract.FeasibilityError
		if !errors.As(err, &shared) {
			return externalFeasibility{}, err
		}
		return externalFeasibility{}, &externalFeasibilityParseError{
			kind:   externalFeasibilityRejectKind(shared.Kind),
			reason: shared.Reason,
		}
	}
	return externalFeasibility{
		status:         declaration.Status,
		assumption:     declaration.Assumption,
		evidenceSource: declaration.EvidenceSource,
		evidence:       declaration.Evidence,
		goDecision:     declaration.GoDecision,
	}, nil
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
	if err := w.state.SaveCurrentTaskAuthority(activeTaskPath, content); err != nil {
		return externalFeasibility{}, fmt.Errorf("ACTIVE task authority snapshotを保存できません: %w", err)
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
	return w.verifyEndSnapshot(snapshotEndCheck{
		stage:          state.SnapshotStagePoCEnd,
		loadStart:      w.state.LoadPoCStartSnapshot,
		failClosed:     w.failClosedPoCSnapshot,
		loadReason:     "PoC開始前snapshot読込失敗",
		captureReason:  "PoC終了後snapshot取得失敗",
		saveReason:     "snapshot comparison保存失敗",
		mismatchReason: "PoC/観測worker開始前から終了後までの間にrepository状態が変化しています(production diff禁止違反)",
	})
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
	boilerplate := pocGoNoGoBoilerplateTiers[0]
	result := packet.Result{
		Status:          packet.StatusNeedsSolDecision,
		Risk:            packet.RiskHigh,
		Summary:         boundedText("PoC/観測専用task(production diff無し)の結果を親Go/No-Goへ返します: "+workerResult.Summary, packet.MaxFieldBytes),
		Decision:        boilerplate.decision,
		Evidence:        boundedText("観測結果: "+strings.Join(observed, "; "), packet.MaxFieldBytes),
		Options:         boilerplate.options,
		Recommendation:  boilerplate.recommendation,
		TestObligations: boundedText(testObligations, packet.MaxFieldBytes),
		Targets:         targets,
		Artifacts:       workerResult.Artifacts,
	}
	return fitPoCResultPacketBudget(result)
}

func fitPoCResultPacketBudget(result packet.Result) packet.Result {
	result = applyPoCCompactions(result)
	if result.ByteSize() <= packet.MaxPacketBytes {
		return result
	}
	return summarizePoCPassthrough(result)
}

func applyPoCCompactions(result packet.Result) packet.Result {
	compactions := []func(*packet.Result) bool{
		func(result *packet.Result) bool { return capPoCField(&result.Evidence, pocGoNoGoObservationFloorBytes) },
		func(result *packet.Result) bool { return applyPoCBoilerplate(result, pocGoNoGoBoilerplateTiers[1]) },
		func(result *packet.Result) bool {
			return capPoCField(&result.TestObligations, pocGoNoGoObservationFloorBytes)
		},
		func(result *packet.Result) bool { return capPoCField(&result.Evidence, pocGoNoGoFieldFloorBytes) },
		func(result *packet.Result) bool {
			return capPoCField(&result.TestObligations, pocGoNoGoFieldFloorBytes)
		},
		func(result *packet.Result) bool { return applyPoCBoilerplate(result, pocGoNoGoBoilerplateTiers[2]) },
	}
	for over := result.ByteSize() - packet.MaxPacketBytes; over > 0; over = result.ByteSize() - packet.MaxPacketBytes {
		reduced := false
		for _, compact := range compactions {
			if compact(&result) {
				reduced = true
				break
			}
		}
		if !reduced {
			return result
		}
	}
	return result
}

func summarizePoCPassthrough(result packet.Result) packet.Result {
	baseEvidence := result.Evidence
	omittedTargets := 0
	omittedArtifacts := 0
	for {
		result.Evidence = attachPoCOmissionCounts(baseEvidence, omittedTargets, omittedArtifacts)
		if result.ByteSize() <= packet.MaxPacketBytes {
			return result
		}
		if len(result.Artifacts) > 0 {
			result.Artifacts = result.Artifacts[:len(result.Artifacts)-1]
			omittedArtifacts++
			continue
		}
		if len(result.Targets) > 1 {
			result.Targets = result.Targets[:len(result.Targets)-1]
			omittedTargets++
			continue
		}
		return result
	}
}

func attachPoCOmissionCounts(base string, omittedTargets int, omittedArtifacts int) string {
	parts := make([]string, 0, 2)
	if omittedTargets > 0 {
		parts = append(parts, fmt.Sprintf("targets省略%d件", omittedTargets))
	}
	if omittedArtifacts > 0 {
		parts = append(parts, fmt.Sprintf("artifacts省略%d件", omittedArtifacts))
	}
	if len(parts) == 0 {
		return base
	}
	return boundedText(base+"; "+strings.Join(parts, "/"), pocGoNoGoFieldFloorBytes)
}

func capPoCField(field *string, maxBytes int) bool {
	if len(*field) <= maxBytes {
		return false
	}
	*field = boundedText(*field, maxBytes)
	return true
}

func applyPoCBoilerplate(result *packet.Result, tier pocGoNoGoBoilerplate) bool {
	current := len(result.Decision) + len(result.Options) + len(result.Recommendation)
	shortened := len(tier.decision) + len(tier.options) + len(tier.recommendation)
	if shortened >= current {
		return false
	}
	result.Decision = tier.decision
	result.Options = tier.options
	result.Recommendation = tier.recommendation
	return true
}
