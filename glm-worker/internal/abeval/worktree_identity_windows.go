//go:build windows

package abeval

import (
	"fmt"
	"os"
	"syscall"
)

func directoryFileIdentity(path string, _ os.FileInfo) (uint64, uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, fmt.Errorf("worktree identityのdirectory open: %w", err)
	}
	defer func() { _ = file.Close() }()
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(file.Fd()), &info); err != nil {
		return 0, 0, fmt.Errorf("worktree identityのdirectory handle情報: %w", err)
	}
	return uint64(info.VolumeSerialNumber), uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow), nil
}
