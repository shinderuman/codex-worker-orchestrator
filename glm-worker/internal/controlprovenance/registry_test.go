package controlprovenance

import "testing"

func TestNegativeResultAuthorityFollowsClassification(t *testing.T) {
	tests := []struct {
		name             string
		classification   Classification
		explicitRecovery bool
		want             bool
	}{
		{name: "machine-negative", classification: ClassificationMachine, explicitRecovery: false, want: false},
		{name: "machine-explicit-recovery", classification: ClassificationMachine, explicitRecovery: true, want: true},
		{name: "partial", classification: ClassificationPartial, explicitRecovery: false, want: true},
		{name: "semantic-parent-only", classification: ClassificationSemanticParent, explicitRecovery: false, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParentMayPromoteNegativeResult(tc.classification, tc.explicitRecovery)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("parent promotion = %v want %v", got, tc.want)
			}
		})
	}
}

func TestNegativeResultAuthorityRejectsUnknownClassification(t *testing.T) {
	got, err := ParentMayPromoteNegativeResult("unknown", false)
	if err == nil {
		t.Fatal("unknown classification was accepted")
	}
	if got {
		t.Fatal("unknown classification allowed parent promotion")
	}
}
