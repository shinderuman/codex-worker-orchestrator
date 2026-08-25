//go:build !darwin && !linux

package app

import "os"

func setStdinFileRaw(file *os.File) (restore func() error, applied bool, err error) {
	return stdinUnsupportedPlatformTerminal(file)
}
