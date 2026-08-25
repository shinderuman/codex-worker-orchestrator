//go:build linux

package app

import "syscall"

const (
	tcgetsRequest = syscall.TCGETS
	tcsetsRequest = syscall.TCSETS
)

const kernelStateLflagBits = 0
