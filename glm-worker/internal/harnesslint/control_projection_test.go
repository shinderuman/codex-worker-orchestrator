package harnesslint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const controlProjectionTestOrigin = "https://github.com/shinderuman/codex-worker-orchestrator.git"

func TestControlProjectionAcceptsMachineEnforcedControl(t *testing.T) {
	root := newControlProvenanceIdentityRepo(t, controlProjectionTestOrigin, controlProvenanceModulePath)
	writeControlProvenanceGo(t, root, "owner.go", "package fixture\nfunc owner() {}\nfunc postcondition() {}\n")
	writeControlProvenanceGo(t, root, "owner_test.go", "package fixture\nfunc TestOwner() {}\n")
	writeControlProvenanceRegistry(t, root, controlProvenanceRegistry{
		Version:  1,
		Controls: []controlProvenanceControl{controlProvenanceMachineFixture()},
	})
	writeControlProjectionMarkdown(t, root, "codex/AGENTS.md", "`control:machine`\n")

	violations, err := scopedControlProvenanceViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestControlProjectionRejectsUnknownControl(t *testing.T) {
	root := newControlProvenanceIdentityRepo(t, controlProjectionTestOrigin, controlProvenanceModulePath)
	writeControlProvenanceGo(t, root, "owner.go", "package fixture\nfunc owner() {}\nfunc postcondition() {}\n")
	writeControlProvenanceGo(t, root, "owner_test.go", "package fixture\nfunc TestOwner() {}\n")
	writeControlProvenanceRegistry(t, root, controlProvenanceRegistry{
		Version:  1,
		Controls: []controlProvenanceControl{controlProvenanceMachineFixture()},
	})
	writeControlProjectionMarkdown(t, root, "codex/AGENTS.md", "`control:missing-control`\n")

	violations, err := scopedControlProvenanceViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasControlProjectionViolation(violations, "codex/AGENTS.md", "missing-control", "no provenance registry entry") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestControlProjectionRejectsMalformedControlID(t *testing.T) {
	root := newControlProvenanceIdentityRepo(t, controlProjectionTestOrigin, controlProvenanceModulePath)
	writeControlProvenanceRegistry(t, root, controlProvenanceRegistry{
		Version: 1,
		Controls: []controlProvenanceControl{{
			ID:                     "partial-control",
			Classification:         controlClassificationPartial,
			Purpose:                "partial purpose",
			ResidualParentJudgment: "parent decides",
			Boundary:               "not fully machine enforced",
		}},
	})
	writeControlProjectionMarkdown(t, root, "codex/AGENTS.md", "`control:Bad_ID`\n")

	violations, err := scopedControlProvenanceViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasControlProjectionViolation(violations, "codex/AGENTS.md", "Bad_ID", "invalid id syntax") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestControlProjectionRejectsNonMachineControl(t *testing.T) {
	root := newControlProvenanceIdentityRepo(t, controlProjectionTestOrigin, controlProvenanceModulePath)
	writeControlProvenanceRegistry(t, root, controlProvenanceRegistry{
		Version: 1,
		Controls: []controlProvenanceControl{{
			ID:                     "partial-control",
			Classification:         controlClassificationPartial,
			Purpose:                "partial purpose",
			ResidualParentJudgment: "parent decides",
			Boundary:               "not fully machine enforced",
		}},
	})
	writeControlProjectionMarkdown(t, root, "codex/AGENTS.md", "`control:partial-control`\n")

	violations, err := scopedControlProvenanceViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasControlProjectionViolation(violations, "codex/AGENTS.md", "partial-control", "instead of machine-enforced") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestControlProjectionRejectsReintroducedMachineProcedureToken(t *testing.T) {
	root := newControlProvenanceIdentityRepo(t, controlProjectionTestOrigin, controlProvenanceModulePath)
	writeControlProvenanceGo(t, root, "owner.go", "package fixture\nfunc owner() {}\nfunc postcondition() {}\n")
	writeControlProvenanceGo(t, root, "owner_test.go", "package fixture\nfunc TestOwner() {}\n")
	path := "codex/instructions/task-request-boundary.md"
	control := controlProvenanceMachineFixture()
	control.ID = "parent-action-staging-admission"
	control.ProjectionGuards = []controlProvenanceProjectionGuard{{Path: path, ForbiddenTokens: []string{"start-milestones <token>"}}}
	writeControlProvenanceRegistry(t, root, controlProvenanceRegistry{
		Version:  1,
		Controls: []controlProvenanceControl{control},
	})
	writeControlProjectionMarkdown(t, root, path, "`control:parent-action-staging-admission`\nRun `glm-parent-action start-milestones <token>` after preparing the payload.\n")

	violations, err := scopedControlProvenanceViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasControlProjectionViolation(violations, path, "parent-action-staging-admission", "start-milestones <token>", "reintroduces machine-owned procedure token") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestControlProjectionAllowsResidualSemanticTextOnGuardedSurface(t *testing.T) {
	root := newControlProvenanceIdentityRepo(t, controlProjectionTestOrigin, controlProvenanceModulePath)
	writeControlProvenanceGo(t, root, "owner.go", "package fixture\nfunc owner() {}\nfunc postcondition() {}\n")
	writeControlProvenanceGo(t, root, "owner_test.go", "package fixture\nfunc TestOwner() {}\n")
	path := "codex/instructions/task-request-boundary.md"
	control := controlProvenanceMachineFixture()
	control.ID = "parent-action-staging-admission"
	control.ProjectionGuards = []controlProvenanceProjectionGuard{{Path: path, ForbiddenTokens: []string{"start-milestones <token>"}}}
	writeControlProvenanceRegistry(t, root, controlProvenanceRegistry{
		Version:  1,
		Controls: []controlProvenanceControl{control},
	})
	writeControlProjectionMarkdown(t, root, path, "`control:parent-action-staging-admission`\nThe parent decides milestone meaning, scope, and acceptance.\n")

	violations, err := scopedControlProvenanceViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestControlProjectionProcedureGuardRequiresCompactProjection(t *testing.T) {
	root := newControlProvenanceIdentityRepo(t, controlProjectionTestOrigin, controlProvenanceModulePath)
	writeControlProvenanceGo(t, root, "owner.go", "package fixture\nfunc owner() {}\nfunc postcondition() {}\n")
	writeControlProvenanceGo(t, root, "owner_test.go", "package fixture\nfunc TestOwner() {}\n")
	path := "codex/instructions/task-request-boundary.md"
	control := controlProvenanceMachineFixture()
	control.ID = "parent-action-staging-admission"
	control.ProjectionGuards = []controlProvenanceProjectionGuard{{Path: path, ForbiddenTokens: []string{"start-milestones <token>"}}}
	writeControlProvenanceRegistry(t, root, controlProvenanceRegistry{
		Version:  1,
		Controls: []controlProvenanceControl{control},
	})
	writeControlProjectionMarkdown(t, root, path, "The parent decides milestone meaning, scope, and acceptance.\n")

	violations, err := scopedControlProvenanceViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasControlProjectionViolation(violations, path, "parent-action-staging-admission", "missing its compact control projection") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestControlProjectionProcedureGuardRejectsMissingSurface(t *testing.T) {
	root := newControlProvenanceIdentityRepo(t, controlProjectionTestOrigin, controlProvenanceModulePath)
	writeControlProvenanceGo(t, root, "owner.go", "package fixture\nfunc owner() {}\nfunc postcondition() {}\n")
	writeControlProvenanceGo(t, root, "owner_test.go", "package fixture\nfunc TestOwner() {}\n")
	control := controlProvenanceMachineFixture()
	control.ID = "parent-action-staging-admission"
	control.ProjectionGuards = []controlProvenanceProjectionGuard{{
		Path:            "codex/instructions/missing.md",
		ForbiddenTokens: []string{"start-milestones <token>"},
	}}
	writeControlProvenanceRegistry(t, root, controlProvenanceRegistry{Version: 1, Controls: []controlProvenanceControl{control}})

	violations, err := scopedControlProvenanceViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasControlProjectionViolation(violations, controlProvenanceRegistryPath, "parent-action-staging-admission", "projection surface", "is missing") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestControlProjectionProcedureGuardRejectsNonMachineRegistration(t *testing.T) {
	root := newControlProvenanceIdentityRepo(t, controlProjectionTestOrigin, controlProvenanceModulePath)
	path := "codex/instructions/glm-execution.md"
	writeControlProvenanceRegistry(t, root, controlProvenanceRegistry{
		Version: 1,
		Controls: []controlProvenanceControl{{
			ID:                     "partial-control",
			Classification:         controlClassificationPartial,
			Purpose:                "partial purpose",
			ProjectionGuards:       []controlProvenanceProjectionGuard{{Path: path, ForbiddenTokens: []string{"machine-token"}}},
			ResidualParentJudgment: "parent decides",
			Boundary:               "not fully machine enforced",
		}},
	})
	writeControlProjectionMarkdown(t, root, path, "The parent keeps the unresolved boundary.\n")

	violations, err := scopedControlProvenanceViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasControlProjectionViolation(violations, controlProvenanceRegistryPath, "partial-control", "partial", "instead of machine-enforced") {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestControlProjectionProcedureGuardLeavesNonMachineBoundariesAlone(t *testing.T) {
	for _, classification := range []string{
		controlClassificationPartial,
		controlClassificationProse,
		controlClassificationSemanticParent,
		controlClassificationExternalUnenforced,
	} {
		t.Run(classification, func(t *testing.T) {
			root := newControlProvenanceIdentityRepo(t, controlProjectionTestOrigin, controlProvenanceModulePath)
			writeControlProvenanceRegistry(t, root, controlProvenanceRegistry{
				Version: 1,
				Controls: []controlProvenanceControl{{
					ID:                     "boundary-" + classification,
					Classification:         classification,
					Purpose:                "keep the unresolved boundary explicit",
					ResidualParentJudgment: "the parent owns the unresolved judgment",
					Boundary:               "not fully machine enforced",
				}},
			})
			writeControlProjectionMarkdown(t, root, "codex/instructions/glm-execution.md", "The parent remains responsible for the unresolved boundary.\n")

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

func TestControlProjectionIgnoresTaskRequirementText(t *testing.T) {
	root := newControlProvenanceIdentityRepo(t, controlProjectionTestOrigin, controlProvenanceModulePath)
	writeControlProvenanceGo(t, root, "owner.go", "package fixture\nfunc owner() {}\nfunc postcondition() {}\n")
	writeControlProvenanceGo(t, root, "owner_test.go", "package fixture\nfunc TestOwner() {}\n")
	writeControlProvenanceRegistry(t, root, controlProvenanceRegistry{
		Version:  1,
		Controls: []controlProvenanceControl{controlProvenanceMachineFixture()},
	})
	writeControlProjectionMarkdown(t, root, "IMPLEMENTATION_TASKS/example.md", "`control:not-a-runtime-projection`\n")

	violations, err := scopedControlProvenanceViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations = %#v", violations)
	}
}

func writeControlProjectionMarkdown(t *testing.T, root, path, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func hasControlProjectionViolation(violations []Violation, path string, fragments ...string) bool {
	for _, violation := range violations {
		if violation.Rule != "control-provenance-projection" || violation.Path != path {
			continue
		}
		matched := true
		for _, fragment := range fragments {
			if !strings.Contains(violation.Message, fragment) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}
