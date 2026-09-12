package workflow

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
)

var repositoryHarnessGuardSurface = guardSurface{
	label:         "repository harness opt-in marker",
	files:         repositoryharness.MarkerPath,
	eventSuffix:   "repository-harness-check",
	outcomePrefix: "repository_harness",
	invariants:    "repository rootのtracked marker file " + repositoryharness.MarkerPath + " は当repository harness protocol(Plan/Task/protected metadata・self-protection risk floor・quality policy)の明示opt-in境界であり、marker不在のforeign repositoryへ名前だけの暗黙適用を行わない。task開始時にactivation済みまたはACTIVE taskを固定したtaskは、marker欠損・内容不一致・非regular file・git追跡喪失でgeneric modeへ黙って縮退しない",
	targets:       repositoryharness.MarkerPath + "のworking tree存在・種別・内容・git追跡状態",
}

func (w *Workflow) repositoryHarnessActive() (bool, error) {
	if w.state.Exists(repositoryharness.ActivationStateKey) {
		activation, err := w.state.Read(repositoryharness.ActivationStateKey)
		if err != nil {
			return false, fmt.Errorf("repository harness activation pinを読み込めません: %w", err)
		}
		switch activation {
		case repositoryharness.ActivationActiveValue:
			return true, nil
		case repositoryharness.ActivationInactiveValue:
			if !w.activeTaskStateSet() {
				return false, nil
			}
			activeTask, err := w.state.Read(activeTaskStateKey)
			if err != nil {
				return false, fmt.Errorf("ACTIVE task pinを読み込めません: %w", err)
			}
			if activeTask != "" {
				return false, fmt.Errorf("repository harness activation pinがinactiveですがACTIVE task %sが固定されています", activeTask)
			}
			return false, nil
		default:
			return false, fmt.Errorf("repository harness activation pinが不正です: %q", activation)
		}
	}
	if w.activeTaskStateSet() {
		return false, fmt.Errorf("repository harness activation pinが欠落しています")
	}
	decision, err := repositoryharness.Evaluate(w.config.RepoRoot)
	if err != nil {
		return false, err
	}
	return decision.Active, nil
}

func (w *Workflow) captureRepositoryHarnessBoundary() (repositoryharness.MarkerGuard, bool, bool, error) {
	harnessActive, err := w.repositoryHarnessActive()
	if err != nil {
		return repositoryharness.MarkerGuard{}, false, true, w.failClosedRepositoryHarness("repository-harness-capture", repositoryHarnessGuardSurface.unavailableOutcome(), "repository harness適用境界を評価できません", err)
	}
	if !harnessActive {
		return repositoryharness.MarkerGuard{}, false, false, nil
	}
	marker, stopped, err := w.captureRepositoryHarnessMarkerGuard()
	return marker, true, stopped, err
}

func (w *Workflow) pinRepositoryHarnessActivation() (bool, error) {
	decision, err := repositoryharness.Evaluate(w.config.RepoRoot)
	if err != nil {
		return false, err
	}
	value := repositoryharness.ActivationInactiveValue
	if decision.Active {
		value = repositoryharness.ActivationActiveValue
	}
	if err := w.state.Write(repositoryharness.ActivationStateKey, value); err != nil {
		return false, err
	}
	return decision.Active, nil
}

func repositoryHarnessOutcome(reason string) string {
	switch reason {
	case repositoryharness.ReasonAbsent:
		return repositoryHarnessGuardSurface.missingOutcome()
	case repositoryharness.ReasonNotRegularFile:
		return repositoryHarnessGuardSurface.malformedOutcome()
	case repositoryharness.ReasonContentMismatch:
		return repositoryHarnessGuardSurface.mismatchOutcome()
	case repositoryharness.ReasonUntracked:
		return repositoryHarnessGuardSurface.outcomePrefix + "_untracked"
	default:
		return repositoryHarnessGuardSurface.unavailableOutcome()
	}
}

func repositoryHarnessInactiveReason(reason string) string {
	switch reason {
	case repositoryharness.ReasonAbsent:
		return "opt-in marker " + repositoryharness.MarkerPath + " がrepository rootへ存在しません"
	case repositoryharness.ReasonNotRegularFile:
		return "opt-in marker " + repositoryharness.MarkerPath + " がregular fileではありません"
	case repositoryharness.ReasonContentMismatch:
		return "opt-in marker " + repositoryharness.MarkerPath + " の内容がprotocol正本と一致しません"
	case repositoryharness.ReasonUntracked:
		return "opt-in marker " + repositoryharness.MarkerPath + " がgit indexで追跡されていません"
	default:
		return "opt-in marker " + repositoryharness.MarkerPath + " の境界判定理由を特定できません(" + reason + ")"
	}
}

func (w *Workflow) failClosedRepositoryHarness(phase string, outcome string, reason string, cause error) error {
	w.recordParentFileEvent(phase, repositoryHarnessGuardSurface, outcome, reason, cause)
	if err := w.state.DiscardResumeAndWaitForSolReview(); err != nil {
		return err
	}
	if cause != nil {
		reason = fmt.Sprintf("%s: %v", reason, cause)
	}
	if err := w.emitResult(repositoryHarnessFailClosedResult(phase, reason)); err != nil {
		return err
	}
	return errParentFileGuardStopped
}

func repositoryHarnessFailClosedResult(phase string, reason string) packet.Result {
	return packet.Result{
		Status:              packet.StatusNeedsSolReview,
		Risk:                packet.RiskHigh,
		Summary:             fmt.Sprintf("repository harness opt-in marker(%s)の境界を確認できず、reviewerを呼ばずSol確認へ昇格(%s)", repositoryharness.MarkerPath, phase),
		RequirementCoverage: "当repository harness protocolの適用可否を機械確定できなかったため、親Codex/Solがmarker状態とtask継続可否を直接確認する必要あり",
		Invariants:          repositoryHarnessGuardSurface.invariants,
		TestEvidence:        "呼出前後のmarker存在・種別・内容・git追跡検査で、欠損・不一致・非追跡・種別変化を検出した",
		Issues:              reason,
		ResidualRisk:        "markerの現在状態はorchestratorが復元せずそのまま残っている",
		Targets:             []string{repositoryHarnessGuardSurface.targets},
		SolQuestion:         "markerの再配置・内容修復・git追跡回復と、同一taskの再開または新規task化をSolが判断する",
	}
}
