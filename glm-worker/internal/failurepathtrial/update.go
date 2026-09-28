package failurepathtrial

import (
	"errors"
	"fmt"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
)

type RegistryUpdate func(Registry) (Registry, error)

var ErrCohortFull = errors.New("failure-path trial cohort上限に到達しているためrecordをregistryへ追加できません")

var registryLockWait = 2 * time.Second

var registryLockRetryInterval = 50 * time.Millisecond

func UpdateRegistry(path string, update RegistryUpdate) (Registry, error) {
	lock, err := acquireRegistryLock(path)
	if err != nil {
		return Registry{}, err
	}
	defer func() { _ = lock.Close() }()
	registry, err := LoadRegistry(path)
	if err != nil {
		return Registry{}, err
	}
	updated, err := update(registry)
	if err != nil {
		return Registry{}, err
	}
	if err := SaveRegistry(path, updated); err != nil {
		return Registry{}, err
	}
	return updated, nil
}

func AppendRecord(path string, record Record) (Registry, error) {
	return UpdateRegistry(path, func(registry Registry) (Registry, error) {
		if registry.HasTaskRecord(record.TaskID) {
			return registry, nil
		}
		if cohortOutcomes[record.Outcome] && registry.CohortSize() >= CohortCap {
			return Registry{}, ErrCohortFull
		}
		return registry.WithRecord(record), nil
	})
}

func acquireRegistryLock(path string) (*repolock.Lock, error) {
	deadline := time.Now().Add(registryLockWait)
	for {
		lock, err := repolock.Acquire(path + ".lock")
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, repolock.ErrRepoLockHeld) {
			return nil, fmt.Errorf("failure-path trial registry lockを取得できません: %w", err)
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("failure-path trial registry lock競合(上限%s): %w", registryLockWait, repolock.ErrRepoLockHeld)
		}
		time.Sleep(registryLockRetryInterval)
	}
}
