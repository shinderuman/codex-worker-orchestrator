package workflow

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// verifyInterruptedRetentionはUserInterrupted resumeがmodel呼出へ戻る前に、停止保存時に
// 固定した元checkout保持基準へ現在状態を照合する。割り込みtask隔離実行中に元taskの
// dirty/untracked作業・stateが保持されたままか、HEAD移動は承認済み経路(親管理metadata更新・
// --isolate隔離branchの統合)に限られるかを機械検証し、範囲外の変化はstateを一切変更せず
// fail closedで呼出元へ返す。
//
// 承認する停止期間中の変化:
//   - 非親管理dirty/untrackedのpath集合・内容hashが停止時と完全一致することを前提とした、
//     親管理metadataのworking tree編集(HEAD不変)
//   - 停止時HEADを祖先に含むHEAD前進。このとき隔離記録がなければ親管理metadataだけの
//     変更範囲に限り、隔離記録があれば記録の実質検証(元task・repo・作成HEADの一致、記録branch
//     の解決と現在HEADへの統合済み、統合後のtipから現在HEADまでが親管理metadata更新だけ、
//     隔離worktree側出自記録との対称)を通過した隔離branch統合として非親管理file変更も認める
//
// それ以外(保持対象の内容変化・消失・新規dirty・停止時HEADを含まないHEAD移動・隔離記録の
// 読込失敗・隔離記録の実質検証失敗)は全てfail closedである。dirty保持fileと統合の両方が触れた
// conflictは内容hashの変化として検出され、解決責任は親Codexにのみある。
func (w *Workflow) verifyInterruptedRetention(checkpoint state.ResumeCheckpoint) error {
	stop := checkpoint.StopGitSnapshot
	if stop == nil || checkpoint.StopDirtyFiles == nil || stop.Head == "" {
		return w.failClosedRetention(checkpoint, "停止時の元checkout保持基準がcheckpointにないため保持を確認できません(旧binaryでの停止・保存時取得失敗)", nil)
	}

	currentFiles, err := state.CaptureStopDirtyFiles(w.config.RepoRoot)
	if err != nil {
		return w.failClosedRetention(checkpoint, "現在の元checkout保持状態を列挙できません", err)
	}
	if diff := state.DescribeStopDirtyDiff(checkpoint.StopDirtyFiles, currentFiles); diff != "" {
		return w.failClosedRetention(checkpoint, "停止時に保持したdirty/untracked fileが変化しています: "+diff, nil)
	}

	current, err := state.CaptureGitSnapshot(w.config.RepoRoot)
	if err != nil {
		return w.failClosedRetention(checkpoint, "現在の元checkout snapshotを取得できません", err)
	}
	if current.Head == stop.Head {
		return nil
	}
	if err := verifyHeadAncestry(w.config.RepoRoot, stop.Head, current.Head); err != nil {
		return w.failClosedRetention(checkpoint, "HEADが停止時commitを祖先に含まない位置へ移動しています", err)
	}

	_, err = w.state.LoadIsolationRecord()
	switch {
	case errors.Is(err, state.ErrNoIsolationRecord):
		nonParent, npErr := headDeltaNonParentPaths(w.config.RepoRoot, stop.Head, current.Head)
		if npErr != nil {
			return w.failClosedRetention(checkpoint, "HEAD移動の変更範囲を確認できません", npErr)
		}
		if len(nonParent) > 0 {
			return w.failClosedRetention(checkpoint, fmt.Sprintf("隔離記録がないままHEADが移動し親管理外file(%s)が変化しています", strings.Join(nonParent, ", ")), nil)
		}
		return nil
	case err != nil:
		return w.failClosedRetention(checkpoint, "隔離記録を読み込めません", err)
	}
	return w.verifyIsolationIntegration(checkpoint, stop, current)
}

