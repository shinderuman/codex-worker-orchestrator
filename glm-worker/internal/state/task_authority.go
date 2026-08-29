package state

import (
	"fmt"
	"os"
	"path/filepath"
)

const taskAuthorityDir = "task-authority"

func (s *StateStore) SaveCurrentTaskAuthority(taskPath string, content []byte) error {
	if taskPath == "" {
		return nil
	}
	taskID, err := s.TaskID()
	if err != nil {
		return err
	}
	dir := s.TaskAuthorityDir(taskID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("task authority directoryを作成できません: %w", err)
	}
	if err := writeFileAtomic(s.TaskAuthorityContentPath(taskID), content, 0o600); err != nil {
		return fmt.Errorf("task authority contentを保存できません: %w", err)
	}
	if err := writeFileAtomic(s.TaskAuthorityPathPath(taskID), []byte(taskPath+"\n"), 0o600); err != nil {
		return fmt.Errorf("task authority pathを保存できません: %w", err)
	}
	return nil
}

func (s *StateStore) TaskAuthorityDir(taskID string) string {
	return s.Path(filepath.Join(taskAuthorityDir, taskID))
}

func (s *StateStore) TaskAuthorityPathPath(taskID string) string {
	return filepath.Join(s.TaskAuthorityDir(taskID), "active-task.path")
}

func (s *StateStore) TaskAuthorityContentPath(taskID string) string {
	return filepath.Join(s.TaskAuthorityDir(taskID), "active-task.md")
}
