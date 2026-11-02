package gitexec

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var localVariableCache = struct {
	sync.RWMutex
	values map[string]map[string]bool
}{values: make(map[string]map[string]bool)}

func localVariables(gitPath string) (map[string]bool, error) {
	localVariableCache.RLock()
	names, cached := localVariableCache.values[gitPath]
	localVariableCache.RUnlock()
	if cached {
		return names, nil
	}
	output, err := exec.Command(gitPath, "rev-parse", "--local-env-vars").Output()
	if err != nil {
		return nil, fmt.Errorf("resolve Git local environment: %w", err)
	}
	names = make(map[string]bool)
	for _, name := range strings.Fields(string(output)) {
		names[name] = true
	}
	localVariableCache.Lock()
	if prior, exists := localVariableCache.values[gitPath]; exists {
		names = prior
	} else {
		localVariableCache.values[gitPath] = names
	}
	localVariableCache.Unlock()
	return names, nil
}

func Command(gitPath, repo string, args ...string) *exec.Cmd {
	return commandForTarget(gitPath, repo, append([]string{"-C", repo}, args...)...)
}

func CommandGitDir(gitPath, gitDir string, args ...string) *exec.Cmd {
	return commandForTarget(gitPath, gitDir, append([]string{"--git-dir", gitDir}, args...)...)
}

func commandForTarget(gitPath, target string, args ...string) *exec.Cmd {
	command := exec.Command(gitPath, args...)
	origin, err := os.Getwd()
	if err == nil {
		command.Env, err = environment(gitPath, origin, target, os.Environ())
	}
	if err != nil {
		command.Err = err
	}
	return command
}

func environment(gitPath, origin, target string, inherited []string) ([]string, error) {
	source, err := worktreeRoot(origin)
	if err != nil {
		return nil, err
	}
	destination, err := worktreeRoot(target)
	if err != nil {
		return nil, err
	}
	if source == destination {
		return append([]string(nil), inherited...), nil
	}
	return withoutLocalEnvironment(gitPath, inherited)
}

func WithoutLocalEnvironment(inherited []string) ([]string, error) {
	return withoutLocalEnvironment("git", inherited)
}

func withoutLocalEnvironment(gitPath string, inherited []string) ([]string, error) {
	names, err := localVariables(gitPath)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(inherited))
	for _, item := range inherited {
		name, _, _ := strings.Cut(item, "=")
		if !names[name] {
			result = append(result, item)
		}
	}
	return result, nil
}

func worktreeRoot(directory string) (string, error) {
	directory, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return "", err
	}
	original := directory
	for {
		_, err := os.Lstat(filepath.Join(directory, ".git"))
		if err == nil {
			return directory, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return original, nil
		}
		directory = parent
	}
}
