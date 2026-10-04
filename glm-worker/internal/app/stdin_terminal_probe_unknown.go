//go:build !darwin && !linux && !windows && !dragonfly && !freebsd && !netbsd && !openbsd

package app

import "os"

func unsupportedPlatformStdinIsTerminal(_ *os.File) (bool, error) {
	return false, nil
}
