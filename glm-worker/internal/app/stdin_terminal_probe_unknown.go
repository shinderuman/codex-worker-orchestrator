//go:build !darwin && !linux && !windows && !dragonfly && !freebsd && !netbsd && !openbsd

package app

import (
	"fmt"
	"os"
)

func unsupportedPlatformStdinIsTerminal(file *os.File) (bool, error) {
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return false, nil
	}
	if nullInfo, err := os.Stat(os.DevNull); err == nil && os.SameFile(info, nullInfo) {
		return false, nil
	}
	return false, fmt.Errorf("terminal state cannot be verified for character-device stdin on this platform")
}
