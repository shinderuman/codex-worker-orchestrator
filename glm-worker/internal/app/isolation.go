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

type isolateOutput struct {
	Result      string `json:"result"`
	IsolationID string `json:"isolation_id"`
	Worktree    string `json:"worktree"`
	Branch      string `json:"branch"`
	TaskID      string `json:"task_id"`
	RepoRoot    string `json:"repo_root"`
}

const isolationBranchPrefix = "glm-worker/isolation/"

func isolateInterruptedTask(st *state.StateStore, cfg config.AppConfig, stdout io.Writer) error {
	taskID, err := isolationTaskID(st)
	if err != nil {
		return err
	}
	replayed, err := replayExistingIsolation(st, cfg, taskID, stdout)
	if err != nil || replayed {
		return err
	}
	return createIsolation(st, cfg, taskID, stdout)
}

func isolationTaskID(st *state.StateStore) (string, error) {
	taskID := st.ReadOr("task.id", "")
	if taskID == "" {
		return "", &workflow.WorkerError{Phase: "isolate", Message: "隔離対象の現在taskがありません"}
	}
	if status := st.TaskStatus(); status != state.TaskStatusInterrupted {
		return "", &workflow.WorkerError{Phase: "isolate", Message: fmt.Sprintf("隔離は--stop停止中(interrupted)のtaskだけを受け付けます。現在: %s", status)}
	}
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil {
		return "", &workflow.WorkerError{Phase: "isolate", Message: "隔離対象のinterrupted checkpointを読み込めません: " + err.Error()}
	}
	if !checkpoint.UserInterrupted || checkpoint.RateLimited || checkpoint.ProviderUnavailable {
		return "", &workflow.WorkerError{Phase: "isolate", Message: "隔離はuser interruptionによる--stop停止状態だけで受け付けます"}
	}
	return taskID, nil
}

func replayExistingIsolation(st *state.StateStore, cfg config.AppConfig, taskID string, stdout io.Writer) (bool, error) {
	existing, err := st.LoadIsolationRecord()
	if errors.Is(err, state.ErrNoIsolationRecord) {
		return false, nil
	}
	if err != nil {
		return false, &workflow.WorkerError{Phase: "isolate", Message: "前回の隔離記録を読み込めません: " + err.Error()}
	}
	return true, replayIsolation(st, cfg, existing, taskID, stdout)
}

func createIsolation(st *state.StateStore, cfg config.AppConfig, taskID string, stdout io.Writer) error {
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

	command := exec.Command("git", "-C", cfg.RepoRoot, "worktree", "add", "--quiet", "-b", branch, worktreePath, head)
	if output, err := command.CombinedOutput(); err != nil {
		return &workflow.WorkerError{Phase: "isolate", Message: fmt.Sprintf("隔離worktreeを作成できません: %v: %s", err, strings.TrimSpace(string(output)))}
	}

	canonical, err := filepath.EvalSymlinks(worktreePath)
	if err != nil {
		removeIsolationWorktree(cfg.RepoRoot, worktreePath, branch)
		return fmt.Errorf("隔離worktreeの実pathを解決できません: %w", err)
	}
	if err := persistIsolationRecords(st, cfg, taskID, head, isolationID, branch, canonical); err != nil {
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

func persistIsolationRecords(st *state.StateStore, cfg config.AppConfig, taskID, head, isolationID, branch, canonical string) error {
	worktreeStore, err := state.NewStateStore(config.AppConfig{
		StateBase: cfg.StateBase,
		RepoHash:  config.RepoHashFor(canonical),
		RepoRoot:  canonical,
	})
	if err != nil {
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
		return err
	}
	return st.SaveIsolationRecord(state.IsolationRecord{
		IsolationID:    isolationID,
		Worktree:       canonical,
		Branch:         branch,
		CreatedAt:      createdAt,
		OriginTaskID:   taskID,
		OriginRepoRoot: cfg.RepoRoot,
		OriginHead:     head,
	})
}

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

func resolveIsolationHead(repoRoot string) (string, error) {
	output, err := exec.Command("git", "-C", repoRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("隔離基準のHEADを取得できません: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func removeIsolationWorktree(repoRoot string, worktreePath string, branch string) {
	_ = exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", worktreePath).Run()
	_ = exec.Command("git", "-C", repoRoot, "branch", "-D", branch).Run()
}
