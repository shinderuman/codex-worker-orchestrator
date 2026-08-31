package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

type SessionRole string

type TaskStatus string

type StateStore struct {
	dir string
}

const (
	WorkerRole   SessionRole = "worker"
	ReviewerRole SessionRole = "reviewer"

	TaskStatusActive              TaskStatus = "active"
	TaskStatusWaitingDecision     TaskStatus = "waiting-decision"
	TaskStatusWaitingSolReview    TaskStatus = "waiting-sol-review"
	TaskStatusComplete            TaskStatus = "complete"
	TaskStatusRateLimited         TaskStatus = "rate-limited"
	TaskStatusProviderUnavailable TaskStatus = "provider-unavailable"
	TaskStatusGuardRecoverable    TaskStatus = "guard-recoverable"

	TaskStatusInterrupted TaskStatus = "interrupted"

	TaskStatusNone TaskStatus = "none"

	ExecutionMilestonesStateFile = "execution-milestones.json"
)

func NewStateStore(config config.AppConfig) (*StateStore, error) {
	dir := filepath.Join(config.StateBase, config.RepoHash)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("GLM stateディレクトリを作成できません: %w", err)
	}

	state := &StateStore{dir: dir}
	if err := state.Write("repo-root", config.RepoRoot); err != nil {
		return nil, err
	}
	return state, nil
}

func AttachStateStore(config config.AppConfig) *StateStore {
	return &StateStore{dir: filepath.Join(config.StateBase, config.RepoHash)}
}

func (s *StateStore) AttachSiblingStore(repoHash string) *StateStore {
	return &StateStore{dir: filepath.Join(filepath.Dir(s.dir), repoHash)}
}

func (s *StateStore) Path(name string) string {
	return filepath.Join(s.dir, name)
}

func (s *StateStore) LockPath() string {
	return s.Path("lock")
}

func (s *StateStore) Exists(name string) bool {
	_, err := os.Stat(s.Path(name))
	return err == nil
}

func (s *StateStore) Read(name string) (string, error) {
	data, err := os.ReadFile(s.Path(name))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (s *StateStore) ReadOr(name string, fallback string) string {
	value, err := s.Read(name)
	if err != nil || value == "" {
		return fallback
	}
	return value
}

func (s *StateStore) Write(name string, value string) error {
	if err := writeFileAtomic(s.Path(name), []byte(value+"\n"), 0o600); err != nil {
		return fmt.Errorf("state %sを書き込めません: %w", name, err)
	}
	return nil
}

func (s *StateStore) Touch(name string) error {
	return s.Write(name, "1")
}

func (s *StateStore) Remove(names ...string) error {
	for _, name := range names {
		err := os.Remove(s.Path(name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("state %sを削除できません: %w", name, err)
		}
	}
	return nil
}

func (s *StateStore) StartNewTask() (string, error) {
	s.ArchiveCurrentStats()
	if err := s.Remove(
		"task.id",
		"worker.id",
		"worker.ready",
		"reviewer.id",
		"reviewer.ready",
		"task.status",
		"isolation.policy",
		"baseline-head",
		ExecutionMilestonesStateFile,
		stopWorktreePatchFile,
		stopIndexPatchFile,
		isolationStateFile,
		workerEndSnapshotFile,
		reviewStartSnapshotFile,
		reportOnlyStartSnapshotFile,
		snapshotComparisonFile,
	); err != nil {
		return "", err
	}

	taskID, err := NewUUID()
	if err != nil {
		return "", err
	}
	if err := s.Write("task.id", taskID); err != nil {
		return "", err
	}
	s.PruneTaskEventLogs(retainedTaskEventLogs, taskID)
	s.InitializeTaskStats(taskID)
	if err := s.SetTaskStatus(TaskStatusActive); err != nil {
		return "", err
	}
	return taskID, nil
}

func taskStateFileNames() []string {
	return []string{
		"task.id",
		"worker.id",
		"worker.ready",
		"reviewer.id",
		"reviewer.ready",
		"task.status",
		"isolation.policy",

		"active-task",
		"last-request",
		"last-decision",
		"pending-decision",
		"last-review",
		"baseline-status",
		"baseline-head",
		"baseline-worktree.patch",
		"baseline-index.patch",
		"accepted-fix-scope.json",
		ExecutionMilestonesStateFile,
		stopWorktreePatchFile,
		stopIndexPatchFile,
		isolationStateFile,
		isolationOriginStateFile,
		resumeStateFile,
		workerEndSnapshotFile,
		reviewStartSnapshotFile,
		reportOnlyStartSnapshotFile,
		snapshotComparisonFile,
	}
}

func (s *StateStore) TaskID() (string, error) {
	taskID, err := s.Read("task.id")
	if err != nil || taskID == "" {
		return "", fmt.Errorf("task.idがありません")
	}
	return taskID, nil
}

func (s *StateStore) TaskStatus() TaskStatus {
	if status, err := s.Read("task.status"); err == nil && status != "" {
		return TaskStatus(status)
	}
	return TaskStatusNone
}

func (s *StateStore) SetTaskStatus(status TaskStatus) error {
	previous := s.TaskStatus()
	if err := s.Write("task.status", string(status)); err != nil {
		return err
	}
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.Status = status
	})
	if previous != status {
		s.appendTaskStatusLifecycle(previous, status)
	}
	return nil
}

func (s *StateStore) appendTaskStatusLifecycle(previous, next TaskStatus) {
	taskID := s.ReadOr("task.id", "")
	if taskID == "" {
		return
	}
	record := TaskLifecycleRecord{
		TaskID:    taskID,
		Timestamp: time.Now().UTC(),
		From:      string(previous),
		To:        string(next),
	}
	if err := s.AppendTaskLifecycle(record); err != nil {
		WarnTaskLifecycleSkip(err)
	}
}

func (s *StateStore) SessionID(role SessionRole) (string, bool, error) {
	idName := string(role) + ".id"
	if id, err := s.Read(idName); err == nil && id != "" {
		return id, s.Exists(string(role) + ".ready"), nil
	}

	id, err := NewUUID()
	if err != nil {
		return "", false, err
	}
	if err := s.Write(idName, id); err != nil {
		return "", false, err
	}
	return id, false, nil
}

func (s *StateStore) MarkReady(role SessionRole) error {
	return s.Touch(string(role) + ".ready")
}

func (s *StateStore) RemoveUnreadySession(role SessionRole) error {
	if s.Exists(string(role) + ".ready") {
		return nil
	}
	return s.Remove(string(role) + ".id")
}

func (s *StateStore) ResetSessionsForPolicy(currentPolicy string) error {
	if s.IsolationPolicy() == currentPolicy {
		return nil
	}
	return s.Remove(
		"worker.id", "worker.ready",
		"reviewer.id", "reviewer.ready",
	)
}

func (s *StateStore) IsolationPolicy() string {
	return s.ReadOr("isolation.policy", "")
}

func (s *StateStore) SetIsolationPolicy(version string) error {
	return s.Write("isolation.policy", version)
}
