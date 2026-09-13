package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/claudeoverride"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/claudesettings"
)

type AppConfig struct {
	RepoRoot  string
	RepoHash  string
	RepoShort string
	StateBase string

	WorktreeBase        string
	RepoSearchCacheRoot string
	PromptDir           string
	ClaudeBin           string
	CodexBin            string

	ClaudeConfigDir    string
	ClaudeSettingsPath string

	ClaudeSettingsOverride string

	EnvAllowlist []string

	CodexConfigDir        string
	WorkerModel           string
	ReviewerModel         string
	HighRiskReviewerModel string
	RoutineEffort         string
	EscalatedEffort       string
	MaxAutoFixRounds      int
	TelemetryContent      bool
	RepoSearch            bool
}

func RepoHashFor(root string) string {
	sum := sha256.Sum256([]byte(root))
	return hex.EncodeToString(sum[:])
}

func Load() (AppConfig, error) {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return AppConfig{}, err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return AppConfig{}, fmt.Errorf("ホームディレクトリを取得できません: %w", err)
	}

	claudeLocation, err := claudesettings.Resolve(home, os.Getenv("CLAUDE_CONFIG_DIR"), os.Getenv("CLAUDE_SETTINGS_FILE"))
	if err != nil {
		return AppConfig{}, fmt.Errorf("claude settings location: %w", err)
	}
	repoHashString := RepoHashFor(repoRoot)

	stateHome := envOrDefault("GLM_WORKER_HOME", filepath.Join(home, ".glm-worker"))
	codexConfigDir := envOrDefault("CODEX_CONFIG_DIR", envOrDefault("CODEX_HOME", filepath.Join(home, ".codex")))
	promptDir := envOrDefault("GLM_WORKER_PROMPT_DIR", filepath.Join(codexConfigDir, "glm-worker", "prompts"))
	claudeSettingsOverride := claudeoverride.ResolvePath(home)
	envAllowlist := splitEnvList(os.Getenv("GLM_WORKER_ENV_ALLOWLIST"))
	rounds, err := intEnv("GLM_WORKER_MAX_AUTO_FIX_ROUNDS", 2)
	if err != nil {
		return AppConfig{}, err
	}
	telemetryContent, err := boolEnv("GLM_WORKER_TELEMETRY_CONTENT", true)
	if err != nil {
		return AppConfig{}, err
	}
	repoSearch, err := boolEnv("GLM_WORKER_REPO_SEARCH", true)
	if err != nil {
		return AppConfig{}, err
	}

	return AppConfig{
		RepoRoot:               repoRoot,
		RepoHash:               repoHashString,
		RepoShort:              repoHashString[:12],
		StateBase:              filepath.Join(stateHome, "sessions"),
		WorktreeBase:           filepath.Join(stateHome, "worktrees"),
		RepoSearchCacheRoot:    filepath.Join(stateHome, "search"),
		PromptDir:              promptDir,
		CodexConfigDir:         codexConfigDir,
		ClaudeBin:              envOrDefault("GLM_WORKER_CLAUDE_BIN", "claude"),
		CodexBin:               envOrDefault("GLM_WORKER_CODEX_BIN", "codex"),
		ClaudeConfigDir:        claudeLocation.ConfigDir,
		ClaudeSettingsPath:     claudeLocation.SettingsPath,
		ClaudeSettingsOverride: claudeSettingsOverride,
		EnvAllowlist:           envAllowlist,
		WorkerModel:            envOrDefault("GLM_WORKER_WORKER_MODEL", "opus"),
		ReviewerModel:          envOrDefault("GLM_WORKER_REVIEWER_MODEL", "haiku"),
		HighRiskReviewerModel:  envOrDefault("GLM_WORKER_HIGH_RISK_REVIEWER_MODEL", "sonnet"),
		RoutineEffort:          envOrDefault("GLM_WORKER_EFFORT", "high"),
		EscalatedEffort:        envOrDefault("GLM_WORKER_ESCALATED_EFFORT", "max"),
		MaxAutoFixRounds:       rounds,
		TelemetryContent:       telemetryContent,
		RepoSearch:             repoSearch,
	}, nil
}

func resolveRepoRoot() (string, error) {
	command := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := command.Output()
	if err == nil {
		root := strings.TrimSpace(string(output))
		return filepath.EvalSymlinks(root)
	}

	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		return "", fmt.Errorf("作業ディレクトリを取得できません: %w", cwdErr)
	}

	root, evalErr := filepath.EvalSymlinks(cwd)
	if evalErr != nil {
		return "", fmt.Errorf("作業ディレクトリを解決できません: %w", evalErr)
	}

	return root, nil
}

func envOrDefault(name string, defaultValue string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return defaultValue
}

func splitEnvList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if key := strings.TrimSpace(part); key != "" {
			result = append(result, key)
		}
	}
	return result
}

func intEnv(name string, defaultValue int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return defaultValue, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%sは0以上の整数で指定してください", name)
	}
	return value, nil
}

func boolEnv(name string, defaultValue bool) (bool, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%sは真偽値で指定してください", name)
	}
	return value, nil
}
