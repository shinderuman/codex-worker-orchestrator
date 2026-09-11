package parentactioncmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/claudeoverride"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/cliinstall"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexinstall"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/settingsmerge"
)

func verifyRuntimeMergedConfigFiles(cfg config.AppConfig, _ []string) error {
	if cfg.CodexConfigDir == "" && cfg.ClaudeSettingsPath == "" {
		return nil
	}
	if cfg.CodexConfigDir == "" {
		return fmt.Errorf("codex config directory is empty")
	}
	if err := codexinstall.Verify(cfg.RepoRoot, cfg.CodexConfigDir); err != nil {
		return fmt.Errorf("verify managed Codex installation: %w", err)
	}
	if err := verifyInstalledClaudeManagedSettings(cfg); err != nil {
		return err
	}
	worker, err := resolveGLMWorker()
	if err != nil {
		return err
	}
	if err := cliinstall.Verify(filepath.Dir(worker)); err != nil {
		return fmt.Errorf("verify repository CLI installation: %w", err)
	}
	return nil
}

func verifyInstalledClaudeManagedSettings(cfg config.AppConfig) error {
	managedPath := filepath.Join(cfg.RepoRoot, "claude", "settings-managed.json")
	managed, err := readJSONObject(managedPath)
	if err != nil {
		return fmt.Errorf("read managed Claude settings: %w", err)
	}
	settingsPath := cfg.ClaudeSettingsPath
	if settingsPath == "" {
		return fmt.Errorf("claude settings path is empty")
	}
	if err := settingsmerge.VerifyManagedInstallation(settingsPath, managedPath, cfg.ClaudeSettingsOverride); err != nil {
		return fmt.Errorf("verify managed Claude settings ownership: %w", err)
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
	deleted := deletedClaudeEnvKeys(override.Deletes)
	if err := verifyManagedClaudeEnvKeys(installed, managed, override.Sets, deleted); err != nil {
		return err
	}
	if err := verifyOverrideClaudeEnvSets(installed, managed, override.Sets); err != nil {
		return err
	}
	return verifyOverrideClaudeEnvDeletes(installed, managed, override.Deletes)
}

func deletedClaudeEnvKeys(keys []string) map[string]bool {
	deleted := make(map[string]bool, len(keys))
	for _, key := range keys {
		deleted[key] = true
	}
	return deleted
}

func verifyManagedClaudeEnvKeys(installed, managed map[string]any, sets map[string]string, deleted map[string]bool) error {
	for key, managedValue := range managed {
		if deleted[key] {
			if _, exists := installed[key]; exists {
				return fmt.Errorf("env.%s should be deleted by local override", key)
			}
			continue
		}
		expected := managedValue
		if value, overridden := sets[key]; overridden {
			expected = value
		}
		actual, exists := installed[key]
		if !exists || !reflect.DeepEqual(actual, expected) {
			return fmt.Errorf("env.%s mismatch", key)
		}
	}
	return nil
}

func verifyOverrideClaudeEnvSets(installed, managed map[string]any, sets map[string]string) error {
	for key, expected := range sets {
		if _, covered := managed[key]; covered {
			continue
		}
		actual, exists := installed[key]
		if !exists || !reflect.DeepEqual(actual, expected) {
			return fmt.Errorf("env.%s mismatch", key)
		}
	}
	return nil
}

func verifyOverrideClaudeEnvDeletes(installed, managed map[string]any, deletes []string) error {
	for _, key := range deletes {
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
