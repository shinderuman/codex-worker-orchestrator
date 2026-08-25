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

func verifyHeadAncestry(repoRoot, stopHead, currentHead string) error {
	command := exec.Command("git", "-C", repoRoot, "merge-base", "--is-ancestor", stopHead, currentHead)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("git merge-base --is-ancestor %s %s: %w: %s", stopHead, currentHead, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func headDeltaNonParentPaths(repoRoot, stopHead, currentHead string) ([]string, error) {
	args := append([]string{"-C", repoRoot, "diff", "--name-only", "-z", stopHead, currentHead, "--"}, state.ParentExcludePathspecs()...)
	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s %s: %w", stopHead, currentHead, err)
	}
	return splitNul(output), nil
}

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
