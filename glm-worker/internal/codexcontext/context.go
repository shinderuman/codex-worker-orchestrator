package codexcontext

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Result struct {
	Status            string `json:"status"`
	Action            string `json:"action"`
	RepoRoot          string `json:"repo_root"`
	ConfigPath        string `json:"config_path"`
	GitExcluded       bool   `json:"git_excluded"`
	RequiresNewThread bool   `json:"requires_new_thread"`
	DesktopRestart    bool   `json:"desktop_restart_required"`
	Detail            string `json:"detail,omitempty"`
}

const (
	ProjectConfigRelativePath = ".codex/config.toml"
	managedMarker             = "# managed-by: codex-worker-orchestrator glm-codex-context v1"
	excludeMarker             = "# codex-worker-orchestrator glm-codex-context v1"
	excludePattern            = "/.codex/config.toml"
)

var managedConfig = []byte(managedMarker + `
# This local project profile reduces Codex Desktop context for glm-worker tasks.
# Start a new Codex thread after enabling or disabling it.

include_apps_instructions = false
include_collaboration_mode_instructions = false

[skills]
include_instructions = false

[features]
apps = false
plugins = false

[features.code_mode]
default_exec_yield_time_ms = 21600000

[features.multi_agent_v2]
root_agent_usage_hint_text = ""
multi_agent_mode_hint_text = ""
`)

func ManagedConfigContent() []byte {
	return append([]byte(nil), managedConfig...)
}

func IsManagedConfig(content []byte) bool {
	return bytes.Equal(content, managedConfig)
}

func Run(args []string, stdout io.Writer) error {
	if len(args) < 1 || len(args) > 2 {
		return errors.New("usage: glm-codex-context enable|disable|status [repository]")
	}
	action := args[0]
	if action != "enable" && action != "disable" && action != "status" {
		return fmt.Errorf("unknown action %q (expected enable, disable, or status)", action)
	}
	repo := "."
	if len(args) == 2 {
		repo = args[1]
	}
	root, err := repositoryRoot(repo)
	if err != nil {
		return err
	}
	var result Result
	switch action {
	case "enable":
		result, err = enable(root)
	case "disable":
		result, err = disable(root)
	default:
		result, err = status(root)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

func repositoryRoot(repo string) (string, error) {
	command := exec.Command("git", "-C", repo, "rev-parse", "--show-toplevel")
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("resolve target repository: %w", err)
	}
	root := strings.TrimSpace(string(output))
	if root == "" {
		return "", errors.New("resolve target repository: empty git root")
	}
	return filepath.Clean(root), nil
}

func enable(root string) (Result, error) {
	configPath := filepath.Join(root, filepath.FromSlash(ProjectConfigRelativePath))
	created := false
	content, err := os.ReadFile(configPath)
	switch {
	case err == nil:
		if !IsManagedConfig(content) {
			return Result{}, fmt.Errorf("refusing to overwrite existing %s; merge the lean Codex settings explicitly or disable/remove the existing project config first", ProjectConfigRelativePath)
		}
	case errors.Is(err, os.ErrNotExist):
		if err := writeManagedConfig(configPath); err != nil {
			return Result{}, err
		}
		created = true
	default:
		return Result{}, fmt.Errorf("read %s: %w", ProjectConfigRelativePath, err)
	}
	excluded, err := ensureGitExclude(root)
	if err != nil {
		if created {
			if removeErr := removeManagedConfig(configPath); removeErr != nil {
				return Result{}, errors.Join(
					fmt.Errorf("configure local Git exclude: %w", err),
					fmt.Errorf("rollback project config: %w", removeErr),
				)
			}
		}
		return Result{}, fmt.Errorf("configure local Git exclude: %w", err)
	}
	return Result{
		Status:            "enabled",
		Action:            "enable",
		RepoRoot:          root,
		ConfigPath:        configPath,
		GitExcluded:       excluded,
		RequiresNewThread: true,
		DesktopRestart:    false,
		Detail:            "start a new Codex thread in this repository; unrelated repositories keep their normal Codex context",
	}, nil
}

func disable(root string) (Result, error) {
	configPath := filepath.Join(root, filepath.FromSlash(ProjectConfigRelativePath))
	content, err := os.ReadFile(configPath)
	switch {
	case err == nil:
		if !IsManagedConfig(content) {
			return Result{}, fmt.Errorf("refusing to remove existing %s because it is not owned by glm-codex-context", ProjectConfigRelativePath)
		}
		if err := removeManagedConfig(configPath); err != nil {
			return Result{}, err
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return Result{}, fmt.Errorf("read %s: %w", ProjectConfigRelativePath, err)
	}
	if err := removeGitExclude(root); err != nil {
		return Result{}, fmt.Errorf("remove local Git exclude: %w", err)
	}
	excluded, err := gitExcluded(root)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Status:            "disabled",
		Action:            "disable",
		RepoRoot:          root,
		ConfigPath:        configPath,
		GitExcluded:       excluded,
		RequiresNewThread: true,
		DesktopRestart:    false,
		Detail:            "start a new Codex thread to return to normal inherited Skills/Plugins/Apps/collaboration behavior",
	}, nil
}

func status(root string) (Result, error) {
	configPath := filepath.Join(root, filepath.FromSlash(ProjectConfigRelativePath))
	content, err := os.ReadFile(configPath)
	state := "disabled"
	detail := "no glm-codex-context managed project config"
	switch {
	case err == nil && IsManagedConfig(content):
		state = "enabled"
		detail = "lean project context is configured; setting changes apply to new Codex threads"
	case err == nil:
		state = "conflict"
		detail = "project config exists but is not owned by glm-codex-context"
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return Result{}, fmt.Errorf("read %s: %w", ProjectConfigRelativePath, err)
	}
	excluded, err := gitExcluded(root)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Status:            state,
		Action:            "status",
		RepoRoot:          root,
		ConfigPath:        configPath,
		GitExcluded:       excluded,
		RequiresNewThread: false,
		DesktopRestart:    false,
		Detail:            detail,
	}, nil
}

