//go:build !windows

package execfile

import (
	"os"
	"syscall"
)

func openStreamFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
