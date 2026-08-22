package app

import (
	"fmt"
	"io"
	"os"
	"runtime"
)

// enterStdinRawModeはstdinがTTY/PTYのときだけtermiosをraw/noecho相当へ変更し、
// 変更前stateへ戻す復元関数とraw適用有無を返す。pipe/fileなどの非terminal stdinは
// applied=falseのno-op復元を返し、probe失敗(ENOTTY以外)とraw化未実装環境のterminal入力は
// payload読み取り前にfail closedする。applied=trueはREADY markerを出す契約条件になる。
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

// stdinUnsupportedPlatformTerminalはtermios相当のterminal制御を実装しない環境向けの境界。
// terminalらしきstdinはraw化できずecho/canonical由来のpayload完全性を保証できないため、
// payload読み取り・state変更前に明示的にfail closedし、pipe/fileだけをno-op復元で通す。
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
