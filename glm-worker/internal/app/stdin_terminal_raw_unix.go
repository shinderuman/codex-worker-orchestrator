//go:build darwin || linux

package app

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

func setStdinFileRaw(file *os.File) (restore func() error, applied bool, err error) {
	var saved syscall.Termios
	if err := getTerminalAttrs(file.Fd(), &saved); err != nil {
		if errors.Is(err, syscall.ENOTTY) {
			return noopStdinRestore, false, nil
		}
		return nil, false, fmt.Errorf("stdin terminal state probe failed: %w", err)
	}

	raw := saved
	makeTermiosRaw(&raw)
	if err := setTerminalAttrs(file.Fd(), &raw); err != nil {
		return nil, false, fmt.Errorf("stdin terminal raw mode setup failed: %w", err)
	}
	return func() error {
		if err := setTerminalAttrs(file.Fd(), &saved); err != nil {
			return fmt.Errorf("stdin terminal state restore failed: %w", err)
		}
		return nil
	}, true, nil
}

func makeTermiosRaw(t *syscall.Termios) {
	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	t.Cflag &^= syscall.CSIZE | syscall.PARENB
	t.Cflag |= syscall.CS8
	t.Cc[syscall.VMIN] = 1
	t.Cc[syscall.VTIME] = 0
}

func getTerminalAttrs(fd uintptr, t *syscall.Termios) error {
	return ioctlTermios(fd, tcgetsRequest, unsafe.Pointer(t))
}

func setTerminalAttrs(fd uintptr, t *syscall.Termios) error {
	return ioctlTermios(fd, tcsetsRequest, unsafe.Pointer(t))
}

func ioctlTermios(fd uintptr, request uintptr, t unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(t))
	if errno != 0 {
		return errno
	}
	return nil
}
