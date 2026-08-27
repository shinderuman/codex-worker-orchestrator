package harnesslint

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const repositoryModule = "module github.com/shinderuman/codex-worker-orchestrator/glm-worker"

func AppliesTo(root string) bool {
	data, err := os.ReadFile(filepath.Join(root, "glm-worker", "go.mod"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == repositoryModule {
			return true
		}
	}
	return false
}

func ValidateRoot(root string) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("repository root is not a directory")
	}
	return nil
}

func repositoryPaths(root string) ([]string, error) {
	command := exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	data, err := command.Output()
	if err == nil {
		parts := bytes.Split(data, []byte{0})
		paths := make([]string, 0, len(parts))
		for _, part := range parts {
			if len(part) > 0 {
				paths = append(paths, filepath.ToSlash(string(part)))
			}
		}
		sort.Strings(paths)
		return paths, nil
	}
	return walkRepository(root)
}

func walkRepository(root string) ([]string, error) {
	paths := []string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk repository: %w", err)
	}
	sort.Strings(paths)
	return paths, nil
}

func readRegularFile(root, path string) ([]byte, error) {
	absolute := filepath.Join(root, filepath.FromSlash(path))
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return os.ReadFile(absolute)
}

func writeRegularFile(root, path string, data []byte) error {
	absolute := filepath.Join(root, filepath.FromSlash(path))
	info, err := os.Lstat(absolute)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return os.WriteFile(absolute, data, info.Mode().Perm())
}

func goFiles(paths []string) []string {
	return pathsMatching(paths, func(path string) bool {
		return strings.HasSuffix(path, ".go")
	})
}

func shellFiles(paths []string) []string {
	return pathsMatching(paths, func(path string) bool {
		base := filepath.Base(path)
		return strings.HasSuffix(path, ".sh") || base == "commentlint" || base == "harnesslint" || path == ".githooks/post-merge"
	})
}

func moduleDirs(paths []string) []string {
	result := []string{}
	for _, path := range paths {
		if filepath.Base(path) != "go.mod" {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(path))
		if dir == "." {
			dir = ""
		}
		result = append(result, dir)
	}
	sort.Strings(result)
	return result
}

func pathsMatching(paths []string, keep func(string) bool) []string {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if keep(path) {
			result = append(result, path)
		}
	}
	return result
}