// verifyIsolationIntegrationは隔離記録があるHEAD前進を実質検証する。記録の存在だけではなく、
// 記録が現在task・repoの隔離として成立していること(元task ID・repo root一致)、作成HEADが停止時HEAD
// から親管理metadata更新だけで動いていること、記録branchが解決可能でそのtipが現在HEADへ統合済み
// であること、統合後のtipから現在HEADまでが親管理metadata更新だけであること、隔離worktree側stateの
// 出自記録と対称であることを、model呼出なしのgit/state読み取りだけ確認する。stale・破損・別branch・
// 未統合(squash/cherry-pick統合を含む)・統合後の非親管理commitはfail closedである。
func (w *Workflow) verifyIsolationIntegration(checkpoint state.ResumeCheckpoint, stop *state.GitSnapshot, current state.GitSnapshot) error {
	record, err := w.state.LoadIsolationRecord()
	if err != nil {
		return w.failClosedRetention(checkpoint, "隔離記録を読み込めません", err)
	}
	taskID := w.state.ReadOr("task.id", "")
	if record.OriginTaskID != taskID {
		return w.failClosedRetention(checkpoint, fmt.Sprintf("隔離記録の元task(%s)が現在task(%s)と一致しません", record.OriginTaskID, taskID), nil)
	}
	if record.OriginRepoRoot != w.config.RepoRoot {
		return w.failClosedRetention(checkpoint, fmt.Sprintf("隔離記録の元repo(%s)が現在repo(%s)と一致しません", record.OriginRepoRoot, w.config.RepoRoot), nil)
	}
	if record.OriginHead != stop.Head {
		// 停止→--isolateの間に親管理metadataだけがcommitされた場合は作成HEADが停止時HEADの
		// 子孫になる。それ以外のずれ(amend・別branch基準)は承認しない。
		if err := verifyHeadAncestry(w.config.RepoRoot, stop.Head, record.OriginHead); err != nil {
			return w.failClosedRetention(checkpoint, "隔離記録の作成HEADが停止時HEADと一致しません", err)
		}
		nonParent, npErr := headDeltaNonParentPaths(w.config.RepoRoot, stop.Head, record.OriginHead)
		if npErr != nil {
			return w.failClosedRetention(checkpoint, "停止時HEADから隔離作成HEADまでの変更範囲を確認できません", npErr)
		}
		if len(nonParent) > 0 {
			return w.failClosedRetention(checkpoint, fmt.Sprintf("停止時HEADから隔離作成HEADの間に親管理外file(%s)が変化しています", strings.Join(nonParent, ", ")), nil)
		}
	}
	tip, err := state.ResolveBranchTip(w.config.RepoRoot, record.Branch)
	if err != nil {
		return w.failClosedRetention(checkpoint, "隔離記録のbranchが現在repoで解決できません", err)
	}
	if err := verifyHeadAncestry(w.config.RepoRoot, tip, current.Head); err != nil {
		return w.failClosedRetention(checkpoint, "隔離branchのtipが現在HEADへ統合されていません(通常mergeで統合してください。squash/cherry-pick統合は照合できません)", err)
	}
	// tipが祖先でも、統合後に追加されたcommitは承認済みHEAD前進の範囲外である。隔離branch
	// tipから現在HEADまでの間の非親管理file変更はfail closedにする。
	nonParent, npErr := headDeltaNonParentPaths(w.config.RepoRoot, tip, current.Head)
	if npErr != nil {
		return w.failClosedRetention(checkpoint, "隔離branch統合後の変更範囲を確認できません", npErr)
	}
	if len(nonParent) > 0 {
		return w.failClosedRetention(checkpoint, fmt.Sprintf("隔離branch統合後に親管理外file(%s)が変化しています", strings.Join(nonParent, ", ")), nil)
	}
	origin, err := w.state.AttachSiblingStore(config.RepoHashFor(record.Worktree)).LoadIsolationOrigin()
	if err != nil {
		return w.failClosedRetention(checkpoint, "隔離worktree側の出自記録を読み込めません(隔離側state dirは元task完了まで残してください)", err)
	}
	if origin.IsolationID != record.IsolationID || origin.OriginTaskID != record.OriginTaskID ||
		origin.OriginRepoRoot != record.OriginRepoRoot || origin.Branch != record.Branch {
		return w.failClosedRetention(checkpoint, "隔離記録と隔離worktree側の出自記録が一致しません", nil)
	}
	return nil
}

// verifyHeadAncestryはstopHeadがcurrentHeadの祖先であることをgitへ問い合わせる。
// amend・rebase等で停止時commitが歴史上にない場合は失敗する。
func verifyHeadAncestry(repoRoot, stopHead, currentHead string) error {
	command := exec.Command("git", "-C", repoRoot, "merge-base", "--is-ancestor", stopHead, currentHead)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("git merge-base --is-ancestor %s %s: %w: %s", stopHead, currentHead, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// headDeltaNonParentPathsは停止時HEADから現在HEADへのcommit間変更のうち親管理metadata
// 集合以外へ触れたpathを返す。隔離記録がないHEAD前進はここが空のときだけ承認する。
func headDeltaNonParentPaths(repoRoot, stopHead, currentHead string) ([]string, error) {
	args := append([]string{"-C", repoRoot, "diff", "--name-only", "-z", stopHead, currentHead, "--"}, state.ParentExcludePathspecs()...)
	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s %s: %w", stopHead, currentHead, err)
	}
	return splitNul(output), nil
}

// failClosedRetentionは保持確認失敗をtask状態(interrupted)・checkpoint・sessionを変更せず
// worker errorへ終端させる。保持基準からの回復は親Codexが、tracked diffはstateの
// stop-worktree.patch/stop-index.patch、untracked fileは親が別途保持する原本で、元checkoutを
// 停止時内容へ復元した後に同じ--resumeで再試行する。停止patchにuntracked本文は含まれない。
func (w *Workflow) failClosedRetention(checkpoint state.ResumeCheckpoint, reason string, cause error) error {
	if cause != nil {
		reason = fmt.Sprintf("%s: %v", reason, cause)
	}
	now := w.now().UTC()
	w.state.RecordModelCallLog(state.ModelCallLog{
		TaskID:      w.state.ReadOr("task.id", "unknown"),
		CallType:    state.CallTypeEvent,
		StartedAt:   now,
		CompletedAt: now,
		Phase:       checkpoint.Phase + "-retention-check",
		Role:        checkpoint.Role,
		ModelAlias:  checkpoint.Model,
		Outcome:     "retention_mismatch",
		Error:       boundedText(reason, packet.MaxDiagnosticBytes),
	})
	return &WorkerError{Phase: checkpoint.Phase, Message: reason}
}
