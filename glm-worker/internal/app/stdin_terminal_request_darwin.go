//go:build darwin

package app

import "syscall"

const (
	tcgetsRequest = syscall.TIOCGETA
	tcsetsRequest = syscall.TIOCSETA
)

const kernelStateLflagBits = 0x20000000