func writeManagedConfig(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create .codex directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config.toml.tmp-*")
	if err != nil {
		return fmt.Errorf("create project config temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(managedConfig); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write project config: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod project config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close project config: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("install project config: %w", err)
	}
	return nil
}

func removeManagedConfig(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove project config: %w", err)
	}
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
	return nil
}

func gitExcludePath(root string) (string, error) {
	command := exec.Command("git", "-C", root, "rev-parse", "--git-path", "info/exclude")
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("resolve git info/exclude: %w", err)
	}
	path := strings.TrimSpace(string(output))
	if path == "" {
		return "", errors.New("resolve git info/exclude: empty path")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	return filepath.Clean(path), nil
}

func ensureGitExclude(root string) (bool, error) {
	excluded, err := gitExcluded(root)
	if err != nil {
		return false, err
	}
	if excluded {
		return true, nil
	}
	path, err := gitExcludePath(root)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("create Git info directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return false, fmt.Errorf("open local Git exclude: %w", err)
	}
	if _, err := fmt.Fprintf(file, "\n%s\n%s\n", excludeMarker, excludePattern); err != nil {
		_ = file.Close()
		return false, fmt.Errorf("write local Git exclude: %w", err)
	}
	if err := file.Close(); err != nil {
		return false, fmt.Errorf("close local Git exclude: %w", err)
	}
	return gitExcluded(root)
}

func removeGitExclude(root string) error {
	path, err := gitExcludePath(root)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read local Git exclude: %w", err)
	}
	lines := strings.Split(string(content), "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == excludeMarker && i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == excludePattern {
			i++
			continue
		}
		out = append(out, lines[i])
	}
	next := strings.Join(out, "\n")
	if next == string(content) {
		return nil
	}
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		return fmt.Errorf("write local Git exclude: %w", err)
	}
	return nil
}

func gitExcluded(root string) (bool, error) {
	command := exec.Command("git", "-C", root, "check-ignore", "-q", "--", ProjectConfigRelativePath)
	err := command.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git check-ignore %s: %w", ProjectConfigRelativePath, err)
}
