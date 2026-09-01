package app

import "strings"

type LockState string

type LockProbe struct {
	State LockState
	PID   string
}

const (
	statusNone    = "none"
	statusPartial = "partial"

	LockHeld    LockState = "held"
	LockFree    LockState = "free"
	LockUnknown LockState = "unknown"
)

func parseLockPID(data []byte) string {
	text := string(data)
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = text[:i]
	}
	if strings.TrimSpace(text) == "" {
		return statusNone
	}
	return text
}
