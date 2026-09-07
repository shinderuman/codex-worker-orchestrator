package workflow

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type parkOutput struct {
	Result          string `json:"result"`
	ParkID          string `json:"park_id"`
	FromStatus      string `json:"from_status"`
	TaskID          string `json:"task_id"`
	Worktree        string `json:"worktree"`
	Branch          string `json:"branch"`
	RepoRoot        string `json:"repo_root"`
	WorkerSession   string `json:"worker_session,omitempty"`
	ReviewerSession string `json:"reviewer_session,omitempty"`
}

type unparkOutput struct {
	Result         string `json:"result"`
	ParkID         string `json:"park_id"`
	TaskID         string `json:"task_id"`
	RestoredStatus string `json:"restored_status"`
	Integration    string `json:"integration"`
	RepoRoot       string `json:"repo_root"`
}

const parkBranchPrefix = "glm-worker/park/"

func (w *Workflow) ExecutePark(stdout io.Writer) error {
	if w.state.TaskStatus() == state.TaskStatusParked {
		record, err := w.state.LoadParkRecord()
		if err != nil {
			return &WorkerError{Phase: "park", Message: "既存のpark記録を読み込めません: " + err.Error()}
		}
		return writeJSONTo(stdout, parkOutput{
			Result:          "parked",
			ParkID:          record.ParkID,
			FromStatus:      string(record.FromStatus),
			TaskID:          record.TaskID,
			Worktree:        record.Worktree,
			Branch:          record.Branch,
			RepoRoot:        record.RepoRoot,
			WorkerSession:   record.WorkerSessionID,
			ReviewerSession: record.ReviewerSessionID,
		})
	}
	if !state.ParkAdmissibleFrom(w.state.TaskStatus()) {
		return &WorkerError{Phase: "park", Message: fmt.Sprintf("parkは親判断待ち(waiting-sol-review/waiting-decision)のtaskだけを受け付けます。現在: %s", w.state.TaskStatus())}
	}
	record, err := w.captureParkRecord()
	if err != nil {
		return err
	}
	if err := w.createParkWorktree(&record); err != nil {
		return err
	}
	if err := w.state.EnterParked(record); err != nil {
		w.removeParkWorktree(record)
		return &WorkerError{Phase: "park", Message: "park状態への遷移に失敗しました: " + err.Error()}
	}
	w.state.RecordModelCallLog(state.ModelCallLog{
		TaskID:      record.TaskID,
		CallType:    state.CallTypeEvent,
		StartedAt:   w.now().UTC(),
		CompletedAt: w.now().UTC(),
		Phase:       "park",
		Outcome:     "parked",
		Error:       boundedText(fmt.Sprintf("parked from %s; worktree %s", record.FromStatus, record.Worktree), 4096),
	})
	return writeJSONTo(stdout, parkOutput{
		Result:          "parked",
		ParkID:          record.ParkID,
		FromStatus:      string(record.FromStatus),
		TaskID:          record.TaskID,
		Worktree:        record.Worktree,
		Branch:          record.Branch,
		RepoRoot:        record.RepoRoot,
		WorkerSession:   record.WorkerSessionID,
		ReviewerSession: record.ReviewerSessionID,
	})
}

func (w *Workflow) captureParkRecord() (state.ParkRecord, error) {
	taskID := w.state.ReadOr("task.id", "")
	if taskID == "" {
		return state.ParkRecord{}, &WorkerError{Phase: "park", Message: "park対象の現在taskがありません"}
	}
	parkID, err := state.NewUUID()
	if err != nil {
		return state.ParkRecord{}, err
	}
	snapshot, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		return state.ParkRecord{}, &WorkerError{Phase: "park", Message: "park基準のrepository snapshotを取得できません: " + err.Error()}
	}
	dirtyFiles, err := state.CaptureStopDirtyFiles(w.config.RepoRoot)
	if err != nil {
		return state.ParkRecord{}, &WorkerError{Phase: "park", Message: "park保持対象のdirty/untracked fileを列挙できません: " + err.Error()}
	}
	if err := w.writeParkContentCopies(dirtyFiles); err != nil {
		return state.ParkRecord{}, err
	}
	branch := parkBranchPrefix + parkID[:8]
	worktreePath := filepath.Join(w.config.WorktreeBase, w.config.RepoShort, parkID)
	record := state.ParkRecord{
		ParkID:            parkID,
		FromStatus:        w.state.TaskStatus(),
		CreatedAt:         w.now().UTC().Format(time.RFC3339),
		TaskID:            taskID,
		WorkerSessionID:   w.state.ReadOr("worker.id", ""),
		ReviewerSessionID: w.state.ReadOr("reviewer.id", ""),
		RepoRoot:          w.config.RepoRoot,
		Head:              snapshot.Head,
		Snapshot:          snapshot,
		DirtyFiles:        dirtyFiles,
		ParentFiles:       snapshot.ParentFiles,
		Baseline:          w.state.BaselineEvidence(),
		Worktree:          worktreePath,
		Branch:            branch,
	}
	return record, nil
}

