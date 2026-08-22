//go:build linux

package app

import "syscall"

const (
	tcgetsRequest = syscall.TCGETS
	tcsetsRequest = syscall.TCSETS
)

// kernelStateLflagBitsはdarwinのPENDIN相当。linuxのtermios復元でkernelが立てる状態bitはない。
const kernelStateLflagBits = 0
