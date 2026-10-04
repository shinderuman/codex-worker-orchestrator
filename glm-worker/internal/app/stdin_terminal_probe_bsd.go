//go:build dragonfly || freebsd || netbsd || openbsd

package app

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

func unsupportedPlatformStdinIsTerminal(file *os.File) (bool, error) {
	var state syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(syscall.TIOCGETA), uintptr(unsafe.Pointer(&state)))
	if errno == 0 {
		return true, nil
	}
	if errors.Is(errno, syscall.ENOTTY) {
		return false, nil
	}
	return false, errno
}
