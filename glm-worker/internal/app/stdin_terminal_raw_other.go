//go:build !darwin && !linux

package app

import "os"

// setStdinFileRawはtermios相当のterminal制御を実装しない環境の境界。
// terminalらしきstdinはsilent no-opにせず読み取り前に明示的にfail closedする。
func setStdinFileRaw(file *os.File) (restore func() error, err error) {
	return stdinUnsupportedPlatformTerminal(file)
}
