package workflow

import "strings"

type instructionConflictBoundary struct {
	genericRuleSubordinate    bool
	scopeExpansionForbidden   bool
	machineOverrideForbidden  bool
	publicSurfaceNeedsPrimary bool
	diagnosticSink            string
}

const diagnosticSinkInternal = "internal-state-or-artifact"

func defaultInstructionConflictBoundary() instructionConflictBoundary {
	return instructionConflictBoundary{
		genericRuleSubordinate:    true,
		scopeExpansionForbidden:   true,
		machineOverrideForbidden:  true,
		publicSurfaceNeedsPrimary: true,
		diagnosticSink:            diagnosticSinkInternal,
	}
}

func (b instructionConflictBoundary) valid() bool {
	return b.genericRuleSubordinate &&
		b.scopeExpansionForbidden &&
		b.machineOverrideForbidden &&
		b.publicSurfaceNeedsPrimary &&
		b.diagnosticSink == diagnosticSinkInternal
}

func renderInstructionConflictBoundary(boundary instructionConflictBoundary) string {
	if !boundary.valid() {
		panic("invalid instruction conflict boundary")
	}
	var block strings.Builder
	block.WriteString("INSTRUCTION_CONFLICT_BOUNDARY:\n")
	block.WriteString("GENERIC_RULE_PRECEDENCE: subordinate-to-primary-authority\n")
	block.WriteString("SCOPE_EXPANSION: forbidden\n")
	block.WriteString("PUBLIC_SURFACE_EXPANSION: requires-primary-authority\n")
	block.WriteString("MACHINE_CONTRACT_OVERRIDE: forbidden\n")
	block.WriteString("DIAGNOSTIC_OUTPUT: ")
	block.WriteString(boundary.diagnosticSink)
	block.WriteString("\nEND_INSTRUCTION_CONFLICT_BOUNDARY\n")
	return block.String()
}
