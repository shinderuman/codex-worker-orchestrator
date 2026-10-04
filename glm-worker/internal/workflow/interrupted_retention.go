package workflow

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) verifyInterruptedRetention(checkpoint state.ResumeCheckpoint) error {
	stop := checkpoint.StopGitSnapshot
	if stop == nil || checkpoint.StopDirtyFiles == nil || stop.Head == "" {
		return w.failClosedRetention(checkpoint, "停止時の元checkout保持基準がcheckpointにないため保持を確認できません(旧binaryでの停止・保存時取得失敗)", nil)
	}
	current, err := w.verifyStoppedCheckoutState(checkpoint, stop)
	if err != nil || current.Head == stop.Head {
		return err
	}
	if err := verifyHeadAncestry(w.config.RepoRoot, stop.Head, current.Head); err != nil {
		return w.failClosedRetention(checkpoint, "HEADが停止時commitを祖先に含まない位置へ移動しています", err)
	}
	return w.verifyHeadMoveAfterStop(checkpoint, stop.Head, current.Head)
}

func (w *Workflow) verifyStoppedCheckoutState(checkpoint state.ResumeCheckpoint, _ *state.GitSnapshot) (state.GitSnapshot, error) {
	currentFiles, err := state.CaptureStopDirtyFiles(w.config.RepoRoot)
	if err != nil {
		return state.GitSnapshot{}, w.failClosedRetention(checkpoint, "現在の元checkout保持状態を列挙できません", err)
	}
	if diff := state.DescribeStopDirtyDiff(checkpoint.StopDirtyFiles, currentFiles); diff != "" {
		return state.GitSnapshot{}, w.failClosedRetention(checkpoint, "停止時に保持したdirty/untracked fileが変化しています: "+diff, nil)
	}
	current, err := state.CaptureGitSnapshot(w.config.RepoRoot)
	if err != nil {
		return state.GitSnapshot{}, w.failClosedRetention(checkpoint, "現在の元checkout snapshotを取得できません", err)
	}
	return current, nil
}

func (w *Workflow) verifyHeadMoveAfterStop(checkpoint state.ResumeCheckpoint, stopHead, currentHead string) error {
	nonParent, err := headDeltaNonParentPaths(w.config.RepoRoot, stopHead, currentHead)
	if err != nil {
		return w.failClosedRetention(checkpoint, "HEAD移動の変更範囲を確認できません", err)
	}
	if len(nonParent) > 0 {
		return w.failClosedRetention(checkpoint, fmt.Sprintf("停止後のHEAD移動で親管理外file(%s)が変化しています", strings.Join(nonParent, ", ")), nil)
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
