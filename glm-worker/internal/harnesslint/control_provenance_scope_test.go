package harnesslint

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestScopedControlProvenanceRequiresRegistryForOrchestratorRepository(t *testing.T) {
	root := newControlProvenanceIdentityRepo(t, "https://github.com/shinderuman/codex-worker-orchestrator.git", controlProvenanceModulePath)

	violations, err := scopedControlProvenanceViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasControlProvenanceViolation(violations, controlProvenanceRegistryPath, "registry is missing") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestScopedControlProvenanceRequiresRegistryAcrossRemoteChanges(t *testing.T) {
	for _, origin := range []string{"", "https://github.com/example/codex-worker-orchestrator.git", "ssh://git@example.invalid/example/codex-worker-orchestrator.git"} {
		t.Run(origin, func(t *testing.T) {
			root := newControlProvenanceIdentityRepo(t, origin, controlProvenanceModulePath)
			violations, err := scopedControlProvenanceViolations(root)
			if err != nil {
				t.Fatal(err)
			}
			if !hasControlProvenanceViolation(violations, controlProvenanceRegistryPath, "registry is missing") {
				t.Fatalf("violations = %#v", violations)
			}
		})
	}
}

func TestScopedControlProvenancePassesValidRegistryForOrchestratorRepository(t *testing.T) {
	root := newControlProvenanceIdentityRepo(t, "https://github.com/shinderuman/codex-worker-orchestrator.git", controlProvenanceModulePath)
	writeControlProvenanceGo(t, root, "owner.go", "package fixture\nfunc owner() {}\nfunc postcondition() {}\n")
	writeControlProvenanceGo(t, root, "owner_test.go", "package fixture\nfunc TestOwner() {}\n")
	writeControlProvenanceRegistry(t, root, controlProvenanceRegistry{
		Version:  1,
		Controls: []controlProvenanceControl{controlProvenanceMachineFixture()},
	})

	violations, err := scopedControlProvenanceViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestScopedControlProvenanceSkipsOtherModuleWithoutRegistry(t *testing.T) {
	for _, origin := range []string{"", "https://github.com/shinderuman/codex-worker-orchestrator.git", "https://github.com/example/other-repository.git"} {
		t.Run(origin, func(t *testing.T) {
			root := newControlProvenanceIdentityRepo(t, origin, "example.com/other/glm-worker")
			violations, err := scopedControlProvenanceViolations(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(violations) != 0 {
				t.Fatalf("violations = %#v", violations)
			}
		})
	}
}

func TestScopedControlProvenanceIgnoresSameNamedRegistryInOtherModule(t *testing.T) {
	root := newControlProvenanceIdentityRepo(t, "https://github.com/shinderuman/codex-worker-orchestrator.git", "example.com/other/glm-worker")
	registryPath := filepath.Join(root, filepath.FromSlash(controlProvenanceRegistryPath))
	if err := os.MkdirAll(filepath.Dir(registryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registryPath, []byte(`{"version":0,"controls":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	violations, err := scopedControlProvenanceViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations = %#v", violations)
	}
}

func newControlProvenanceIdentityRepo(t *testing.T, origin, modulePath string) string {
	t.Helper()
	root := t.TempDir()
	runControlProvenanceGit(t, root, "init", "-q")
	if origin != "" {
		runControlProvenanceGit(t, root, "remote", "add", "origin", origin)
	}
	moduleFile := filepath.Join(root, "glm-worker", "go.mod")
	if err := os.MkdirAll(filepath.Dir(moduleFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(moduleFile, []byte("module "+modulePath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func runControlProvenanceGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
