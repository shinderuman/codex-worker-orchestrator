package taskdiff

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

func RuntimeChangedPaths(paths []string) []string {
	runtimePaths := make([]string, 0, len(paths))
	for _, path := range paths {
		if RuntimeInstallPath(path) {
			runtimePaths = append(runtimePaths, filepath.ToSlash(filepath.Clean(path)))
		}
	}
	sort.Strings(runtimePaths)
	return runtimePaths
}

func RuntimeSourceDigest(repoRoot string, paths []string) (string, error) {
	hash := sha256.New()
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(path)))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				_, _ = fmt.Fprintf(hash, "%s\x00missing\n", path)
				continue
			}
			return "", fmt.Errorf("runtime source %s: %w", path, err)
		}
		content := sha256.Sum256(data)
		_, _ = fmt.Fprintf(hash, "%s\x00%s\n", path, hex.EncodeToString(content[:]))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
