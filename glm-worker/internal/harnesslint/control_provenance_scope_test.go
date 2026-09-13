package harnesslint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScopedControlProvenanceSkipsUnrelatedRepositoryWithoutRegistry(t *testing.T) {
	violations, err := scopedControlProvenanceViolations(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestScopedControlProvenanceRequiresRegistryForOrchestratorRepository(t *testing.T) {
	root := t.TempDir()
	moduleFile := filepath.Join(root, "glm-worker", "go.mod")
	if err := os.MkdirAll(filepath.Dir(moduleFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(moduleFile, []byte("module "+controlProvenanceModulePath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	violations, err := scopedControlProvenanceViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasControlProvenanceViolation(violations, controlProvenanceRegistryPath, "registry is missing") {
		t.Fatalf("violations = %#v", violations)
	}
}
