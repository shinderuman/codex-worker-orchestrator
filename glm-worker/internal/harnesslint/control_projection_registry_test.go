package harnesslint

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestControlProjectionRegistryCoversEarlierMechanizedThinningSurfaces(t *testing.T) {
	root := controlProjectionRepositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(controlProvenanceRegistryPath)))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := decodeControlProvenanceRegistry(data)
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string][]string{
		"external-feasibility-admission": {
			"codex/instructions/glm-execution.md",
		},
		"packet-schema-result": {
			"codex/instructions/glm-packets.md",
		},
		"parent-action-staging-admission": {
			"codex/AGENTS.md",
			"codex/instructions/glm-execution.md",
		},
		"parent-evidence-projection-dedup": {
			"codex/AGENTS.md",
		},
		"quality-snapshot-binding": {
			"codex/instructions/quality-gate-capability.md",
		},
		"session-rotation-claim-bind-start": {
			"codex/instructions/session-rotation.md",
		},
	}

	for controlID, paths := range expected {
		control, ok := controlProjectionRegistryControl(registry, controlID)
		if !ok {
			t.Fatalf("control %q is missing", controlID)
		}
		if control.Classification != controlClassificationMachine {
			t.Fatalf("control %q classification = %q", controlID, control.Classification)
		}
		for _, path := range paths {
			guard, ok := controlProjectionGuardForPath(control, path)
			if !ok {
				t.Errorf("control %q has no projection guard for %q", controlID, path)
				continue
			}
			if len(guard.ForbiddenTokens) == 0 {
				t.Errorf("control %q projection guard for %q has no forbidden tokens", controlID, path)
			}
		}
	}
}

func controlProjectionRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func controlProjectionRegistryControl(registry controlProvenanceRegistry, id string) (controlProvenanceControl, bool) {
	for _, control := range registry.Controls {
		if control.ID == id {
			return control, true
		}
	}
	return controlProvenanceControl{}, false
}

func controlProjectionGuardForPath(control controlProvenanceControl, path string) (controlProvenanceProjectionGuard, bool) {
	for _, guard := range control.ProjectionGuards {
		if guard.Path == path {
			return guard, true
		}
	}
	return controlProvenanceProjectionGuard{}, false
}
