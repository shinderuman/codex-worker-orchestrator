//go:build darwin

package app

import "syscall"

const (
	tcgetsRequest = syscall.TIOCGETA
	tcsetsRequest = syscall.TIOCSETA
)

// kernelStateLflagBitsはdarwin kernelがraw modeからcanonicalへの復元時に自律的に立てる
// PENDIN(0x20000000, 再echo保留状態)。`stty -g`保存→`stty raw -echo`→復元でも同じ差分が
// 出るkernel管理状態bitのため、復元判定から除外する。syscall packageはこの定数をexportしない。
const kernelStateLflagBits = 0x20000000
