package packet

import (
	"reflect"
	"slices"
	"testing"
)

func TestMachineContractStatusAndRiskVocabulary(t *testing.T) {
	cases := []struct {
		name     string
		contract machineContract
		statuses []Status
		risks    []Risk
		required []machineField
	}{
		{
			name:     "worker",
			contract: workerMachineContract,
			statuses: []Status{StatusImplemented, StatusNeedsSolDecision},
			risks:    []Risk{RiskLow, RiskHigh},
			required: []machineField{fieldStatus, fieldRisk, fieldTargets, fieldArtifacts},
		},
		{
			name:     "reviewer",
			contract: reviewerMachineContract,
			statuses: []Status{StatusPass, StatusFixRequired, StatusNeedsSolReview, StatusNeedsSolDecision},
			risks:    []Risk{RiskLow, RiskHigh},
			required: []machineField{fieldStatus, fieldRisk, fieldTargets, fieldArtifacts},
		},
		{
			name:     "high-floor reviewer",
			contract: highFloorReviewerMachineContract,
			statuses: []Status{StatusFixRequired, StatusNeedsSolReview, StatusNeedsSolDecision},
			risks:    []Risk{RiskLow, RiskHigh},
			required: []machineField{fieldStatus, fieldRisk, fieldTargets, fieldArtifacts},
		},
		{
			name:     "risk-floor reviewer",
			contract: riskFloorReviewerMachineContract,
			statuses: []Status{StatusNeedsSolReview},
			risks:    []Risk{RiskHigh},
			required: []machineField{
				fieldStatus,
				fieldRisk,
				fieldSummary,
				fieldRequirementCoverage,
				fieldInvariants,
				fieldTestEvidence,
				fieldIssues,
				fieldResidualRisk,
				fieldSolQuestion,
				fieldTargets,
				fieldArtifacts,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !slices.Equal(tc.contract.statuses, tc.statuses) {
				t.Fatalf("statuses = %v, want %v", tc.contract.statuses, tc.statuses)
			}
			if got := machineContractRisks(tc.contract); !slices.Equal(got, tc.risks) {
				t.Fatalf("risks = %v, want %v", got, tc.risks)
			}
			if got := schemaRequiredFields(tc.contract); !slices.Equal(got, tc.required) {
				t.Fatalf("required = %v, want %v", got, tc.required)
			}
		})
	}
}

func TestMachineContractStatusResultFields(t *testing.T) {
	cases := map[Status][]machineField{
		StatusImplemented: {
			fieldSummary,
			fieldRequirementCoverage,
			fieldTests,
			fieldUnverified,
		},
		StatusNeedsSolDecision: {
			fieldDecision,
			fieldEvidence,
			fieldOptions,
			fieldRecommendation,
			fieldTestObligations,
		},
		StatusPass: {
			fieldSummary,
			fieldRequirementCoverage,
			fieldInvariants,
			fieldTestEvidence,
			fieldIssues,
			fieldResidualRisk,
		},
		StatusFixRequired: {
			fieldSummary,
			fieldRequirementCoverage,
			fieldInvariants,
			fieldTestEvidence,
			fieldIssues,
			fieldResidualRisk,
		},
		StatusNeedsSolReview: {
			fieldSummary,
			fieldRequirementCoverage,
			fieldInvariants,
			fieldTestEvidence,
			fieldIssues,
			fieldResidualRisk,
			fieldSolQuestion,
		},
	}
	for status, want := range cases {
		if got := resultFieldsForStatus(status); !slices.Equal(got, want) {
			t.Fatalf("%s result fields = %v, want %v", status, got, want)
		}
	}
}

func TestModelMachineFieldVocabularyMatchesResultJSONTags(t *testing.T) {
	resultType := reflect.TypeOf(Result{})
	tags := make(map[string]struct{}, resultType.NumField())
	for i := 0; i < resultType.NumField(); i++ {
		name := resultType.Field(i).Tag.Get("json")
		if comma := indexByte(name, ','); comma >= 0 {
			name = name[:comma]
		}
		if name != "" && name != "-" {
			tags[name] = struct{}{}
		}
	}
	for field := range modelFieldContracts {
		if _, ok := tags[string(field)]; !ok {
			t.Fatalf("canonical machine field %q has no Result JSON field", field)
		}
	}
	if _, ok := tags[string(fieldParentValidationEvidence)]; !ok {
		t.Fatalf("wrapper-owned machine field %q has no Result JSON field", fieldParentValidationEvidence)
	}
}

func indexByte(value string, target byte) int {
	for i := 0; i < len(value); i++ {
		if value[i] == target {
			return i
		}
	}
	return -1
}

func TestTerminalSemanticFieldsFollowPacketContract(t *testing.T) {
	cases := []struct {
		status Status
		extras []string
	}{
		{StatusImplemented, []string{"parent_validation", "parent_validation_working_dir", "parent_validation_evidence"}},
		{StatusNeedsSolDecision, []string{"failure_path_advisory"}},
		{StatusPass, []string{"failure_path_advisory"}},
		{StatusFixRequired, nil},
		{StatusNeedsSolReview, []string{"failure_path_advisory"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			fields := TerminalSemanticFields(tc.status)
			want := []string{"status", "risk"}
			for _, field := range packetStatusContracts[tc.status].resultFields {
				want = append(want, string(field))
			}
			want = append(want, tc.extras...)
			want = append(want, "targets", "artifacts")
			if !slices.Equal(fields, want) {
				t.Fatalf("terminal fields = %v, want %v", fields, want)
			}
			seen := map[string]bool{}
			for _, field := range fields {
				if seen[field] {
					t.Fatalf("duplicate packet field %q", field)
				}
				seen[field] = true
			}
			fields[0] = "mutated"
			if got := TerminalSemanticFields(tc.status)[0]; got != "status" {
				t.Fatalf("packet projection contract was mutated: %q", got)
			}
		})
	}
	if got := TerminalSemanticFields(Status("unknown")); got != nil {
		t.Fatalf("unknown status has terminal fields: %v", got)
	}
}
