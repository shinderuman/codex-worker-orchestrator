package harnesslint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controlprovenance"
)

type controlProvenanceRegistry = controlprovenance.Registry
type controlProvenanceControl = controlprovenance.Control
type controlProvenanceLocator = controlprovenance.Locator
type controlProvenanceProjectionGuard = controlprovenance.ProjectionGuard
type controlProvenanceClassification = controlprovenance.Classification

const controlProvenanceRegistryPath = controlprovenance.RegistryPath

const (
	controlClassificationMachine            = controlprovenance.ClassificationMachine
	controlClassificationPartial            = controlprovenance.ClassificationPartial
	controlClassificationProse              = controlprovenance.ClassificationProse
	controlClassificationSemanticParent     = controlprovenance.ClassificationSemanticParent
	controlClassificationExternalUnenforced = controlprovenance.ClassificationExternalUnenforced

	parentActionMachineProjectionControlID = "parent-action-machine-projection"
	parentActionGrammarOwnerPath           = "glm-worker/internal/parentactiongrammar/grammar.go"
	parentActionGrammarOwnerSymbol         = "Project"
	publicationGuardSetupControlID         = "publication-guard-setup"
)

func controlProvenanceViolations(root string) ([]Violation, error) {
	registryPath := filepath.Join(root, filepath.FromSlash(controlProvenanceRegistryPath))
	data, err := os.ReadFile(registryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []Violation{controlProvenanceViolation(controlProvenanceRegistryPath, "machine control provenance registry is missing")}, nil
		}
		return nil, fmt.Errorf("read %s: %w", controlProvenanceRegistryPath, err)
	}

	registry, err := decodeControlProvenanceRegistry(data)
	if err != nil {
		return []Violation{controlProvenanceViolation(controlProvenanceRegistryPath, err.Error())}, nil
	}

	return validateControlProvenanceRegistry(root, registry)
}

func decodeControlProvenanceRegistry(data []byte) (controlProvenanceRegistry, error) {
	registry, err := controlprovenance.Decode(data)
	if err != nil {
		return controlProvenanceRegistry{}, fmt.Errorf("decode control provenance registry: %w", err)
	}
	return registry, nil
}

func validateControlProvenanceRegistry(root string, registry controlProvenanceRegistry) ([]Violation, error) {
	var violations []Violation
	if registry.Version != 1 {
		violations = append(violations, controlProvenanceViolation(controlProvenanceRegistryPath, fmt.Sprintf("unsupported control provenance registry version: %d", registry.Version)))
	}
	if len(registry.Controls) == 0 {
		violations = append(violations, controlProvenanceViolation(controlProvenanceRegistryPath, "control provenance registry has no controls"))
		return violations, nil
	}

	seen := make(map[string]struct{}, len(registry.Controls))
	lastID := ""
	for _, control := range registry.Controls {
		if control.ID == "" {
			violations = append(violations, controlProvenanceViolation(controlProvenanceRegistryPath, "control provenance entry has an empty id"))
			continue
		}
		if _, duplicate := seen[control.ID]; duplicate {
			violations = append(violations, controlProvenanceViolation(controlProvenanceRegistryPath, fmt.Sprintf("control %q is registered more than once", control.ID)))
			continue
		}
		seen[control.ID] = struct{}{}
		if lastID != "" && control.ID < lastID {
			violations = append(violations, controlProvenanceViolation(controlProvenanceRegistryPath, fmt.Sprintf("control %q is out of stable id order", control.ID)))
		}
		lastID = control.ID
		violations = append(violations, validateControlProvenanceControl(root, control)...)
	}
	violations = append(violations, validateKnownMachineControlCoverage(root, registry.Controls)...)
	return violations, nil
}

