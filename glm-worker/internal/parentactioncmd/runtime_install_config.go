package parentactioncmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/claudeoverride"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/cliinstall"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexinstall"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/settingsmerge"
)

type topLevelTOMLScanState struct {
	quote       byte
	escaped     bool
	squareDepth int
	curlyDepth  int
}

func verifyRuntimeMergedConfigFiles(cfg config.AppConfig, _ []string) error {
	// Some focused unit fixtures intentionally omit installer destinations. A
	// real loaded AppConfig resolves both paths before runtime installation.
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
		stop, err := addTopLevelTOMLAssignment(values, strings.TrimSpace(raw))
		if err != nil {
			return nil, err
		}
		if stop {
			break
		}
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("no managed top-level assignments")
	}
	return values, nil
}

func addTopLevelTOMLAssignment(values map[string]string, line string) (bool, error) {
	if line == "" || strings.HasPrefix(line, "#") {
		return false, nil
	}
	if strings.HasPrefix(line, "[") {
		return true, nil
	}
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return false, nil
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return false, fmt.Errorf("invalid top-level assignment %q", line)
	}
	if topLevelTOMLValueContinues(value) {
		return false, fmt.Errorf("multiline top-level assignment is not supported: %q", key)
	}
	if _, duplicate := values[key]; duplicate {
		return false, fmt.Errorf("duplicate top-level assignment %q", key)
	}
	values[key] = value
	return false, nil
}

func topLevelTOMLValueContinues(value string) bool {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, `"""`) || strings.HasPrefix(value, "'''") {
		return true
	}
	state := topLevelTOMLScanState{}
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if state.quote != 0 {
			state.consumeQuoted(ch)
			continue
		}
		if ch == '#' {
			return state.hasOpenContainer()
		}
		state.consumeUnquoted(ch)
		if state.invalidDepth() {
			return true
		}
	}
	return state.quote != 0 || state.hasOpenContainer()
}

func (state *topLevelTOMLScanState) consumeQuoted(ch byte) {
	if state.quote == '"' && state.escaped {
		state.escaped = false
		return
	}
	if state.quote == '"' && ch == '\\' {
		state.escaped = true
		return
	}
	if ch == state.quote {
		state.quote = 0
	}
}

func (state *topLevelTOMLScanState) consumeUnquoted(ch byte) {
	switch ch {
	case '"', '\'':
		state.quote = ch
	case '[':
		state.squareDepth++
	case ']':
		state.squareDepth--
	case '{':
		state.curlyDepth++
	case '}':
		state.curlyDepth--
	}
}

func (state topLevelTOMLScanState) hasOpenContainer() bool {
	return state.squareDepth != 0 || state.curlyDepth != 0
}

func (state topLevelTOMLScanState) invalidDepth() bool {
	return state.squareDepth < 0 || state.curlyDepth < 0
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
