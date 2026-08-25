package app

import (
	"fmt"
	"io"
	"os"
	"runtime"
)

func enterStdinRawMode(stdin io.Reader) (restore func() error, applied bool, err error) {
	file, ok := stdin.(*os.File)
	if !ok {
		return noopStdinRestore, false, nil
	}
	return setStdinFileRaw(file)
}

func noopStdinRestore() error {
	return nil
}

func stdinUnsupportedPlatformTerminal(file *os.File) (restore func() error, applied bool, err error) {
	info, statErr := file.Stat()
	if statErr != nil {
		return nil, false, fmt.Errorf("stdin state probe failed on GOOS=%s: %w", runtime.GOOS, statErr)
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return noopStdinRestore, false, nil
	}
	return nil, false, fmt.Errorf("stdin appears to be a terminal, but raw mode is not implemented on this platform (GOOS=%s); feed the payload through a pipe or file", runtime.GOOS)
}
