package codexinstall

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexcontext"
)

// TestInstalledInstructionOptInScope covers installation and the separately
// activated project instruction entrypoint, without claiming to execute Codex.
func TestInstalledInstructionOptInScope(t *testing.T) {
	cases := []struct {
		name   string
		plan   bool
		optIn  bool
	}{
		{name: "project-plan-opt-in", plan: true, optIn: true},
		{name: "project-plan-no-opt-in", plan: true},
		{name: "no-project-plan-no-opt-in"},
		{name: "no-project-plan-explicit-opt-in", optIn: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sourceRepo := initInstallFixtureRepo(t)
			codexDir := t.TempDir()
			globalAgents := filepath.Join(codexDir, "AGENTS.md")
			globalOriginal := []byte("# user-owned global instructions\n")
			writeTestFile(t, globalAgents, globalOriginal)

			runInstall(t, sourceRepo, codexDir)
			installedInstructions := filepath.Join(codexDir, "instructions", "codex-worker-orchestrator.md")
			assertFileBytes(t, installedInstructions, readTestFile(t, filepath.Join(sourceRepo, "codex", "AGENTS.md")))
			assertFileBytes(t, globalAgents, globalOriginal)
			globalConfig := readTestFile(t, filepath.Join(codexDir, "config.toml"))
			if bytes.Contains(globalConfig, []byte("developer_instructions")) ||
				bytes.Contains(globalConfig, []byte("instructions/codex-worker-orchestrator.md")) {
				t.Fatalf("installer activated project instructions in global config:\n%s", globalConfig)
			}

			targetRepo := filepath.Join(t.TempDir(), "target")
			if err := os.MkdirAll(targetRepo, 0o755); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command("git", "init", "-q", targetRepo).CombinedOutput(); err != nil {
				t.Fatalf("init target repo: %v: %s", err, output)
			}
			projectAgents := filepath.Join(targetRepo, "AGENTS.md")
			projectOriginal := []byte("# target repository's own instructions\n")
			writeTestFile(t, projectAgents, projectOriginal)
			if tc.plan {
				writeTestFile(t, filepath.Join(targetRepo, "IMPLEMENTATION_PLAN.local.md"), []byte("# target plan\n"))
			}

			configPath := filepath.Join(targetRepo, filepath.FromSlash(codexcontext.ProjectConfigRelativePath))
			if _, err := os.Stat(configPath); !os.IsNotExist(err) {
				t.Fatalf("install unexpectedly activated target repository: %v", err)
			}
			if tc.optIn {
				var output bytes.Buffer
				if err := codexcontext.Run([]string{"enable", targetRepo}, &output); err != nil {
					t.Fatalf("explicit project opt-in: %v", err)
				}
				projectConfig := readTestFile(t, configPath)
				for _, required := range []string{
					"developer_instructions = ",
					"instructions/codex-worker-orchestrator.md",
				} {
					if !bytes.Contains(projectConfig, []byte(required)) {
						t.Fatalf("opt-in config missing %q:\n%s", required, projectConfig)
					}
				}
				var disabled bytes.Buffer
				if err := codexcontext.Run([]string{"disable", targetRepo}, &disabled); err != nil {
					t.Fatalf("disable project opt-in: %v", err)
				}
				if _, err := os.Stat(configPath); !os.IsNotExist(err) {
					t.Fatalf("project opt-in remains after disable: %v", err)
				}
			} else if _, err := os.Stat(configPath); !os.IsNotExist(err) {
				t.Fatalf("no opt-in should leave target project config absent: %v", err)
			}

			assertFileBytes(t, globalAgents, globalOriginal)
			assertFileBytes(t, projectAgents, projectOriginal)
			if _, err := os.Stat(filepath.Join(targetRepo, "AGENTS.override.md")); !os.IsNotExist(err) {
				t.Fatalf("target user override created: %v", err)
			}
			if _, err := os.Stat(filepath.Join(targetRepo, "IMPLEMENTATION_PLAN.local.md")); tc.plan && err != nil {
				t.Fatalf("target plan removed or changed: %v", err)
			} else if !tc.plan && !os.IsNotExist(err) {
				t.Fatalf("tool created missing target plan: %v", err)
			}
		})
	}
}
