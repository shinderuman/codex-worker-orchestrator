package runner

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type gitBashSandboxPolicy struct {
	allowWrite []string
	denyWrite  []string
}

func (g *gitAuthorityGuard) bashSandboxPolicy() *gitBashSandboxPolicy {
	if g == nil || !g.before.active {
		return nil
	}
	return &gitBashSandboxPolicy{
		allowWrite: []string{g.workerTempDir},
		denyWrite:  append([]string(nil), g.metadataPaths...),
	}
}

func (p *gitBashSandboxPolicy) settings() map[string]any {
	return map[string]any{
		"enabled":                  true,
		"failIfUnavailable":        true,
		"autoAllowBashIfSandboxed": true,
		"allowUnsandboxedCommands": false,
		"excludedCommands":         []string{},
		"filesystem": map[string]any{
			"allowWrite": sandboxAbsolutePaths(p.allowWrite),
			"denyWrite":  sandboxAbsolutePaths(p.denyWrite),
		},
		"network": map[string]any{
			"allowedDomains":      []string{},
			"strictAllowlist":     true,
			"allowUnixSockets":    []string{},
			"allowAllUnixSockets": false,
			"allowLocalBinding":   false,
		},
	}
}

func resolveGitAuthorityMetadataPaths(realGit, repoRoot string) ([]string, error) {
	gitDir, err := gitAuthorityPathOutput(realGit, repoRoot, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, err
	}
	commonDir, err := gitAuthorityPathOutput(realGit, repoRoot, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	gitMarker, err := filepath.Abs(filepath.Join(repoRoot, ".git"))
	if err != nil {
		return nil, err
	}
	paths := uniqueSortedStrings([]string{gitDir, commonDir, filepath.Clean(gitMarker)})
	if len(paths) == 0 {
		return nil, fmt.Errorf("git metadata pathが空です")
	}
	return paths, nil
}

func gitAuthorityPathOutput(realGit, repoRoot string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", repoRoot}, args...)
	output, err := exec.Command(realGit, commandArgs...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	path := strings.TrimSpace(string(output))
	if path == "" {
		return "", fmt.Errorf("git %s returned empty path", strings.Join(args, " "))
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoRoot, path)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

func sandboxAbsolutePaths(paths []string) []string {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		result = append(result, filepath.ToSlash(filepath.Clean(path)))
	}
	sort.Strings(result)
	return result
}
