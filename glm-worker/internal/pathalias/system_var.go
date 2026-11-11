package pathalias

import (
	"os"
	"path/filepath"
	"runtime"
)

func IsStandardDarwinVar(path string) bool {
	if runtime.GOOS != "darwin" || path != "/var" {
		return false
	}
	target, err := os.Readlink(path)
	if err != nil || (target != "private/var" && target != "/private/var") {
		return false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != "/private/var" {
		return false
	}
	for _, component := range []string{"/private", "/private/var"} {
		info, err := os.Lstat(component)
		if err != nil || !info.IsDir() {
			return false
		}
	}
	return true
}
