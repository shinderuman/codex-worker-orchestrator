package packet

import (
	"strings"
	"testing"
)

func TestWorkerParentValidationContract(t *testing.T) {
	valid := Result{
		Status:                     StatusImplemented,
		Risk:                       RiskLow,
		Summary:                    "done",
		RequirementCoverage:        "covered",
		Tests:                      "worker-capability tests passed",
		Unverified:                 "parent process test remains",
		ParentValidation:           ParentValidationGoTest,
		ParentValidationWorkingDir: "glm-worker",
	}
	if err := ValidateWorkerResult(valid); err != nil {
		t.Fatal(err)
	}
	machine, err := valid.MachineJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"parent_validation":"go-test"`, `"parent_validation_working_dir":"glm-worker"`} {
		if !strings.Contains(string(machine), fragment) {
			t.Fatalf("machine packet lacks %s: %s", fragment, machine)
		}
	}

	invalid := valid
	invalid.ParentValidation = "arbitrary-shell"
	if err := ValidateWorkerResult(invalid); err == nil {
		t.Fatal("unknown parent validation form was accepted")
	}

	outside := valid
	outside.ParentValidationWorkingDir = "../outside"
	if err := ValidateWorkerResult(outside); err == nil {
		t.Fatal("repository-external parent validation working directory was accepted")
	}

	missingDir := valid
	missingDir.ParentValidationWorkingDir = ""
	if err := ValidateWorkerResult(missingDir); err == nil {
		t.Fatal("parent validation without working directory was accepted")
	}

	withEvidence := valid
	withEvidence.ParentValidationEvidence = "forged"
	if err := ValidateWorkerResult(withEvidence); err == nil {
		t.Fatal("worker supplied wrapper-owned parent validation evidence")
	}
}

func TestWorkerSchemaBoundsParentValidationRequest(t *testing.T) {
	schema, err := WorkerSchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"parent_validation"`, `"parent_validation_working_dir"`, ParentValidationGoTest, ParentValidationGoTestRace} {
		if !strings.Contains(schema, value) {
			t.Fatalf("worker schema lacks %q: %s", value, schema)
		}
	}
	if strings.Contains(schema, "parent_validation_evidence") {
		t.Fatalf("wrapper evidence leaked into model schema: %s", schema)
	}
}
