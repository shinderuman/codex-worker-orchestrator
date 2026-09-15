package app

import (
	"errors"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskview"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
)

type LockState string

type LockProbe struct {
	State LockState
	PID   string
}

type RepoLock = repolock.Lock

const (
	statusPartial = "partial"

	LockHeld    LockState = "held"
	LockFree    LockState = "free"
	LockUnknown LockState = "unknown"
)

var AcquireRepoLock = repolock.Acquire
var ErrRepoLockHeld = repolock.ErrRepoLockHeld
var ErrRepoLockLeaseUnavailable = errors.New("repo lock leaseはこのplatformで取得できません")

func parseLockPID(data []byte) string {
	text := string(data)
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = text[:i]
	}
	if strings.TrimSpace(text) == "" {
		return taskview.StatusNone
	}
	return text
}
