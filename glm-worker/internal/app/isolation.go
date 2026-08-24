// --isolate: --stop停止中の元taskを保持したまま割り込みtask実行用checkoutを作る。
package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

// isolationBranchPrefixは--isolateが作成するbranchの固定接頭辞。
const isolationBranchPrefix = "glm-worker/isolation/"

// isolateOutputは--isolate成功時のmachine JSON 1行。親Codexはworktreeで割り込みtaskを
// 実行し、統合・worktree削除の責務を持つ。
type isolateOutput struct {
	Result      string `json:"result"`
	IsolationID string `json:"isolation_id"`
	Worktree    string `json:"worktree"`
	Branch      string `json:"branch"`
	TaskID      string `json:"task_id"`
	RepoRoot    string `json:"repo_root"`
}

// isolateInterruptedTaskは--stop停止中(user interruption)の元taskから隔離checkout
// (git worktree + branch)をHEAD位置へ作成し、元repo側とworktree側のstateへ対称な
// 隔離記録を保存する。元checkoutのworking tree・元taskのstate/checkpoint/sessionへ
// 書き込まず、隔離先はpath由来の別repo hashでstate・lock・session・stop endpointが
// 分離される。統合(branch merge)の実施時期とconflict解決は親Codexの責務であり、
// 元taskのresume保持照合が統合済みHEADを隔離記録の実質検証付きで承認する。
// 既存の有効な隔離記録があるときは新規作成せず同じmachine結果を冪等に返し、
// stale・破損記録は上書きせずfail closedにする。
func isolateInterruptedTask(st *state.StateStore, cfg config.AppConfig, stdout io.Writer) error {
	taskID := st.ReadOr("task.id", "")
	if taskID == "" {
		return &workflow.WorkerError{Phase: "isolate", Message: "隔離対象の現在taskがありません"}
	}
	if status := st.TaskStatus(); status != state.TaskStatusInterrupted {
		return &workflow.WorkerError{Phase: "isolate", Message: fmt.Sprintf("隔離は--stop停止中(interrupted)のtaskだけを受け付けます。現在: %s", status)}
	}
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil {
		return &workflow.WorkerError{Phase: "isolate", Message: "隔離対象のinterrupted checkpointを読み込めません: " + err.Error()}
	}
	if !checkpoint.UserInterrupted || checkpoint.RateLimited || checkpoint.ProviderUnavailable {
		return &workflow.WorkerError{Phase: "isolate", Message: "隔離はuser interruptionによる--stop停止状態だけで受け付けます"}
	}

	// 既存隔離記録の再実行は冪等再観測かfail closedのどちらかで、先行隔離先を孤児化する
	// 上書きを作らない。
	switch existing, recErr := st.LoadIsolationRecord(); {
	case errors.Is(recErr, state.ErrNoIsolationRecord):
	case recErr != nil:
		return &workflow.WorkerError{Phase: "isolate", Message: "前回の隔離記録を読み込めません: " + recErr.Error()}
	default:
		return replayIsolation(st, cfg, existing, taskID, stdout)
	}

	head, err := resolveIsolationHead(cfg.RepoRoot)
	if err != nil {
		return err
	}
	isolationID, err := state.NewUUID()
	if err != nil {
		return err
	}
	branch := isolationBranchPrefix + isolationID[:8]
	worktreePath := filepath.Join(cfg.WorktreeBase, cfg.RepoShort, isolationID)
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
		return fmt.Errorf("隔離worktreeの親directoryを作成できません: %w", err)
	}

	// git worktree addは空でない存在dirを拒否するため、worktreePath自体は作らない。
	command := exec.Command("git", "-C", cfg.RepoRoot, "worktree", "add", "--quiet", "-b", branch, worktreePath, head)
	if output, err := command.CombinedOutput(); err != nil {
		return &workflow.WorkerError{Phase: "isolate", Message: fmt.Sprintf("隔離worktreeを作成できません: %v: %s", err, strings.TrimSpace(string(output)))}
	}
	// macOSのstate baseは/var symlink配下になり得る。state分離keyと記録は解決後pathで
	// 固定しないと--status・再実行が同じhashを引かない。
	canonical, err := filepath.EvalSymlinks(worktreePath)
	if err != nil {
		removeIsolationWorktree(cfg.RepoRoot, worktreePath, branch)
		return fmt.Errorf("隔離worktreeの実pathを解決できません: %w", err)
	}

	worktreeStore, err := state.NewStateStore(config.AppConfig{
		StateBase: cfg.StateBase,
		RepoHash:  config.RepoHashFor(canonical),
		RepoRoot:  canonical,
	})
	if err != nil {
		removeIsolationWorktree(cfg.RepoRoot, worktreePath, branch)
		return err
	}
	createdAt := time.Now().UTC().Format(time.RFC3339)
	if err := worktreeStore.SaveIsolationOrigin(state.IsolationOrigin{
		IsolationID:    isolationID,
		OriginRepoRoot: cfg.RepoRoot,
		OriginTaskID:   taskID,
		Branch:         branch,
		CreatedAt:      createdAt,
	}); err != nil {
		removeIsolationWorktree(cfg.RepoRoot, worktreePath, branch)
		return err
	}
	// 元repo側の隔離記録を最後に書く。ここまでの失敗はresume保持照合へ隔離経路を
	// 出現させない。記録は現在の隔離先を指す単一pointerで、再--isolateは上書きせず
	// 冪等再観測かfail closedにする(replayIsolation)。
	if err := st.SaveIsolationRecord(state.IsolationRecord{
		IsolationID:    isolationID,
		Worktree:       canonical,
		Branch:         branch,
		CreatedAt:      createdAt,
		OriginTaskID:   taskID,
		OriginRepoRoot: cfg.RepoRoot,
		OriginHead:     head,
	}); err != nil {
		removeIsolationWorktree(cfg.RepoRoot, worktreePath, branch)
		return err
	}

	return writeJSON(stdout, isolateOutput{
		Result:      "isolated",
		IsolationID: isolationID,
		Worktree:    canonical,
		Branch:      branch,
		TaskID:      taskID,
		RepoRoot:    cfg.RepoRoot,
	})
}

