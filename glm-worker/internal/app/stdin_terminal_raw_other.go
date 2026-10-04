//go:build !darwin && !linux

package app

import (
	"fmt"
	"os"
	"runtime"
)

func setStdinFileRaw(file *os.File) (restore func() error, applied bool, err error) {
	terminal, err := unsupportedPlatformStdinIsTerminal(file)
	if err != nil {
		return nil, false, fmt.Errorf("stdin terminal state probe failed on GOOS=%s: %w", runtime.GOOS, err)
	}
	if !terminal {
		return noopStdinRestore, false, nil
	}
	return nil, false, fmt.Errorf("stdin is a terminal, but raw mode is not implemented on this platform (GOOS=%s); feed the payload through a pipe or file", runtime.GOOS)
}
