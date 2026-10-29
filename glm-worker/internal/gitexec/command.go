package gitexec

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var localVariables = sync.OnceValues(func() (map[string]bool, error) {
	output, err := exec.Command("git", "rev-parse", "--local-env-vars").Output()
	if err != nil {
		return nil, fmt.Errorf("resolve Git local environment: %w", err)
	}
	names := make(map[string]bool)
	for _, name := range strings.Fields(string(output)) {
		names[name] = true
	}
	return names, nil
})

func Command(gitPath, repo string, args ...string) *exec.Cmd {
	command := exec.Command(gitPath, append([]string{"-C", repo}, args...)...)
	origin, err := os.Getwd()
	if err == nil {
		command.Env, err = environment(origin, repo, os.Environ())
	}
	if err != nil {
		command.Err = err
	}
	return command
}

func environment(origin, target string, inherited []string) ([]string, error) {
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
	names, err := localVariables()
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
