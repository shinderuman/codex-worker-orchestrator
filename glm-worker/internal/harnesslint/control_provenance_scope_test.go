package harnesslint

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestScopedControlProvenanceRequiresRegistryForOrchestratorRepository(t *testing.T) {
	root := newControlProvenanceIdentityRepo(t, "https://github.com/shinderuman/codex-worker-orchestrator.git")

	violations, err := scopedControlProvenanceViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasControlProvenanceViolation(violations, controlProvenanceRegistryPath, "registry is missing") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestScopedControlProvenancePassesValidRegistryForOrchestratorRepository(t *testing.T) {
	root := newControlProvenanceIdentityRepo(t, "https://github.com/shinderuman/codex-worker-orchestrator.git")
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

func TestScopedControlProvenanceSkipsOtherRepositoryWithoutRegistry(t *testing.T) {
	for _, origin := range []string{"", "https://github.com/example/other-repository.git"} {
		t.Run(origin, func(t *testing.T) {
			root := newControlProvenanceIdentityRepo(t, origin)
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

func TestScopedControlProvenanceIgnoresSameNamedRegistryInOtherRepository(t *testing.T) {
	root := newControlProvenanceIdentityRepo(t, "https://github.com/example/other-repository.git")
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

func newControlProvenanceIdentityRepo(t *testing.T, origin string) string {
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
	if err := os.WriteFile(moduleFile, []byte("module "+controlProvenanceModulePath+"\n"), 0o600); err != nil {
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
