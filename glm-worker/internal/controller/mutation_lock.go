package controller

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
)

type mutationLock struct {
	lock *repolock.Lock
}

func (s *Store) acquireMutationLock() (*mutationLock, error) {
	lock, err := repolock.AcquireWait(s.LockPath())
	if err != nil {
		return nil, fmt.Errorf("acquire repository controller mutation lock: %w", err)
	}
	return &mutationLock{lock: lock}, nil
}

func (lock *mutationLock) Close() error {
	if lock == nil || lock.lock == nil {
		return nil
	}
	return lock.lock.Close()
}