func validateKnownMachineControlCoverage(root string, controls []controlProvenanceControl) []Violation {
	known := []struct {
		id                   string
		ownerPath            string
		canonicalOwnerSymbol string
	}{
		{id: forwardOnlyCompatibilityRule, ownerPath: "glm-worker/internal/harnesslint/forward_only_test_surface_usage.go"},
		{id: parentActionMachineProjectionControlID, ownerPath: parentActionGrammarOwnerPath, canonicalOwnerSymbol: parentActionGrammarOwnerSymbol},
		{id: publicationGuardSetupControlID, ownerPath: "glm-worker/internal/publicationguard/setup.go"},
	}

	registered := make(map[string]controlProvenanceControl, len(controls))
	for _, control := range controls {
		if _, exists := registered[control.ID]; !exists {
			registered[control.ID] = control
		}
	}

	var violations []Violation
	for _, knownControl := range known {
		absolute := filepath.Join(root, filepath.FromSlash(knownControl.ownerPath))
		info, err := os.Stat(absolute)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			violations = append(violations, controlProvenanceViolation(controlProvenanceRegistryPath, fmt.Sprintf("cannot inspect current machine control %q owner %q: %v", knownControl.id, knownControl.ownerPath, err)))
			continue
		}
		if !info.Mode().IsRegular() {
			violations = append(violations, controlProvenanceViolation(controlProvenanceRegistryPath, fmt.Sprintf("current machine control %q owner %q is not a regular file", knownControl.id, knownControl.ownerPath)))
			continue
		}
		control, ok := registered[knownControl.id]
		if !ok {
			violations = append(violations, controlProvenanceViolation(controlProvenanceRegistryPath, fmt.Sprintf("current machine control %q is missing from provenance registry", knownControl.id)))
			continue
		}
		if knownControl.canonicalOwnerSymbol != "" && !hasControlProvenanceLocator(control.MachineOwners, knownControl.ownerPath, knownControl.canonicalOwnerSymbol) {
			violations = append(violations, controlProvenanceViolation(controlProvenanceRegistryPath, fmt.Sprintf("current machine control %q canonical owner %q::%s is missing from provenance machine owners", knownControl.id, knownControl.ownerPath, knownControl.canonicalOwnerSymbol)))
		}
	}
	return violations
}

func hasControlProvenanceLocator(locators []controlProvenanceLocator, locatorPath, symbol string) bool {
	for _, locator := range locators {
		if locator.Path == locatorPath && locator.Symbol == symbol {
			return true
		}
	}
	return false
}

func validateControlProvenanceControl(root string, control controlProvenanceControl) []Violation {
	var violations []Violation
	if strings.TrimSpace(control.Purpose) == "" {
		violations = append(violations, controlProvenanceViolation(controlProvenanceRegistryPath, fmt.Sprintf("control %q has no purpose", control.ID)))
	}
	if strings.TrimSpace(control.ResidualParentJudgment) == "" {
		violations = append(violations, controlProvenanceViolation(controlProvenanceRegistryPath, fmt.Sprintf("control %q has no residual parent judgment", control.ID)))
	}

	switch control.Classification {
	case controlClassificationMachine:
		violations = append(violations, validateFullyMachineControl(root, control)...)
	case controlClassificationPartial:
		violations = append(violations, validatePartialControl(root, control)...)
	case controlClassificationProse, controlClassificationSemanticParent, controlClassificationExternalUnenforced:
		violations = append(violations, validateResidualOnlyControl(control)...)
	default:
		violations = append(violations, controlProvenanceViolation(controlProvenanceRegistryPath, fmt.Sprintf("control %q has unsupported classification %q", control.ID, control.Classification)))
	}
	return violations
}

func validateFullyMachineControl(root string, control controlProvenanceControl) []Violation {
	var violations []Violation
	if strings.TrimSpace(control.Boundary) != "" {
		violations = append(violations, controlProvenanceViolation(controlProvenanceRegistryPath, fmt.Sprintf("machine-enforced control %q must not declare a non-machine boundary", control.ID)))
	}
	return append(violations, validateMachineControlLocators(root, control)...)
}

func validatePartialControl(root string, control controlProvenanceControl) []Violation {
	var violations []Violation
	if strings.TrimSpace(control.Boundary) == "" {
		violations = append(violations, controlProvenanceViolation(controlProvenanceRegistryPath, fmt.Sprintf("partial control %q must declare its residual enforcement boundary", control.ID)))
	}
	if len(control.ProjectionGuards) != 0 {
		violations = append(violations, controlProvenanceViolation(controlProvenanceRegistryPath, fmt.Sprintf("partial control %q must not carry projection guard metadata", control.ID)))
	}
	return append(violations, validateMachineControlLocators(root, control)...)
}

