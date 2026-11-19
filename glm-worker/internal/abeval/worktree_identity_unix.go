//go:build !windows

package abeval

import (
	"fmt"
	"os"
	"syscall"
)

func directoryFileIdentity(path string, info os.FileInfo) (uint64, uint64, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Ino == 0 {
		return 0, 0, fmt.Errorf("worktree identityのdevice/inodeを取得できません: %s", path)
	}
	return uint64(stat.Dev), uint64(stat.Ino), nil
}
