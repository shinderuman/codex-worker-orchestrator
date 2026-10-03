package app

import (
	"errors"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskview"
	"os"
	"path/filepath"
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

func acquireWorkflowLock(cfg config.AppConfig) (*RepoLock, error) {
	path, err := controller.WorkflowLockPath(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return AcquireRepoLock(path)
}
