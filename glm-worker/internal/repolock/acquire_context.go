package repolock

import (
	"context"
	"errors"
	"time"
)

const contextAcquirePollInterval = 20 * time.Millisecond

func AcquireContext(ctx context.Context, path string) (*Lock, error) {
	for {
		lock, err := Acquire(path)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, ErrRepoLockHeld) {
			return nil, err
		}
		timer := time.NewTimer(contextAcquirePollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
