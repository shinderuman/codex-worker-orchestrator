package repolock

import "errors"

var ErrRepoLockHeld = errors.New("another glm-worker is already running for this repository")
