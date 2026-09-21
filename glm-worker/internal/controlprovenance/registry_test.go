package controlprovenance

import "testing"

func TestNegativeResultAuthorityFollowsClassification(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		classification          Classification
		withoutMachineRecovery  bool
		withMachineRecovery     bool
	}{
		{name: "machine-enforced", classification: ClassificationMachine, withoutMachineRecovery: false, withMachineRecovery: true},
		{name: "partial", classification: ClassificationPartial, withoutMachineRecovery: true, withMachineRecovery: true},
		{name: "semantic-parent-only", classification: ClassificationSemanticParent, withoutMachineRecovery: true, withMachineRecovery: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authority := ResultAuthority{Classification: tc.classification}
			policy, err := NegativeResultPolicyFor(tc.classification)
			if err != nil {
				t.Fatal(err)
			}
			authority.NegativeResult = policy
			if got := authority.ParentMayPromoteNegativeResult(false); got != tc.withoutMachineRecovery {
				t.Fatalf("without machine recovery = %v want %v", got, tc.withoutMachineRecovery)
			}
			if got := authority.ParentMayPromoteNegativeResult(true); got != tc.withMachineRecovery {
				t.Fatalf("with machine recovery = %v want %v", got, tc.withMachineRecovery)
			}
		})
	}
}

func TestMachineRecoveryDoesNotChooseAmongPositiveActions(t *testing.T) {
	authority := ResultAuthority{
		Classification: ClassificationMachine,
		NegativeResult: NegativeResultMachineRecoveryRequired,
	}
	if !authority.ParentMayPromoteNegativeResult(true) {
		t.Fatal("explicit machine-owned recovery must remain available")
	}
}
