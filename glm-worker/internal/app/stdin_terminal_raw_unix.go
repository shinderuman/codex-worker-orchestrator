//go:build darwin || linux

package app

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// setStdinFileRawはstdin fdがterminalのときだけtermiosをcfmakeraw相当へ変更する。
// termios取得ioctlはENOTTY(fdがterminalでない)のときだけpipe/fileとしてno-op復元を返し、
// その他の失敗はTTYでないと黙って解釈せずfail closedする。
// 復元は呼び出し元の責務で、読み取り成功・不足・sha256不一致を含む全pathで実行する。
func setStdinFileRaw(file *os.File) (restore func() error, err error) {
	var saved syscall.Termios
	if err := getTerminalAttrs(file.Fd(), &saved); err != nil {
		if errors.Is(err, syscall.ENOTTY) {
			return noopStdinRestore, nil
		}
		return nil, fmt.Errorf("stdin terminal state probe failed: %w", err)
	}

	raw := saved
	makeTermiosRaw(&raw)
	if err := setTerminalAttrs(file.Fd(), &raw); err != nil {
		return nil, fmt.Errorf("stdin terminal raw mode setup failed: %w", err)
	}
	return func() error {
		if err := setTerminalAttrs(file.Fd(), &saved); err != nil {
			return fmt.Errorf("stdin terminal state restore failed: %w", err)
		}
		return nil
	}, nil
}

// makeTermiosRawは`stty raw -echo`相当の設定。input/output processing・canonical・
// signals・flow controlをまとめて無効化し、CR/NL置換(ICRNL/INLCR)・制御byteの信号化(ISIG)・
// flow control(IXON)・stdoutのPTY側加工(OPOST)を防ぐ。
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