func (w *Workflow) writeParkContentCopies(files []state.StopDirtyFile) error {
	for _, file := range files {
		abs, err := joinRepoPath(w.config.RepoRoot, file.Path)
		if err != nil {
			return &WorkerError{Phase: "park", Message: fmt.Sprintf("park本文保持対象 %s を解決できません: %v", file.Path, err)}
		}
		content, err := os.ReadFile(abs)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return &WorkerError{Phase: "park", Message: fmt.Sprintf("park本文保持対象 %s を読めません: %v", file.Path, err)}
		}
		target := w.state.ParkContentPath(filepath.ToSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return &WorkerError{Phase: "park", Message: "park本文保持directoryを作成できません: " + err.Error()}
		}
		if err := writeFileForPark(target, content); err != nil {
			return &WorkerError{Phase: "park", Message: fmt.Sprintf("park本文保持対象 %s を保存できません: %v", file.Path, err)}
		}
	}
	return nil
}

func writeFileForPark(path string, content []byte) error {
	return os.WriteFile(path, content, 0o600)
}

func (w *Workflow) createParkWorktree(record *state.ParkRecord) error {
	if err := os.MkdirAll(filepath.Dir(record.Worktree), 0o700); err != nil {
		return &WorkerError{Phase: "park", Message: "割込みworktreeの親directoryを作成できません: " + err.Error()}
	}
	command := exec.Command("git", "-C", w.config.RepoRoot, "worktree", "add", "--quiet", "-b", record.Branch, record.Worktree, record.Head)
	if output, err := command.CombinedOutput(); err != nil {
		return &WorkerError{Phase: "park", Message: fmt.Sprintf("割込みworktreeを作成できません: %v: %s", err, strings.TrimSpace(string(output)))}
	}
	canonical, err := filepath.EvalSymlinks(record.Worktree)
	if err != nil {
		w.removeParkWorktree(*record)
		return &WorkerError{Phase: "park", Message: "割込みworktreeの実pathを解決できません: " + err.Error()}
	}
	record.Worktree = canonical
	if err := w.persistParkOrigin(*record); err != nil {
		w.removeParkWorktree(*record)
		return err
	}
	return nil
}

func (w *Workflow) persistParkOrigin(record state.ParkRecord) error {
	worktreeStore, err := state.NewStateStore(config.AppConfig{
		StateBase: w.config.StateBase,
		RepoHash:  config.RepoHashFor(record.Worktree),
		RepoRoot:  record.Worktree,
	})
	if err != nil {
		return err
	}
	return worktreeStore.SaveParkOrigin(state.ParkOrigin{
		ParkID:    record.ParkID,
		RepoRoot:  record.RepoRoot,
		TaskID:    record.TaskID,
		Branch:    record.Branch,
		CreatedAt: record.CreatedAt,
	})
}

func (w *Workflow) removeParkWorktree(record state.ParkRecord) {
	_ = w.removeParkWorktreeChecked(record)
}

func (w *Workflow) ExecuteUnpark(stdout io.Writer) error {
	if w.state.TaskStatus() != state.TaskStatusParked {
		return &WorkerError{Phase: "unpark", Message: fmt.Sprintf("unparkはparked taskだけを受け付けます。現在: %s", w.state.TaskStatus())}
	}
	record, err := w.state.LoadParkRecord()
	if err != nil {
		return &WorkerError{Phase: "unpark", Message: "park記録を読み込めません: " + err.Error()}
	}
	integration, err := w.verifyParkIntegration(record)
	if err != nil {
		return err
	}
	restored, err := w.state.LeaveParked()
	if err != nil {
		return &WorkerError{Phase: "unpark", Message: "park状態からの復帰に失敗しました: " + err.Error()}
	}
	cleanupNote := w.cleanupParkResources(record)
	w.state.RecordModelCallLog(state.ModelCallLog{
		TaskID:      record.TaskID,
		CallType:    state.CallTypeEvent,
		StartedAt:   w.now().UTC(),
		CompletedAt: w.now().UTC(),
		Phase:       "unpark",
		Outcome:     "unparked",
		Error:       boundedText(fmt.Sprintf("unparked to %s; integration %s; %s", restored, integration, cleanupNote), 4096),
	})
	return writeJSONTo(stdout, unparkOutput{
		Result:         "unparked",
		ParkID:         record.ParkID,
		TaskID:         record.TaskID,
		RestoredStatus: string(restored),
		Integration:    integration,
		RepoRoot:       record.RepoRoot,
	})
}

