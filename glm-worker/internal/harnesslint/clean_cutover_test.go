package harnesslint

import (
	"strings"
	"testing"
)

const currentParentActionCutoverFixture = `package parentactioncmd

type parentActionExecutionKind uint8
type parentActionCommandDescriptor struct {
	Action string
	Execute parentActionExecutionKind
	TerminalExecute parentActionExecutionKind
}
const (
	parentActionExecutionUnsupported parentActionExecutionKind = iota
	parentActionExecutionCurrent
	parentActionExecutionSpecial
	parentActionExecutionRetired
)
var parentActionCommands = map[string]parentActionCommandDescriptor{
	"current": {Action: "current", Execute: parentActionExecutionCurrent},
	"special": {Action: "special", Execute: parentActionExecutionSpecial},
}
func executeParentActionCommand(execution parentActionExecutionKind) error {
	if err, handled := executeStandardParentAction(execution); handled {
		return err
	}
	return executeSpecialParentAction(execution)
}
func executeStandardParentAction(execution parentActionExecutionKind) (error, bool) {
	return executeInterfaceParentAction(execution)
}
func executeInterfaceParentAction(execution parentActionExecutionKind) {
	switch execution {
	case parentActionExecutionCurrent:
		return executeCurrent(), true
	default:
		return nil, false
	}
}
func executeSpecialParentAction(execution parentActionExecutionKind) {
	if execution == parentActionExecutionSpecial {
		return executeSpecial()
	}
	return usageError()
}
`

func TestCleanCutoverRejectsRegisteredParentActionRejectionRoots(t *testing.T) {
	const current = `"special": {Action: "special", Execute: parentActionExecutionSpecial},`

	t.Run("unsupported tombstone", func(t *testing.T) {
		source := strings.Replace(currentParentActionCutoverFixture, current, `"special": {Action: "special", Execute: parentActionExecutionUnsupported},`, 1)
		checkCleanCutoverViolation(t, source, "no live executable dispatch")
	})
	t.Run("registered kind with no dispatch", func(t *testing.T) {
		source := strings.Replace(currentParentActionCutoverFixture, current, `"retired": {Action: "retired", Execute: parentActionExecutionRetired},`, 1)
		checkCleanCutoverViolation(t, source, "no live executable dispatch")
	})
	t.Run("error only dispatch", func(t *testing.T) {
		source := strings.Replace(currentParentActionCutoverFixture, current, `"retired": {Action: "retired", Execute: parentActionExecutionRetired},`, 1)
		source = strings.Replace(source, "return usageError()", "if execution == parentActionExecutionRetired { return retiredError() }; return usageError()", 1)
		checkCleanCutoverViolation(t, source, "no live executable dispatch")
	})
	t.Run("retired alias mismatch", func(t *testing.T) {
		source := strings.Replace(currentParentActionCutoverFixture, current, `"retired": {Action: "special", Execute: parentActionExecutionSpecial},`, 1)
		checkCleanCutoverViolation(t, source, "same canonical action")
	})
	t.Run("terminal tombstone", func(t *testing.T) {
		source := strings.Replace(currentParentActionCutoverFixture, current, `"special": {Action: "special", Execute: parentActionExecutionSpecial, TerminalExecute: parentActionExecutionRetired},`, 1)
		checkCleanCutoverViolation(t, source, "no live terminal dispatch")
	})
}

func checkCleanCutoverViolation(t *testing.T, source, message string) {
	t.Helper()
	root := fixtureRoot(t)
	writeFixture(t, root, parentActionMetadataPath, source)
	violations, err := scanCleanCutover(root, []string{parentActionMetadataPath})
	if err != nil {
		t.Fatalf("scanCleanCutover: %v", err)
	}
	if len(violations) != 1 || violations[0].Rule != cleanCutoverRule ||
		violations[0].Path != parentActionMetadataPath ||
		!strings.Contains(violations[0].Message, message) {
		t.Fatalf("violations = %+v, want one %q rule", violations, message)
	}
}

func TestCleanCutoverAllowsCurrentParentActionRegistry(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, parentActionMetadataPath, currentParentActionCutoverFixture)
	violations, err := scanCleanCutover(root, []string{parentActionMetadataPath})
	if err != nil {
		t.Fatalf("scanCleanCutover: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("unexpected violations: %+v", violations)
	}
}

func TestCleanCutoverRejectsUnprovenDispatch(t *testing.T) {
	for _, tc := range []struct {
		name string
		from string
		to   string
	}{
		{"missing dispatch", "return executeSpecialParentAction(execution)", "return missingSpecialDispatch()"},
		{"disconnected dispatch", "return executeSpecialParentAction(execution)", "if false { return executeSpecialParentAction(execution) }; return usageError()"},
		{"ignored dispatch result", "return executeInterfaceParentAction(execution)", "executeInterfaceParentAction(execution); return nil, false"},
		{"early return shadows interface", "return executeInterfaceParentAction(execution)", "if execution == parentActionExecutionCurrent { return nil, false }; return executeInterfaceParentAction(execution)"},
		{"wrong interface argument", "return executeInterfaceParentAction(execution)", "return executeInterfaceParentAction(parentActionExecutionSpecial)"},
		{"wrong special argument", "return executeSpecialParentAction(execution)", "return executeSpecialParentAction(parentActionExecutionCurrent)"},
		{"wrong standard argument", "executeStandardParentAction(execution); handled", "executeStandardParentAction(parentActionExecutionSpecial); handled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := strings.Replace(currentParentActionCutoverFixture, tc.from, tc.to, 1)
			checkCleanCutoverViolation(t, source, "not provable")
		})
	}
}

func TestCleanCutoverLintIsReportOnly(t *testing.T) {
	root := fixtureRoot(t)
	source := strings.Replace(currentParentActionCutoverFixture,
		"\"special\": {Action: \"special\", Execute: parentActionExecutionSpecial},",
		"\"special\": {Action: \"special\", Execute: parentActionExecutionUnsupported},", 1)
	writeFixture(t, root, parentActionMetadataPath, source)
	if violations, err := scanCleanCutover(root, []string{parentActionMetadataPath}); err != nil || len(violations) != 1 {
		t.Fatalf("scan before unchanged source: %+v, %v", violations, err)
	}
	after, err := readRegularFile(root, parentActionMetadataPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != source {
		t.Fatal("report-only lint modified the source")
	}
}
