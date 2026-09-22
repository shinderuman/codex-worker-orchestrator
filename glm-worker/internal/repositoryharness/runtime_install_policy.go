package repositoryharness

import (
	"path/filepath"
	"strings"
)

// RuntimeInstallPath reports whether a repository path belongs to the installed
// runtime surface for this repository. The path inventory is repository policy;
// callers should keep diff enumeration and digest mechanics separate.
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
