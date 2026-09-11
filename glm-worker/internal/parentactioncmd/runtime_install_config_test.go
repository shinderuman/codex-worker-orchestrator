package parentactioncmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/settingsmerge"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestVerifyInstalledCodexManagedConfigPreservesLocalKeysButRejectsManagedDrift(t *testing.T) {
	repo := t.TempDir()
	codexDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "codex", "config-managed.toml"), []byte("background_terminal_max_timeout = 21600000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(codexDir, "config.toml")
	if err := os.WriteFile(installed, []byte("local_key = \"keep\"\nbackground_terminal_max_timeout = 21600000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.AppConfig{RepoRoot: repo, CodexConfigDir: codexDir}
	if err := verifyInstalledCodexManagedConfig(cfg); err != nil {
		t.Fatalf("matching managed config rejected: %v", err)
	}
	if err := os.WriteFile(installed, []byte("local_key = \"keep\"\nbackground_terminal_max_timeout = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyInstalledCodexManagedConfig(cfg); err == nil {
		t.Fatal("stale managed Codex value was accepted")
	}
}

func TestTopLevelTOMLAssignmentsRejectsMultilineValues(t *testing.T) {
	for _, input := range []string{
		"managed = [\n  1,\n  2,\n]\n",
		"managed = \"\"\"line one\nline two\"\"\"\n",
	} {
		if _, err := topLevelTOMLAssignments([]byte(input)); err == nil {
			t.Fatalf("multiline managed TOML value was accepted: %q", input)
		}
	}
}

func TestVerifyInstalledClaudeManagedSettingsAppliesLocalOverride(t *testing.T) {
	repo := t.TempDir()
	claudeDir := t.TempDir()
	overrideDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	managedPath := filepath.Join(repo, "claude", "settings-managed.json")
	managed := `{"env":{"A":"managed","B":"managed"},"permissions":{"mode":"managed"}}`
	if err := os.WriteFile(managedPath, []byte(managed), 0o644); err != nil {
		t.Fatal(err)
	}
	overridePath := filepath.Join(overrideDir, "claude-settings.local.json")
	if err := os.WriteFile(overridePath, []byte(`{"env":{"A":"override","B":null}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	installedPath := filepath.Join(claudeDir, "settings.json")
	baseline := `{"env":{"LOCAL":"keep"},"permissions":{"local":true},"other":"keep"}`
	if err := os.WriteFile(installedPath, []byte(baseline), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := settingsmerge.MergeFiles(installedPath, managedPath, overridePath); err != nil {
		t.Fatal(err)
	}
	cfg := config.AppConfig{RepoRoot: repo, ClaudeConfigDir: claudeDir, ClaudeSettingsPath: installedPath, ClaudeSettingsOverride: overridePath}
	if err := verifyInstalledClaudeManagedSettings(cfg); err != nil {
		t.Fatalf("matching managed Claude settings rejected: %v", err)
	}
	stale := `{"env":{"A":"managed","LOCAL":"keep"},"permissions":{"mode":"managed","local":true}}`
	if err := os.WriteFile(installedPath, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyInstalledClaudeManagedSettings(cfg); err == nil {
		t.Fatal("stale managed Claude value was accepted")
	}
}

func TestVerifyInstalledClaudeManagedSettingsChecksOverrideOnlyEnvKeys(t *testing.T) {
	repo := t.TempDir()
	claudeDir := t.TempDir()
	overrideDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	managedPath := filepath.Join(repo, "claude", "settings-managed.json")
	managed := `{"env":{"A":"managed"}}`
	if err := os.WriteFile(managedPath, []byte(managed), 0o644); err != nil {
		t.Fatal(err)
	}
	overridePath := filepath.Join(overrideDir, "claude-settings.local.json")
	if err := os.WriteFile(overridePath, []byte(`{"env":{"ONLY":"override","REMOVE":null}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	installedPath := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(installedPath, []byte(`{"env":{"REMOVE":"baseline"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := settingsmerge.MergeFiles(installedPath, managedPath, overridePath); err != nil {
		t.Fatal(err)
	}
	cfg := config.AppConfig{RepoRoot: repo, ClaudeConfigDir: claudeDir, ClaudeSettingsPath: installedPath, ClaudeSettingsOverride: overridePath}

	if err := verifyInstalledClaudeManagedSettings(cfg); err != nil {
		t.Fatalf("valid override-only env state rejected: %v", err)
	}
	if err := os.WriteFile(installedPath, []byte(`{"env":{"A":"managed"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyInstalledClaudeManagedSettings(cfg); err == nil {
		t.Fatal("missing override-only set key was accepted")
	}
	if err := os.WriteFile(installedPath, []byte(`{"env":{"A":"managed","ONLY":"override","REMOVE":"stale"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyInstalledClaudeManagedSettings(cfg); err == nil {
		t.Fatal("retained override-only delete key was accepted")
	}
}

func TestVerifyRuntimeMergedConfigFilesOnlyChecksChangedManagedSurfaces(t *testing.T) {
	cfg := config.AppConfig{RepoRoot: t.TempDir()}
	if err := verifyRuntimeMergedConfigFiles(cfg, []string{"glm-worker/internal/app/app.go"}); err != nil {
		t.Fatalf("unrelated runtime path required merged config state: %v", err)
	}
}

func TestCompleteRejectsManagedCodexConfigDrift(t *testing.T) {
	fixture := newCompleteFixture(t)
	fixture.cfg.CodexConfigDir = t.TempDir()
	if err := state.CaptureGitBaseline(fixture.cfg, fixture.st); err != nil {
		t.Fatal(err)
	}
	writeRuntimeInstallHarnessMarker(t, fixture.repo)
	managedDir := filepath.Join(fixture.repo, "codex")
	if err := os.MkdirAll(managedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(managedDir, "config-managed.toml"), []byte("background_terminal_max_timeout = 21600000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runFinalizationGit(t, fixture.repo, "add", "-A")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-m", "runtime config")
	installedHead := completeFixtureHead(t, fixture.repo)
	requirement, err := runtimeInstallRequirementForTask(fixture.repo, fixture.st)
	if err != nil || !requirement.Required {
		t.Fatalf("runtime requirement = %#v err=%v", requirement, err)
	}
	taskID, err := fixture.st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.st.SaveRuntimeInstallEvidence(state.RuntimeInstallEvidence{
		Version:           1,
		TaskID:            taskID,
		Head:              installedHead,
		SourceDigest:      requirement.SourceDigest,
		InstalledRevision: installedHead,
		SmokeResult:       state.ValidationResultPass,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.cfg.CodexConfigDir, "config.toml"), []byte("background_terminal_max_timeout = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeInstalledRuntimeProbeStub(t, installedHead)
	fixture.commitParentMetadataSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusAwaiting || output.Completed || output.Failure == nil || output.Failure.Reason != runtimeInstallFailureInstalled {
		t.Fatalf("completion accepted stale managed config = %#v", output)
	}
}