func validateResidualOnlyControl(control controlProvenanceControl) []Violation {
	var violations []Violation
	if strings.TrimSpace(control.Boundary) == "" {
		violations = append(violations, controlProvenanceViolation(controlProvenanceRegistryPath, fmt.Sprintf("non-machine control %q must declare its enforcement boundary", control.ID)))
	}
	if len(control.MachineOwners) != 0 || len(control.Tests) != 0 || len(control.Postconditions) != 0 || len(control.ProjectionGuards) != 0 {
		violations = append(violations, controlProvenanceViolation(controlProvenanceRegistryPath, fmt.Sprintf("non-machine control %q must not carry machine owner, test, postcondition, or projection guard metadata", control.ID)))
	}
	return violations
}

func validateMachineControlLocators(root string, control controlProvenanceControl) []Violation {
	groups := []struct {
		name     string
		locators []controlProvenanceLocator
		testOnly bool
	}{
		{name: "machine owner", locators: control.MachineOwners},
		{name: "test", locators: control.Tests, testOnly: true},
		{name: "postcondition", locators: control.Postconditions},
	}
	var violations []Violation
	for _, group := range groups {
		if len(group.locators) == 0 {
			violations = append(violations, controlProvenanceViolation(controlProvenanceRegistryPath, fmt.Sprintf("control %q has no %s locator", control.ID, group.name)))
			continue
		}
		for _, locator := range group.locators {
			violations = append(violations, validateControlProvenanceLocator(root, control.ID, group.name, locator, group.testOnly)...)
		}
	}
	return violations
}

func validateControlProvenanceLocator(root, controlID, kind string, locator controlProvenanceLocator, testOnly bool) []Violation {
	if locator.Path == "" || locator.Symbol == "" {
		return []Violation{controlProvenanceViolation(controlProvenanceRegistryPath, fmt.Sprintf("control %q has an incomplete %s locator", controlID, kind))}
	}
	if !validControlProvenancePath(locator.Path) {
		return []Violation{controlProvenanceViolation(controlProvenanceRegistryPath, fmt.Sprintf("control %q has invalid %s path %q", controlID, kind, locator.Path))}
	}
	if testOnly != strings.HasSuffix(locator.Path, "_test.go") {
		expectation := "production Go file"
		if testOnly {
			expectation = "*_test.go file"
		}
		return []Violation{controlProvenanceViolation(controlProvenanceRegistryPath, fmt.Sprintf("control %q %s locator %q must reference a %s", controlID, kind, locator.Path, expectation))}
	}

	absolute := filepath.Join(root, filepath.FromSlash(locator.Path))
	file, err := parser.ParseFile(token.NewFileSet(), absolute, nil, parser.SkipObjectResolution)
	if err != nil {
		if os.IsNotExist(err) {
			return []Violation{controlProvenanceViolation(locator.Path, fmt.Sprintf("control %q %s file is missing", controlID, kind))}
		}
		return []Violation{controlProvenanceViolation(locator.Path, fmt.Sprintf("control %q %s locator cannot parse file: %v", controlID, kind, err))}
	}
	if !goFileDefinesSymbol(file, locator.Symbol) {
		return []Violation{controlProvenanceViolation(locator.Path, fmt.Sprintf("control %q %s symbol %q is missing or renamed", controlID, kind, locator.Symbol))}
	}
	return nil
}

func validControlProvenancePath(value string) bool {
	if value == "" || strings.Contains(value, "\\") || path.IsAbs(value) || path.Clean(value) != value {
		return false
	}
	parts := strings.Split(value, "/")
	for _, part := range parts[:len(parts)-1] {
		if part == "testdata" || strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_") {
			return false
		}
	}
	return strings.HasSuffix(value, ".go") && value != ".." && !strings.HasPrefix(value, "../")
}

func goFileDefinesSymbol(file *ast.File, symbol string) bool {
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if function.Recv == nil {
			if function.Name.Name == symbol {
				return true
			}
			continue
		}
		if len(function.Recv.List) != 1 {
			continue
		}
		receiver := receiverTypeName(function.Recv.List[0].Type)
		if receiver != "" && receiver+"."+function.Name.Name == symbol {
			return true
		}
	}
	return false
}

func receiverTypeName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return receiverTypeName(value.X)
	case *ast.IndexExpr:
		return receiverTypeName(value.X)
	case *ast.IndexListExpr:
		return receiverTypeName(value.X)
	default:
		return ""
	}
}

func controlProvenanceViolation(path, message string) Violation {
	return Violation{
		Rule:    "control-provenance",
		Path:    path,
		Line:    1,
		Column:  1,
		Message: message,
	}
}
