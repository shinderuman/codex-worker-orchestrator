package harnesslint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestControlProvenanceValidMachineAndNonMachineEntries(t *testing.T) {
	root := t.TempDir()
	writeControlProvenanceGo(t, root, "owner.go", "package fixture\ntype ownerType struct{}\nfunc (*ownerType) owner() {}\nfunc postcondition() {}\n")
	writeControlProvenanceGo(t, root, "owner_test.go", "package fixture\nfunc TestOwner() {}\n")
	writeControlProvenanceRegistry(t, root, controlProvenanceRegistry{
		Version: 1,
		Controls: []controlProvenanceControl{
			{
				ID:                     "machine",
				Classification:         controlClassificationMachine,
				Purpose:                "machine purpose",
				MachineOwners:          []controlProvenanceLocator{{Path: "owner.go", Symbol: "ownerType.owner"}},
				Tests:                  []controlProvenanceLocator{{Path: "owner_test.go", Symbol: "TestOwner"}},
				Postconditions:         []controlProvenanceLocator{{Path: "owner.go", Symbol: "postcondition"}},
				ResidualParentJudgment: "semantic disposition",
			},
			{
				ID:                     "partial",
				Classification:         controlClassificationPartial,
				Purpose:                "partial purpose",
				ResidualParentJudgment: "semantic disposition",
				Boundary:               "not fully machine enforced",
			},
		},
	})

	violations, err := controlProvenanceViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestControlProvenanceLocatorDriftFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name       string
		kind       string
		mutate     func(*controlProvenanceControl)
		file       string
		wantSymbol string
	}{
		{
			name: "owner rename",
			kind: "machine owner",
			mutate: func(control *controlProvenanceControl) {
				control.MachineOwners[0].Symbol = "renamedOwner"
			},
			file:       "owner.go",
			wantSymbol: "renamedOwner",
		},
		{
			name: "test deletion",
			kind: "test",
			mutate: func(control *controlProvenanceControl) {
				control.Tests[0].Symbol = "DeletedTest"
			},
			file:       "owner_test.go",
			wantSymbol: "DeletedTest",
		},
		{
			name: "postcondition rename",
			kind: "postcondition",
			mutate: func(control *controlProvenanceControl) {
				control.Postconditions[0].Symbol = "renamedPostcondition"
			},
			file:       "owner.go",
			wantSymbol: "renamedPostcondition",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeControlProvenanceGo(t, root, "owner.go", "package fixture\nfunc owner() {}\nfunc postcondition() {}\n")
			writeControlProvenanceGo(t, root, "owner_test.go", "package fixture\nfunc TestOwner() {}\n")
			control := controlProvenanceMachineFixture()
			tc.mutate(&control)
			writeControlProvenanceRegistry(t, root, controlProvenanceRegistry{Version: 1, Controls: []controlProvenanceControl{control}})

			violations, err := controlProvenanceViolations(root)
			if err != nil {
				t.Fatal(err)
			}
			if !hasControlProvenanceViolation(violations, tc.file, tc.kind, tc.wantSymbol) {
				t.Fatalf("violations = %#v", violations)
			}
		})
	}
}

func TestControlProvenanceRejectsMachineLocatorsOnNonMachineControl(t *testing.T) {
	root := t.TempDir()
	writeControlProvenanceGo(t, root, "owner.go", "package fixture\nfunc owner() {}\n")
	writeControlProvenanceRegistry(t, root, controlProvenanceRegistry{
		Version: 1,
		Controls: []controlProvenanceControl{{
			ID:                     "partial",
			Classification:         controlClassificationPartial,
			Purpose:                "partial purpose",
			MachineOwners:          []controlProvenanceLocator{{Path: "owner.go", Symbol: "owner"}},
			ResidualParentJudgment: "semantic disposition",
			Boundary:               "not fully machine enforced",
		}},
	})

	violations, err := controlProvenanceViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasControlProvenanceViolation(violations, controlProvenanceRegistryPath, "must not carry machine owner", "partial") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestControlProvenanceRejectsLineNumberSchema(t *testing.T) {
	root := t.TempDir()
	registryPath := filepath.Join(root, filepath.FromSlash(controlProvenanceRegistryPath))
	if err := os.MkdirAll(filepath.Dir(registryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"version":1,"controls":[{"id":"partial","classification":"partial","purpose":"p","residual_parent_judgment":"r","boundary":"b","line":42}]}`
	if err := os.WriteFile(registryPath, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	violations, err := controlProvenanceViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasControlProvenanceViolation(violations, controlProvenanceRegistryPath, "unknown field", "line") {
		t.Fatalf("violations = %#v", violations)
	}
}

func controlProvenanceMachineFixture() controlProvenanceControl {
	return controlProvenanceControl{
		ID:                     "machine",
		Classification:         controlClassificationMachine,
		Purpose:                "machine purpose",
		MachineOwners:          []controlProvenanceLocator{{Path: "owner.go", Symbol: "owner"}},
		Tests:                  []controlProvenanceLocator{{Path: "owner_test.go", Symbol: "TestOwner"}},
		Postconditions:         []controlProvenanceLocator{{Path: "owner.go", Symbol: "postcondition"}},
		ResidualParentJudgment: "semantic disposition",
	}
}

func writeControlProvenanceRegistry(t *testing.T, root string, registry controlProvenanceRegistry) {
	t.Helper()
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(root, filepath.FromSlash(controlProvenanceRegistryPath))
	if err := os.MkdirAll(filepath.Dir(registryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registryPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeControlProvenanceGo(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func hasControlProvenanceViolation(violations []Violation, path string, fragments ...string) bool {
	for _, violation := range violations {
		if violation.Rule != "control-provenance" || violation.Path != path {
			continue
		}
		match := true
		for _, fragment := range fragments {
			if !strings.Contains(violation.Message, fragment) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
