package codexcontext

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	ProjectAgentsOverrideRelativePath = "AGENTS.override.md"
	projectAgentsManagedMarker        = "<!-- managed-by: codex-worker-orchestrator glm-codex-context v1 -->"
	projectAgentsExcludeMarker        = "# codex-worker-orchestrator glm-codex-context agents v1"
	projectAgentsExcludePattern       = "/AGENTS.override.md"
)

func managedProjectAgentsContent() ([]byte, error) {
	codexDir := strings.TrimSpace(os.Getenv("CODEX_CONFIG_DIR"))
	if codexDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve Codex home: %w", err)
		}
		codexDir = filepath.Join(home, ".codex")
	}
	instruction := filepath.ToSlash(filepath.Join(codexDir, "instructions", "codex-worker-orchestrator.md"))
	content := projectAgentsManagedMarker + `
# glm-worker project-scoped Codex bootstrap

- このfileはglm-codex-contextがこのrepositoryだけへ適用するtool-owned bootstrapであり、user-global AGENTSではない。
- repository rootにAGENTS.mdが存在する場合は、そのproject instructionも読む。このoverrideはrepository固有authorityを置換するための第二正本ではない。
- glm-worker / glm-parent-actionを利用する親Codexは、実行前にtool-owned parent instruction ` + "`" + instruction + "`" + ` を読み、そのfileをparent/tool rule本文の正本として扱う。
`
	return []byte(content), nil
}

func projectAgentsOverrideState(root string) (string, error) {
	path := filepath.Join(root, ProjectAgentsOverrideRelativePath)
	content, err := os.ReadFile(path)
	switch {
	case err == nil && bytes.HasPrefix(content, []byte(projectAgentsManagedMarker+"\n")):
		return "enabled", nil
	case err == nil:
		return "conflict", nil
	case errors.Is(err, os.ErrNotExist):
		return "disabled", nil
	default:
		return "", fmt.Errorf("read %s: %w", ProjectAgentsOverrideRelativePath, err)
	}
}

func enableProjectAgentsOverride(root string) (bool, error) {
	state, err := projectAgentsOverrideState(root)
	if err != nil {
		return false, err
	}
	if state == "conflict" {
		return false, fmt.Errorf("refusing to overwrite existing %s", ProjectAgentsOverrideRelativePath)
	}
	if tracked, err := projectAgentsOverrideTracked(root); err != nil {
		return false, err
	} else if tracked {
		return false, fmt.Errorf("refusing to manage tracked %s", ProjectAgentsOverrideRelativePath)
	}
	content, err := managedProjectAgentsContent()
	if err != nil {
		return false, err
	}
	path := filepath.Join(root, ProjectAgentsOverrideRelativePath)
	created := state == "disabled"
	if current, readErr := os.ReadFile(path); readErr == nil && bytes.Equal(current, content) {
		return false, ensureProjectAgentsExclude(root)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", ProjectAgentsOverrideRelativePath, err)
	}
	if err := ensureProjectAgentsExclude(root); err != nil {
		if created {
			_ = os.Remove(path)
		}
		return false, err
	}
	return created, nil
}

func validateProjectAgentsDisable(root string) error {
	state, err := projectAgentsOverrideState(root)
	if err != nil {
		return err
	}
	if state == "conflict" {
		return fmt.Errorf("refusing to remove existing %s because it is not owned by glm-codex-context", ProjectAgentsOverrideRelativePath)
	}
	return nil
}

func disableProjectAgentsOverride(root string) error {
	if err := validateProjectAgentsDisable(root); err != nil {
		return err
	}
	path := filepath.Join(root, ProjectAgentsOverrideRelativePath)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", ProjectAgentsOverrideRelativePath, err)
	}
	return removeProjectAgentsExclude(root)
}

func projectAgentsOverrideTracked(root string) (bool, error) {
	command := exec.Command("git", "-C", root, "ls-files", "--error-unmatch", "--", ProjectAgentsOverrideRelativePath)
	err := command.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git ls-files %s: %w", ProjectAgentsOverrideRelativePath, err)
}

func ensureProjectAgentsExclude(root string) error {
	excluded, err := projectAgentsIgnored(root)
	if err != nil {
		return err
	}
	if excluded {
		return nil
	}
	path, err := gitExcludePath(root)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open local Git exclude: %w", err)
	}
	if _, err := fmt.Fprintf(file, "\n%s\n%s\n", projectAgentsExcludeMarker, projectAgentsExcludePattern); err != nil {
		_ = file.Close()
		return fmt.Errorf("write local Git exclude: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close local Git exclude: %w", err)
	}
	return nil
}

func removeProjectAgentsExclude(root string) error {
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
	for index := 0; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == projectAgentsExcludeMarker && index+1 < len(lines) && strings.TrimSpace(lines[index+1]) == projectAgentsExcludePattern {
			index++
			continue
		}
		out = append(out, lines[index])
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

func projectAgentsIgnored(root string) (bool, error) {
	command := exec.Command("git", "-C", root, "check-ignore", "-q", "--", ProjectAgentsOverrideRelativePath)
	err := command.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git check-ignore %s: %w", ProjectAgentsOverrideRelativePath, err)
}
