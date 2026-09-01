package packet

import (
	"encoding/json"
	"testing"
)

func TestParentValidationEvidenceResolvedFor(t *testing.T) {
	evidence := &ParentValidationEvidence{
		ValidationRunID: "run-pass",
		Form:            ParentValidationGoTest,
		Repository:      "/repo",
		WorkingDir:      "/repo/glm-worker",
		Head:            "head",
		IndexDigest:     "index",
		WorktreeDigest:  "worktree",
		Status:          "pass",
		Log:             "/evidence/run-pass/gate.log",
	}
	if !evidence.ResolvedFor(ParentValidationGoTest) {
		t.Fatal("complete wrapper-owned PASS evidence was not resolved")
	}

	failed := *evidence
	failed.Status = "fail"
	if failed.ResolvedFor(ParentValidationGoTest) {
		t.Fatal("failed evidence was resolved")
	}
	if evidence.ResolvedFor(ParentValidationGoTestRace) {
		t.Fatal("mismatched form was resolved")
	}
	missing := *evidence
	missing.WorktreeDigest = ""
	if missing.ResolvedFor(ParentValidationGoTest) {
		t.Fatal("incomplete snapshot evidence was resolved")
	}
	var absent *ParentValidationEvidence
	if absent.ResolvedFor(ParentValidationGoTest) {
		t.Fatal("nil evidence was resolved")
	}
}

func TestParentValidationEvidenceMachineJSONRoundTrip(t *testing.T) {
	result := Result{
		Status:                     StatusImplemented,
		Risk:                       RiskLow,
		Summary:                    "done",
		RequirementCoverage:        "covered",
		Tests:                      "targeted tests passed",
		Unverified:                 "parent validation was required",
		ParentValidation:           ParentValidationGoTest,
		ParentValidationWorkingDir: "glm-worker",
		ParentValidationEvidence: &ParentValidationEvidence{
			ValidationRunID: "run-pass",
			Form:            ParentValidationGoTest,
			Repository:      "/repo",
			WorkingDir:      "/repo/glm-worker",
			Head:            "head",
			IndexDigest:     "index",
			WorktreeDigest:  "worktree",
			Status:          "pass",
			ExitCode:        0,
			DurationMS:      123,
			Log:             "/evidence/run-pass/gate.log",
		},
	}
	machine, err := result.MachineJSON()
	if err != nil {
		t.Fatal(err)
	}
	var decoded Result
	if err := json.Unmarshal(machine, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ParentValidationEvidence == nil || !decoded.ParentValidationEvidence.ResolvedFor(ParentValidationGoTest) {
		t.Fatalf("round-trip evidence = %#v", decoded.ParentValidationEvidence)
	}
	if decoded.ParentValidationEvidence.Repository != "/repo" || decoded.ParentValidationEvidence.DurationMS != 123 || decoded.ParentValidationEvidence.Log != "/evidence/run-pass/gate.log" {
		t.Fatalf("round-trip lost provenance: %#v", decoded.ParentValidationEvidence)
	}
}
