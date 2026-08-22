package app

import (
	"fmt"
	"io"
	"os"
	"runtime"
)

// enterStdinRawModeはstdinがTTY/PTYのときだけtermiosをraw/noecho相当へ変更し、
// 変更前stateへ戻す復元関数を返す。pipe/fileなどの非terminal stdinはno-op復元を返し、
// probe失敗(ENOTTY以外)とraw化未実装環境のterminal入力はpayload読み取り前にfail closedする。
func enterStdinRawMode(stdin io.Reader) (restore func() error, err error) {
	file, ok := stdin.(*os.File)
	if !ok {
		return noopStdinRestore, nil
	}
	return setStdinFileRaw(file)
}

func noopStdinRestore() error {
	return nil
}

// stdinUnsupportedPlatformTerminalはtermios相当のterminal制御を実装しない環境向けの境界。
// terminalらしきstdinはraw化できずecho/canonical由来のpayload完全性を保証できないため、
// payload読み取り・state変更前に明示的にfail closedし、pipe/fileだけをno-op復元で通す。
func stdinUnsupportedPlatformTerminal(file *os.File) (restore func() error, err error) {
	info, statErr := file.Stat()
	if statErr != nil {
		return nil, fmt.Errorf("stdin state probe failed on GOOS=%s: %w", runtime.GOOS, statErr)
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return noopStdinRestore, nil
	}
	return nil, fmt.Errorf("stdin appears to be a terminal, but raw mode is not implemented on this platform (GOOS=%s); feed the payload through a pipe or file", runtime.GOOS)
}
