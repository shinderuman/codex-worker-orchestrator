//go:build windows

package app

import (
	"os"
	"syscall"
)

func unsupportedPlatformStdinIsTerminal(file *os.File) (bool, error) {
	var mode uint32
	if err := syscall.GetConsoleMode(syscall.Handle(file.Fd()), &mode); err != nil {
		return false, nil
	}
	return true, nil
}
