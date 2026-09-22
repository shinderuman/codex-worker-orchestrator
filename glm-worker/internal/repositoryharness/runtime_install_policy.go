package repositoryharness

import (
	"path/filepath"
	"strings"
)

func RuntimeInstallPath(path string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	switch path {
	case "install.sh", "quality-tools.yml", "claude/settings-managed.json":
		return true
	}
	if strings.HasPrefix(path, "codex/") {
		return true
	}
	if !strings.HasPrefix(path, "glm-worker/") || strings.HasSuffix(path, "_test.go") {
		return false
	}
	return path == "glm-worker/go.mod" || path == "glm-worker/go.sum" || strings.HasSuffix(path, ".go")
}
