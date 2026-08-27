package workflow

import "testing"

func TestInstructionConflictBoundaryPinsGenericRuleLimits(t *testing.T) {
	boundary := defaultInstructionConflictBoundary()
	if !boundary.valid() {
		t.Fatal("default instruction conflict boundary must be valid")
	}
}

func TestInstructionConflictBoundaryRejectsModelChoiceReintroduction(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*instructionConflictBoundary)
	}{
		{
			name: "generic rule may override primary authority",
			mutate: func(boundary *instructionConflictBoundary) {
				boundary.genericRuleSubordinate = false
			},
		},
		{
			name: "generic rule may expand scope",
			mutate: func(boundary *instructionConflictBoundary) {
				boundary.scopeExpansionForbidden = false
			},
		},
		{
			name: "generic rule may override machine contract",
			mutate: func(boundary *instructionConflictBoundary) {
				boundary.machineOverrideForbidden = false
			},
		},
		{
			name: "generic rule may add public surface",
			mutate: func(boundary *instructionConflictBoundary) {
				boundary.publicSurfaceNeedsPrimary = false
			},
		},
		{
			name: "diagnostic output may use external stream",
			mutate: func(boundary *instructionConflictBoundary) {
				boundary.diagnosticSink = "stdout"
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := defaultInstructionConflictBoundary()
			tc.mutate(&boundary)
			if boundary.valid() {
				t.Fatal("weakened conflict boundary must be invalid")
			}
		})
	}
}

func TestInstructionConflictBoundaryRendersMachineContract(t *testing.T) {
	want := "INSTRUCTION_CONFLICT_BOUNDARY:\n" +
		"GENERIC_RULE_PRECEDENCE: subordinate-to-primary-authority\n" +
		"SCOPE_EXPANSION: forbidden\n" +
		"PUBLIC_SURFACE_EXPANSION: requires-primary-authority\n" +
		"MACHINE_CONTRACT_OVERRIDE: forbidden\n" +
		"DIAGNOSTIC_OUTPUT: internal-state-or-artifact\n" +
		"END_INSTRUCTION_CONFLICT_BOUNDARY\n"
	if got := renderInstructionConflictBoundary(defaultInstructionConflictBoundary()); got != want {
		t.Fatalf("conflict boundary = %q want %q", got, want)
	}
}
