//go:build !unix

package app

import "os"

func ProbeRepoLock(path string) LockProbe {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return LockProbe{State: LockFree, PID: "none"}
		}
		return LockProbe{State: LockUnknown, PID: "unknown"}
	}
	return LockProbe{State: LockUnknown, PID: parseLockPID(data)}
}
