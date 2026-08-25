package app

import (
	"errors"
	"strings"
)

var ErrRepoLockHeld = errors.New("another glm-worker is already running for this repository")

type LockState string

const (
	LockHeld    LockState = "held"
	LockFree    LockState = "free"
	LockUnknown LockState = "unknown"
)

type LockProbe struct {
	State LockState
	PID   string
}

func parseLockPID(data []byte) string {
	text := string(data)
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = text[:i]
	}
	if strings.TrimSpace(text) == "" {
		return "none"
	}
	return text
}
