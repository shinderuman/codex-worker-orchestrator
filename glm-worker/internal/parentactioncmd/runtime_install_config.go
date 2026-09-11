package parentactioncmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/claudeoverride"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func verifyRuntimeMergedConfigFiles(cfg config.AppConfig, paths []string) error {
	for _, path := range paths {
		switch path {
		case "codex/config-managed.toml":
			if err := verifyInstalledCodexManagedConfig(cfg); err != nil {
				return err
			}
		case "claude/settings-managed.json":
			if err := verifyInstalledClaudeManagedSettings(cfg); err != nil {
				return err
			}
		}
	}
	return nil
}

func verifyInstalledCodexManagedConfig(cfg config.AppConfig) error {
	managed, err := os.ReadFile(filepath.Join(cfg.RepoRoot, "codex", "config-managed.toml"))
	if err != nil {
		return fmt.Errorf("read managed Codex config: %w", err)
	}
	installed, err := os.ReadFile(filepath.Join(cfg.CodexConfigDir, "config.toml"))
	if err != nil {
		return fmt.Errorf("read installed Codex config: %w", err)
	}
	managedValues, err := topLevelTOMLAssignments(managed)
	if err != nil {
		return fmt.Errorf("managed Codex config: %w", err)
	}
	installedValues, err := topLevelTOMLAssignments(installed)
	if err != nil {
		return fmt.Errorf("installed Codex config: %w", err)
	}
	for key, expected := range managedValues {
		if actual, ok := installedValues[key]; !ok || actual != expected {
			return fmt.Errorf("installed Codex config does not match managed value: %s", key)
		}
	}
	return nil
}

func topLevelTOMLAssignments(data []byte) (map[string]string, error) {
	values := map[string]string{}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			break
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			return nil, fmt.Errorf("invalid top-level assignment %q", line)
		}
		if topLevelTOMLValueContinues(value) {
			return nil, fmt.Errorf("multiline top-level assignment is not supported: %q", key)
		}
		if _, duplicate := values[key]; duplicate {
			return nil, fmt.Errorf("duplicate top-level assignment %q", key)
		}
		values[key] = value
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("no managed top-level assignments")
	}
	return values, nil
}

func topLevelTOMLValueContinues(value string) bool {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, `"""`) || strings.HasPrefix(value, "'''") {
		return true
	}
	var quote byte
	escaped := false
	squareDepth := 0
	curlyDepth := 0
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if quote != 0 {
			if quote == '"' && escaped {
				escaped = false
				continue
			}
			if quote == '"' && ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '"', '\'':
			quote = ch
		case '#':
			return squareDepth != 0 || curlyDepth != 0
		case '[':
			squareDepth++
		case ']':
			squareDepth--
		case '{':
			curlyDepth++
		case '}':
			curlyDepth--
		}
		if squareDepth < 0 || curlyDepth < 0 {
			return true
		}
	}
	return quote != 0 || squareDepth != 0 || curlyDepth != 0
}

func verifyInstalledClaudeManagedSettings(cfg config.AppConfig) error {
	managed, err := readJSONObject(filepath.Join(cfg.RepoRoot, "claude", "settings-managed.json"))
	if err != nil {
		return fmt.Errorf("read managed Claude settings: %w", err)
	}
	settingsPath := os.Getenv("CLAUDE_SETTINGS_FILE")
	if settingsPath == "" {
		settingsPath = filepath.Join(cfg.ClaudeConfigDir, "settings.json")
	}
	installed, err := readJSONObject(settingsPath)
	if err != nil {
		return fmt.Errorf("read installed Claude settings: %w", err)
	}
	override, err := claudeoverride.Load(cfg.ClaudeSettingsOverride)
	if err != nil {
		return fmt.Errorf("read Claude settings override: %w", err)
	}
	if err := verifyManagedJSONSubset(installed, managed, override); err != nil {
		return fmt.Errorf("installed Claude settings do not match managed values: %w", err)
	}
	return nil
}

func readJSONObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, fmt.Errorf("top-level JSON value must be an object")
	}
	return value, nil
}

func verifyManagedJSONSubset(installed, managed map[string]any, override claudeoverride.EnvOverride) error {
	for key, expected := range managed {
		if key == "env" {
			managedEnv, ok := expected.(map[string]any)
			if !ok {
				return fmt.Errorf("managed env is not an object")
			}
			installedEnv, _ := installed[key].(map[string]any)
			if err := verifyManagedClaudeEnv(installedEnv, managedEnv, override); err != nil {
				return err
			}
			continue
		}
		actual, ok := installed[key]
		if !ok {
			return fmt.Errorf("missing key %s", key)
		}
		if err := verifyManagedJSONValue(key, actual, expected); err != nil {
			return err
		}
	}
	return nil
}

func verifyManagedClaudeEnv(installed, managed map[string]any, override claudeoverride.EnvOverride) error {
	deleted := make(map[string]bool, len(override.Deletes))
	for _, key := range override.Deletes {
		deleted[key] = true
	}
	for key, managedValue := range managed {
		if deleted[key] {
			if _, exists := installed[key]; exists {
				return fmt.Errorf("env.%s should be deleted by local override", key)
			}
			continue
		}
		expected := managedValue
		if value, overridden := override.Sets[key]; overridden {
			expected = value
		}
		actual, exists := installed[key]
		if !exists || !reflect.DeepEqual(actual, expected) {
			return fmt.Errorf("env.%s mismatch", key)
		}
	}
	for key, expected := range override.Sets {
		if _, covered := managed[key]; covered {
			continue
		}
		actual, exists := installed[key]
		if !exists || !reflect.DeepEqual(actual, expected) {
			return fmt.Errorf("env.%s mismatch", key)
		}
	}
	for _, key := range override.Deletes {
		if _, covered := managed[key]; covered {
			continue
		}
		if _, exists := installed[key]; exists {
			return fmt.Errorf("env.%s should be deleted by local override", key)
		}
	}
	return nil
}

func verifyManagedJSONValue(path string, actual, expected any) error {
	expectedMap, nested := expected.(map[string]any)
	if !nested {
		if !reflect.DeepEqual(actual, expected) {
			return fmt.Errorf("%s mismatch", path)
		}
		return nil
	}
	actualMap, ok := actual.(map[string]any)
	if !ok {
		return fmt.Errorf("%s is not an object", path)
	}
	for key, childExpected := range expectedMap {
		childActual, exists := actualMap[key]
		if !exists {
			return fmt.Errorf("missing key %s.%s", path, key)
		}
		if err := verifyManagedJSONValue(path+"."+key, childActual, childExpected); err != nil {
			return err
		}
	}
	return nil
}