func (w *Workflow) cleanupParkResources(record state.ParkRecord) string {
	notes := make([]string, 0, 4)
	if err := w.removeParkWorktreeChecked(record); err != nil {
		notes = append(notes, "worktree cleanup: "+err.Error())
	} else {
		notes = append(notes, "worktree cleaned")
	}
	if err := w.state.ClearParkRecord(); err != nil {
		notes = append(notes, "park record cleanup: "+err.Error())
	} else {
		notes = append(notes, "park record cleaned")
	}
	if err := w.state.RemoveParkContent(); err != nil {
		notes = append(notes, "park content cleanup: "+err.Error())
	} else {
		notes = append(notes, "park content cleaned")
	}
	if err := w.state.AttachSiblingStore(config.RepoHashFor(record.Worktree)).RemoveParkOrigin(); err != nil {
		notes = append(notes, "park origin cleanup: "+err.Error())
	} else {
		notes = append(notes, "park origin cleaned")
	}
	return strings.Join(notes, "; ")
}

func (w *Workflow) removeParkWorktreeChecked(record state.ParkRecord) error {
	remove := exec.Command("git", "-C", w.config.RepoRoot, "worktree", "remove", "--force", record.Worktree)
	if output, err := remove.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove %s: %w: %s", record.Worktree, err, strings.TrimSpace(string(output)))
	}
	delBranch := exec.Command("git", "-C", w.config.RepoRoot, "branch", "-D", record.Branch)
	if output, err := delBranch.CombinedOutput(); err != nil {
		return fmt.Errorf("git branch -D %s: %w: %s", record.Branch, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (w *Workflow) verifyParkIntegration(record state.ParkRecord) (string, error) {
	if record.RepoRoot != w.config.RepoRoot {
		return "", &WorkerError{Phase: "unpark", Message: fmt.Sprintf("park記録のrepo(%s)が現在repo(%s)と一致しません", record.RepoRoot, w.config.RepoRoot)}
	}
	if record.TaskID != w.state.ReadOr("task.id", "") {
		return "", &WorkerError{Phase: "unpark", Message: fmt.Sprintf("park記録のtask(%s)が現在task(%s)と一致しません", record.TaskID, w.state.ReadOr("task.id", ""))}
	}
	currentFiles, err := state.CaptureStopDirtyFiles(w.config.RepoRoot)
	if err != nil {
		return "", &WorkerError{Phase: "unpark", Message: "復帰時のdirty/untracked保持状態を列挙できません: " + err.Error()}
	}
	if diff := state.DescribeStopDirtyDiff(record.DirtyFiles, currentFiles); diff != "" {
		return "", &WorkerError{Phase: "unpark", Message: "park中に保持対象のdirty/untracked fileが変化しています: " + diff}
	}
	current, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		return "", &WorkerError{Phase: "unpark", Message: "復帰時のrepository snapshotを取得できません: " + err.Error()}
	}
	return w.verifyParkHeadProvenance(record, current)
}

func (w *Workflow) verifyParkHeadProvenance(record state.ParkRecord, current state.GitSnapshot) (string, error) {
	tip, tipErr := state.ResolveBranchTip(w.config.RepoRoot, record.Branch)
	if tipErr == nil && tip != record.Head {
		if err := verifyHeadAncestry(w.config.RepoRoot, tip, current.Head); err != nil {
			return "", &WorkerError{Phase: "unpark", Message: "割込みbranchの成果が現在HEADへ統合されていないため復帰できません(統合後にunparkしてください): " + err.Error()}
		}
		return "interrupt-integrated", nil
	}
	if current.Head == record.Head {
		return "head-unchanged", nil
	}
	if err := verifyHeadAncestry(w.config.RepoRoot, record.Head, current.Head); err != nil {
		return "", &WorkerError{Phase: "unpark", Message: "park後のHEAD移動がpark基準commitを祖先に含みません: " + err.Error()}
	}
	nonParent, err := headDeltaNonParentPaths(w.config.RepoRoot, record.Head, current.Head)
	if err != nil {
		return "", &WorkerError{Phase: "unpark", Message: "park後のHEAD移動範囲を確認できません: " + err.Error()}
	}
	if len(nonParent) > 0 {
		return "", &WorkerError{Phase: "unpark", Message: fmt.Sprintf("park後に割込みbranch経由ではない親管理外file(%s)の変更があるため復帰できません", strings.Join(nonParent, ", "))}
	}
	return "parent-metadata-only", nil
}

func writeJSONTo(stdout io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = stdout.Write(append(data, '\n'))
	return err
}

func joinRepoPath(repoRoot string, relative string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(relative))
	if clean == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("path %qがrepository相対ではありません", relative)
	}
	return filepath.Join(repoRoot, filepath.FromSlash(clean)), nil
}
