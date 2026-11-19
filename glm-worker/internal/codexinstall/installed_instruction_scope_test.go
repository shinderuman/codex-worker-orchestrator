package codexinstall

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexcontext"
)

func TestInstalledInstructionOptInScope(t *testing.T) {
	cases := []struct {
		name  string
		plan  bool
		optIn bool
	}{
		{name: "project-plan-opt-in", plan: true, optIn: true},
		{name: "project-plan-no-opt-in", plan: true},
		{name: "no-project-plan-no-opt-in"},
		{name: "no-project-plan-explicit-opt-in", optIn: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			codexDir := installScopeFixture(t)
			targetRepo := initScopeTargetRepo(t, tc.plan)
			checkScopeActivation(t, targetRepo, tc.optIn)
			assertFileBytes(t, filepath.Join(codexDir, "AGENTS.md"), []byte("# user-owned global instructions\n"))
			assertFileBytes(t, filepath.Join(targetRepo, "AGENTS.md"), []byte("# target repository's own instructions\n"))
			if _, err := os.Stat(filepath.Join(targetRepo, "AGENTS.override.md")); !os.IsNotExist(err) {
				t.Fatalf("target user override created: %v", err)
			}
			planPath := filepath.Join(targetRepo, "IMPLEMENTATION_PLAN.local.md")
			if tc.plan {
				assertFileBytes(t, planPath, []byte("# target plan\n"))
			} else if _, err := os.Stat(planPath); !os.IsNotExist(err) {
				t.Fatalf("tool created missing target plan: %v", err)
			}
		})
	}
}

func installScopeFixture(t *testing.T) string {
	t.Helper()
	sourceRepo := initInstallFixtureRepo(t)
	codexDir := t.TempDir()
	globalAgents := filepath.Join(codexDir, "AGENTS.md")
	globalOriginal := []byte("# user-owned global instructions\n")
	writeTestFile(t, globalAgents, globalOriginal)
	runInstall(t, sourceRepo, codexDir)

	installedInstructions := filepath.Join(codexDir, "instructions", "codex-worker-orchestrator.md")
	if _, err := os.Lstat(installedInstructions); !os.IsNotExist(err) {
		t.Fatalf("repository-specific instructions were installed at user scope: %v", err)
	}
	assertFileBytes(t, globalAgents, globalOriginal)
	globalConfig := readTestFile(t, filepath.Join(codexDir, "config.toml"))
	if bytes.Contains(globalConfig, []byte("developer_instructions")) ||
		bytes.Contains(globalConfig, []byte("instructions/codex-worker-orchestrator.md")) {
		t.Fatalf("installer activated project instructions in global config:\n%s", globalConfig)
	}
	return codexDir
}

func initScopeTargetRepo(t *testing.T, plan bool) string {
	t.Helper()
	targetRepo := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(targetRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "init", "-q", targetRepo).CombinedOutput(); err != nil {
		t.Fatalf("init target repo: %v: %s", err, output)
	}
	writeTestFile(t, filepath.Join(targetRepo, "AGENTS.md"), []byte("# target repository's own instructions\n"))
	if plan {
		writeTestFile(t, filepath.Join(targetRepo, "IMPLEMENTATION_PLAN.local.md"), []byte("# target plan\n"))
	}
	return targetRepo
}

func checkScopeActivation(t *testing.T, targetRepo string, optIn bool) {
	t.Helper()
	configPath := filepath.Join(targetRepo, filepath.FromSlash(codexcontext.ProjectConfigRelativePath))
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("install unexpectedly activated target repository: %v", err)
	}
	if !optIn {
		return
	}
	var output bytes.Buffer
	if err := codexcontext.Run([]string{"enable", targetRepo}, &output); err != nil {
		t.Fatalf("explicit project opt-in: %v", err)
	}
	projectConfig := readTestFile(t, configPath)
	if bytes.Contains(projectConfig, []byte("developer_instructions")) || bytes.Contains(projectConfig, []byte("instructions/codex-worker-orchestrator.md")) {
		t.Fatalf("project opt-in injected repository-specific instructions: %s", projectConfig)
	}
	var disabled bytes.Buffer
	if err := codexcontext.Run([]string{"disable", targetRepo}, &disabled); err != nil {
		t.Fatalf("disable project opt-in: %v", err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("project opt-in remains after disable: %v", err)
	}
}

func TestInstallRetiresPreviouslyOwnedParentRules(t *testing.T) {
	repo := initInstallFixtureRepo(t)
	codexDir := installScopeFixture(t)
	state := loadTestState(t, codexDir)
	retired := []string{
		"instructions/codex-worker-orchestrator.md",
		"instructions/glm-execution.md",
		"instructions/quality-gate-capability.md",
	}
	for _, path := range retired {
		content := []byte("# formerly managed project rule\n")
		writeTestFile(t, filepath.Join(codexDir, filepath.FromSlash(path)), content)
		state.Files = append(state.Files, managedFileRecord{Path: path, SHA256: digestBytes(content)})
	}
	if err := writeState(codexDir, state); err != nil {
		t.Fatal(err)
	}
	runInstall(t, repo, codexDir)

	for _, path := range retired {
		if _, err := os.Lstat(filepath.Join(codexDir, filepath.FromSlash(path))); !os.IsNotExist(err) {
			t.Fatalf("retired parent rule %s remains installed: %v", path, err)
		}
		if _, owned := stateFileMap(loadTestState(t, codexDir))[path]; owned {
			t.Fatalf("removed parent rule remains tool-owned: %s", path)
		}
	}
	assertFileBytes(t, filepath.Join(codexDir, "AGENTS.md"), []byte("# user-owned global instructions\n"))
	assertFileBytes(t, filepath.Join(codexDir, "instructions", "worker", "test.md"),
		readTestFile(t, filepath.Join(repo, "codex", "instructions", "worker", "test.md")))
}
