package controller

import "testing"

func TestExecutionOperationContractsCoverCurrentKinds(t *testing.T) {
	store := &Store{}
	cases := []struct {
		kind    string
		effects executionOperationEffectPolicy
	}{
		{executionModelCall, executionOperationEffectsNone},
		{terminalRetire, executionOperationEffectsOneOrMore},
		{executionSuspend, executionOperationEffectsSingle},
		{executionMaterialize, executionOperationEffectsSingle},
		{executionCleanup, executionOperationEffectsSingle},
		{executionGC, executionOperationEffectsSingle},
		{publicationAccept, executionOperationEffectsOneOrMore},
		{publicationPromote, executionOperationEffectsOneOrMore},
		{publicationPublish, executionOperationEffectsOneOrMore},
		{publicationRebind, executionOperationEffectsOneOrMore},
		{publicationAdopt, executionOperationEffectsOneOrMore},
		{publicationRevalidate, executionOperationEffectsOneOrMore},
		{publicationReenter, executionOperationEffectsOneOrMore},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			contract, err := store.executionOperationContract(tc.kind)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateExecutionOperationContract(contract); err != nil {
				t.Fatal(err)
			}
			if contract.effects != tc.effects {
				t.Fatalf("effects = %v, want %v", contract.effects, tc.effects)
			}
		})
	}
}

func TestExecutionOperationContractRejectsUnknownKind(t *testing.T) {
	store := &Store{}
	if _, err := store.executionOperationContract("fixture-unknown"); err == nil {
		t.Fatal("unknown execution operation was admitted")
	}
}

func TestFixtureExecutionOperationUsesOneContractBoundary(t *testing.T) {
	calls := []string{}
	contract := executionOperationContract{
		effects: executionOperationEffectsSingle,
		validate: func(ExecutionOperation) error {
			calls = append(calls, "validate")
			return nil
		},
		apply: func(ExecutionOperation) error {
			calls = append(calls, "apply")
			return nil
		},
		verifyCommitted: func(ExecutionOperation, RepositoryControllerHead) error {
			calls = append(calls, "verify")
			return nil
		},
		projectResult: func(ExecutionOperation, RepositoryControllerHead, *ExecutionOperationResult) error {
			calls = append(calls, "result")
			return nil
		},
	}
	if err := validateExecutionOperationContract(contract); err != nil {
		t.Fatal(err)
	}
	op := ExecutionOperation{Transition: TransitionRecord{Effects: []EffectExpectation{{}}}}
	if err := validateExecutionOperationEffects(op, contract.effects); err != nil {
		t.Fatal(err)
	}
	if err := contract.validate(op); err != nil {
		t.Fatal(err)
	}
	if err := contract.apply(op); err != nil {
		t.Fatal(err)
	}
	if err := contract.verifyCommitted(op, RepositoryControllerHead{}); err != nil {
		t.Fatal(err)
	}
	if err := contract.projectResult(op, RepositoryControllerHead{}, &ExecutionOperationResult{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"validate", "apply", "verify", "result"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}
