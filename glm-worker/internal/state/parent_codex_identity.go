package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type ParentCodexIdentity struct {
	Version   int    `json:"version"`
	TaskID    string `json:"task_id"`
	ThreadID  string `json:"thread_id"`
	SessionID string `json:"session_id"`
}

const (
	parentCodexIdentityFile    = "parent-codex-identity.json"
	parentCodexIdentityVersion = 1

	ParentActionCodexThreadIDEnv  = "GLM_PARENT_ACTION_CODEX_THREAD_ID"
	ParentActionCodexSessionIDEnv = "GLM_PARENT_ACTION_CODEX_SESSION_ID"
)

func (s *StateStore) SetParentCodexIdentity(threadID, sessionID string, readSessionLimit func() *SessionLimitReading) error {
	if !ValidUUIDFormat(threadID) || !ValidUUIDFormat(sessionID) {
		return fmt.Errorf("parent Codex identityが不正です: thread=%s session=%s", threadID, sessionID)
	}
	taskID := s.ReadOr("task.id", "")
	if taskID == "" {
		return nil
	}
	bound, err := s.parentCodexIdentityAlreadyBound(taskID, threadID, sessionID)
	if err != nil || bound {
		return err
	}
	identity := ParentCodexIdentity{Version: parentCodexIdentityVersion, TaskID: taskID, ThreadID: threadID, SessionID: sessionID}
	if err := s.captureParentCodexLimitBaseline(threadID, readSessionLimit); err != nil {
		return err
	}
	return s.writeParentCodexIdentity(identity)
}

func (s *StateStore) parentCodexIdentityAlreadyBound(taskID, threadID, sessionID string) (bool, error) {
	identity, err := s.readParentCodexIdentity()
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if identity.TaskID != taskID || identity.ThreadID != threadID || identity.SessionID != sessionID {
		return false, fmt.Errorf(
			"保存済みparent Codex identityと矛盾します: stored thread=%s session=%s, observed thread=%s session=%s",
			identity.ThreadID, identity.SessionID, threadID, sessionID,
		)
	}
	return true, nil
}

func (s *StateStore) captureParentCodexLimitBaseline(threadID string, readSessionLimit func() *SessionLimitReading) error {
	if readSessionLimit != nil {
		if reading := readSessionLimit(); reading != nil {
			return s.SaveSessionLimitBaseline(threadID, *reading)
		}
	}
	return nil
}

func (s *StateStore) CurrentParentCodexIdentity() (ParentCodexIdentity, error) {
	return s.readParentCodexIdentity()
}

func (s *StateStore) readParentCodexIdentity() (ParentCodexIdentity, error) {
	data, err := os.ReadFile(s.Path(parentCodexIdentityFile))
	if err != nil {
		return ParentCodexIdentity{}, err
	}
	var identity ParentCodexIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return ParentCodexIdentity{}, fmt.Errorf("parent Codex identityを読めません: %w", err)
	}
	if identity.Version != parentCodexIdentityVersion || identity.TaskID == "" || identity.TaskID != s.ReadOr("task.id", "") || !ValidUUIDFormat(identity.ThreadID) || !ValidUUIDFormat(identity.SessionID) {
		return ParentCodexIdentity{}, fmt.Errorf("parent Codex identityのschemaが不正です")
	}
	return identity, nil
}

func (s *StateStore) writeParentCodexIdentity(identity ParentCodexIdentity) error {
	data, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		return fmt.Errorf("parent Codex identityをJSON化できません: %w", err)
	}
	return writeFileAtomic(s.Path(parentCodexIdentityFile), append(data, '\n'), 0o600)
}
