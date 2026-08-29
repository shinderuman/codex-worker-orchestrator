package packet

import (
	"strings"
	"testing"
)

func TestWorkerParentValidationContract(t *testing.T) {
	valid := Result{
		Status:              StatusImplemented,
		Risk:                RiskLow,
		Summary:             "done",
		RequirementCoverage: "covered",
		Tests:               "worker-capability tests passed",
		Unverified:          "parent process test remains",
		ParentValidation:    ParentValidationGoTest,
	}
	if err := ValidateWorkerResult(valid); err != nil {
		t.Fatal(err)
	}
	machine, err := valid.MachineJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(machine), `"parent_validation":"go-test"`) {
		t.Fatalf("machine packet = %s", machine)
	}

	invalid := valid
	invalid.ParentValidation = "arbitrary-shell"
	if err := ValidateWorkerResult(invalid); err == nil {
		t.Fatal("unknown parent validation form was accepted")
	}

	withEvidence := valid
	withEvidence.ParentValidationEvidence = "forged"
	if err := ValidateWorkerResult(withEvidence); err == nil {
		t.Fatal("worker supplied wrapper-owned parent validation evidence")
	}
}

func TestWorkerSchemaBoundsParentValidationForms(t *testing.T) {
	schema, err := WorkerSchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"parent_validation"`, ParentValidationGoTest, ParentValidationGoTestRace} {
		if !strings.Contains(schema, value) {
			t.Fatalf("worker schema lacks %q: %s", value, schema)
		}
	}
	if strings.Contains(schema, "parent_validation_evidence") {
		t.Fatalf("wrapper evidence leaked into model schema: %s", schema)
	}
}