// replayIsolationは有効な既存隔離記録へ同じmachine結果を冪等に返す。新worktreeを作って
// 単一pointerを上書きせず、記録が現在task・repoの生きた隔離としてstill成立していること
// (worktree・branch・隔離側出自記録の対称)だけを確認する。確認できない記録はstaleとして
// fail closedにし、都合のよい上書きをしない。
func replayIsolation(st *state.StateStore, cfg config.AppConfig, record state.IsolationRecord, taskID string, stdout io.Writer) error {
	if record.OriginTaskID != taskID {
		return &workflow.WorkerError{Phase: "isolate", Message: fmt.Sprintf("既存隔離記録の元task(%s)が現在task(%s)と一致しません", record.OriginTaskID, taskID)}
	}
	if record.OriginRepoRoot != cfg.RepoRoot {
		return &workflow.WorkerError{Phase: "isolate", Message: fmt.Sprintf("既存隔離記録の元repo(%s)が現在repo(%s)と一致しません", record.OriginRepoRoot, cfg.RepoRoot)}
	}
	if info, err := os.Stat(record.Worktree); err != nil || !info.IsDir() {
		return &workflow.WorkerError{Phase: "isolate", Message: "既存の隔離記録がありますが隔離先worktreeが存在しないため再実行できません(記録は上書きしません)"}
	}
	if _, err := state.ResolveBranchTip(cfg.RepoRoot, record.Branch); err != nil {
		return &workflow.WorkerError{Phase: "isolate", Message: "既存の隔離記録のbranchが解決できないため再実行できません: " + err.Error()}
	}
	origin, err := st.AttachSiblingStore(config.RepoHashFor(record.Worktree)).LoadIsolationOrigin()
	if err != nil || origin.IsolationID != record.IsolationID || origin.OriginTaskID != record.OriginTaskID ||
		origin.OriginRepoRoot != record.OriginRepoRoot || origin.Branch != record.Branch {
		return &workflow.WorkerError{Phase: "isolate", Message: "既存の隔離記録と隔離worktree側の出自記録が一致しないため再実行できません"}
	}
	return writeJSON(stdout, isolateOutput{
		Result:      "isolated",
		IsolationID: record.IsolationID,
		Worktree:    record.Worktree,
		Branch:      record.Branch,
		TaskID:      record.OriginTaskID,
		RepoRoot:    record.OriginRepoRoot,
	})
}

// resolveIsolationHeadは隔離branchの作成位置を現在HEADとして解決する。
func resolveIsolationHead(repoRoot string) (string, error) {
	output, err := exec.Command("git", "-C", repoRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("隔離基準のHEADを取得できません: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// removeIsolationWorktreeは隔離作成の失敗経路でgit側資材を取り下げる。取り下げ失敗は
// 呼出元の失敗理由を上書きしない。
func removeIsolationWorktree(repoRoot string, worktreePath string, branch string) {
	_ = exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", worktreePath).Run()
	_ = exec.Command("git", "-C", repoRoot, "branch", "-D", branch).Run()
}
