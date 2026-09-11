package codexinstall

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const managedConfigKey = "background_terminal_max_timeout"

type configAssignment struct {
	Index int
	Line  string
	Value string
}

type configInstallPlan struct {
	Path      string
	Mode      os.FileMode
	Next      []byte
	Changed   bool
	Record    *managedConfigRecord
	Preserved bool
}

func buildConfigInstallPlan(repoRoot, codexDir string, state installState) (configInstallPlan, error) {
	managedPath := filepath.Join(repoRoot, "codex", "config-managed.toml")
	managedData, err := os.ReadFile(managedPath)
	if err != nil {
		return configInstallPlan{}, fmt.Errorf("read managed Codex config: %w", err)
	}
	managedAssignment, managedFound, err := findTopLevelAssignment(managedData, managedConfigKey)
	if err != nil {
		return configInstallPlan{}, fmt.Errorf("managed Codex config: %w", err)
	}
	path := filepath.Join(codexDir, "config.toml")
	data, mode, err := readOptionalFile(path)
	if err != nil {
		return configInstallPlan{}, err
	}
	current, currentFound, err := findTopLevelAssignment(data, managedConfigKey)
	if err != nil {
		return configInstallPlan{}, fmt.Errorf("installed Codex config: %w", err)
	}
	previous, previouslyOwned := state.Config[managedConfigKey]
	plan := configInstallPlan{Path: path, Mode: mode, Next: data}
	if previouslyOwned {
		return planPreviouslyOwnedConfig(plan, data, current, currentFound, managedAssignment, managedFound, previous)
	}
	return planUnownedConfig(plan, data, current, currentFound, managedAssignment, managedFound)
}

func planPreviouslyOwnedConfig(plan configInstallPlan, data []byte, current configAssignment, currentFound bool, managed configAssignment, managedFound bool, previous managedConfigRecord) (configInstallPlan, error) {
	if !managedFound {
		if !currentFound {
			return plan, nil
		}
		if digestBytes([]byte(current.Line)) != previous.LineSHA256 {
			plan.Preserved = true
			return plan, nil
		}
		plan.Next = replaceAssignmentLine(data, current.Index, "")
		plan.Changed = !strings.EqualFold(string(plan.Next), string(data)) || string(plan.Next) != string(data)
		return plan, nil
	}
	if !currentFound {
		return configInstallPlan{}, fmt.Errorf("managed Codex config key is missing; refusing silent recreation: %s", managedConfigKey)
	}
	if digestBytes([]byte(current.Line)) != previous.LineSHA256 {
		return configInstallPlan{}, fmt.Errorf("managed Codex config key was modified after install; refusing to overwrite: %s", managedConfigKey)
	}
	nextLine := assignmentLine(managedConfigKey, managed.Value, lineEnding(current.Line))
	plan.Next = replaceAssignmentLine(data, current.Index, nextLine)
	plan.Changed = string(plan.Next) != string(data)
	plan.Record = &managedConfigRecord{Value: managed.Value, LineSHA256: digestBytes([]byte(nextLine))}
	return plan, nil
}

func planUnownedConfig(plan configInstallPlan, data []byte, current configAssignment, currentFound bool, managed configAssignment, managedFound bool) (configInstallPlan, error) {
	if !managedFound {
		return plan, nil
	}
	if currentFound {
		if current.Value != managed.Value {
			return configInstallPlan{}, fmt.Errorf("refusing to overwrite user-owned Codex config key %s: current=%s managed=%s", managedConfigKey, current.Value, managed.Value)
		}
		return plan, nil
	}
	nextLine := assignmentLine(managedConfigKey, managed.Value, "\n")
	plan.Next = append([]byte(nextLine), data...)
	plan.Changed = true
	plan.Record = &managedConfigRecord{Value: managed.Value, LineSHA256: digestBytes([]byte(nextLine))}
	return plan, nil
}

func applyConfigInstallPlan(plan configInstallPlan, output func(string, ...any)) error {
	if plan.Preserved {
		output("preserved user-modified retired Codex config key: %s\n", managedConfigKey)
	}
	if !plan.Changed {
		return nil
	}
	if err := writeAtomic(plan.Path, plan.Next, plan.Mode); err != nil {
		return fmt.Errorf("write Codex config: %w", err)
	}
	output("updated: %s\n", plan.Path)
	return nil
}

func readOptionalFile(path string) ([]byte, os.FileMode, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0o644, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("stat Codex config: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("Codex config is not a regular file: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, fmt.Errorf("read Codex config: %w", err)
	}
	return data, info.Mode().Perm(), nil
}

func findTopLevelAssignment(data []byte, key string) (configAssignment, bool, error) {
	lines := strings.SplitAfter(string(data), "\n")
	found := false
	assignment := configAssignment{}
	for index, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\n"))
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			break
		}
		left, right, ok := strings.Cut(trimmed, "=")
		if !ok || strings.TrimSpace(left) != key {
			continue
		}
		if found {
			return configAssignment{}, false, fmt.Errorf("duplicate top-level assignment %q", key)
		}
		value := strings.TrimSpace(strings.SplitN(right, "#", 2)[0])
		if value == "" {
			return configAssignment{}, false, fmt.Errorf("empty top-level assignment %q", key)
		}
		assignment = configAssignment{Index: index, Line: line, Value: value}
		found = true
	}
	return assignment, found, nil
}

func replaceAssignmentLine(data []byte, index int, replacement string) []byte {
	lines := strings.SplitAfter(string(data), "\n")
	if replacement == "" {
		lines = append(lines[:index], lines[index+1:]...)
	} else {
		lines[index] = replacement
	}
	return []byte(strings.Join(lines, ""))
}

func assignmentLine(key, value, ending string) string {
	if ending == "" {
		ending = "\n"
	}
	return key + " = " + value + ending
}

func lineEnding(line string) string {
	switch {
	case strings.HasSuffix(line, "\r\n"):
		return "\r\n"
	case strings.HasSuffix(line, "\n"):
		return "\n"
	default:
		return ""
	}
}
