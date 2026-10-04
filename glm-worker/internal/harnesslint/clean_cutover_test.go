package harnesslint

import (
	"strings"
	"testing"
)

func TestCleanCutoverRejectsRegisteredParentActionRejectionRoots(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, parentActionMetadataPath, `package parentactioncmd

type parentActionExecutionKind uint8
type parentActionCommandDescriptor struct {
	Action string
	Execute parentActionExecutionKind
	TerminalExecute parentActionExecutionKind
}
const (
	parentActionExecutionUnsupported parentActionExecutionKind = iota
	parentActionExecutionCurrent
	parentActionExecutionLegacy
	parentActionExecutionGitEvidence
)
var parentActionCommands = map[string]parentActionCommandDescriptor{
	"current": {Action: "current", Execute: parentActionExecutionCurrent},
	"legacy-kind": {Action: "legacy-kind", Execute: parentActionExecutionLegacy},
	"legacy-action": {Action: "legacy-action", Execute: parentActionExecutionGitEvidence},
}
func executeParentActionCommand(descriptor parentActionCommandDescriptor) error {
	execution := descriptor.Execute
	if err := denyRetiredSurface(execution, descriptor.Action); err != nil {
		return err
	}
	return nil
}
func denyRetiredSurface(kind parentActionExecutionKind, surface string) error {
	if !matchesRetiredSurface(kind, surface) {
		return nil
	}
	return retiredSurfaceError()
}
func matchesRetiredSurface(kind parentActionExecutionKind, surface string) bool {
	switch kind {
	case parentActionExecutionLegacy:
		return true
	case parentActionExecutionGitEvidence:
		return surface == "legacy-action"
	default:
		return false
	}
}
`)
	violations, err := scanCleanCutover(root, []string{parentActionMetadataPath})
	if err != nil {
		t.Fatalf("scanCleanCutover: %v", err)
	}
	if len(violations) != 2 {
		t.Fatalf("violations = %d, want 2: %+v", len(violations), violations)
	}
	for _, violation := range violations {
		if violation.Rule != cleanCutoverRule {
			t.Fatalf("rule = %q, want %q", violation.Rule, cleanCutoverRule)
		}
		if violation.Path != parentActionMetadataPath {
			t.Fatalf("path = %q, want %q", violation.Path, parentActionMetadataPath)
		}
	}
}

func TestCleanCutoverAllowsCurrentParentActionRegistry(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, parentActionMetadataPath, `package parentactioncmd

type parentActionExecutionKind uint8
type parentActionCommandDescriptor struct {
	Action string
	Execute parentActionExecutionKind
}
const parentActionExecutionCurrent parentActionExecutionKind = 1
var parentActionCommands = map[string]parentActionCommandDescriptor{
	"current": {Action: "current", Execute: parentActionExecutionCurrent},
}
func executeParentActionCommand(descriptor parentActionCommandDescriptor) error {
	if err := validateCurrentInput(); err != nil {
		return err
	}
	return nil
}
`)
	violations, err := scanCleanCutover(root, []string{parentActionMetadataPath})
	if err != nil {
		t.Fatalf("scanCleanCutover: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("unexpected violations: %+v", violations)
	}
}

func TestCleanCutoverFixRemovesOnlyRejectedRegistryRootsAndConverges(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, parentActionMetadataPath, `package parentactioncmd

type parentActionExecutionKind uint8
type parentActionCommandDescriptor struct {
	Action string
	Execute parentActionExecutionKind
}
const (
	parentActionExecutionCurrent parentActionExecutionKind = iota
	parentActionExecutionLegacy
)
var parentActionCommands = map[string]parentActionCommandDescriptor{
	"current": {Action: "current", Execute: parentActionExecutionCurrent},
	"legacy": {Action: "legacy", Execute: parentActionExecutionLegacy},
}
func executeParentActionCommand(descriptor parentActionCommandDescriptor) error {
	execution := descriptor.Execute
	if err := cutoverGuard(execution, descriptor.Action); err != nil {
		return err
	}
	return nil
}
func cutoverGuard(kind parentActionExecutionKind, surface string) error {
	if !retiredKind(kind, surface) {
		return nil
	}
	return retiredSurfaceError()
}
func retiredKind(kind parentActionExecutionKind, surface string) bool {
	switch kind {
	case parentActionExecutionLegacy:
		return true
	default:
		return false
	}
}
`)
	paths := []string{parentActionMetadataPath}
	if err := fixCleanCutover(root, paths); err != nil {
		t.Fatalf("fixCleanCutover: %v", err)
	}
	first, err := readRegularFile(root, parentActionMetadataPath)
	if err != nil {
		t.Fatalf("read fixed file: %v", err)
	}
	fixed := string(first)
	if !strings.Contains(fixed, `"current"`) {
		t.Fatalf("current registry entry was removed:\n%s", fixed)
	}
	if strings.Contains(fixed, `"legacy"`) {
		t.Fatalf("rejection-only registry entry remains:\n%s", fixed)
	}
	violations, err := scanCleanCutover(root, paths)
	if err != nil {
		t.Fatalf("scan after fix: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("violations after fix: %+v", violations)
	}
	if err := fixCleanCutover(root, paths); err != nil {
		t.Fatalf("second fixCleanCutover: %v", err)
	}
	second, err := readRegularFile(root, parentActionMetadataPath)
	if err != nil {
		t.Fatalf("read second fixed file: %v", err)
	}
	if string(second) != fixed {
		t.Fatalf("fix did not converge\nfirst:\n%s\nsecond:\n%s", fixed, string(second))
	}
}
