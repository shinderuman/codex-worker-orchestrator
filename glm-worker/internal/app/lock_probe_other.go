//go:build !unix

package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskview"
)

import "os"

func ProbeRepoLock(path string) LockProbe {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return LockProbe{State: LockFree, PID: taskview.StatusNone}
		}
		return LockProbe{State: LockUnknown, PID: "unknown"}
	}
	return LockProbe{State: LockUnknown, PID: parseLockPID(data)}
}
