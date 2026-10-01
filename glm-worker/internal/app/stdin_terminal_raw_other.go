//go:build !darwin && !linux

package app

import (
	"fmt"
	"os"
	"runtime"
)

func setStdinFileRaw(file *os.File) (restore func() error, applied bool, err error) {
	info, statErr := file.Stat()
	if statErr != nil {
		return nil, false, fmt.Errorf("stdin state probe failed on GOOS=%s: %w", runtime.GOOS, statErr)
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return noopStdinRestore, false, nil
	}
	return nil, false, fmt.Errorf("stdin appears to be a terminal, but raw mode is not implemented on this platform (GOOS=%s); feed the payload through a pipe or file", runtime.GOOS)
}
