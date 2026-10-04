package harnesslint

import "testing"

func TestCleanCutoverRejectsRegisteredParentActionRejectionRoots(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, parentActionMetadataPath, `package parentactioncmd

type parentActionExecutionKind uint8
type parentActionCommandDescriptor struct {
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
	"current": {Execute: parentActionExecutionCurrent},
	"legacy-kind": {Execute: parentActionExecutionLegacy},
	"legacy-action": {Execute: parentActionExecutionGitEvidence},
}
func isLegacyParentActionInvocation(execution parentActionExecutionKind, action string) bool {
	switch execution {
	case parentActionExecutionLegacy:
		return true
	case parentActionExecutionGitEvidence:
		return action == "legacy-action"
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
type parentActionCommandDescriptor struct { Execute parentActionExecutionKind }
const parentActionExecutionCurrent parentActionExecutionKind = 1
var parentActionCommands = map[string]parentActionCommandDescriptor{
	"current": {Execute: parentActionExecutionCurrent},
}
func isLegacyParentActionInvocation(execution parentActionExecutionKind, action string) bool {
	return false
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
