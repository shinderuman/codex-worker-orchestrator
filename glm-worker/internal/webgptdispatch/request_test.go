package webgptdispatch

import (
	"strings"
	"testing"
)

func TestParseMailboxAcceptsBoundedAutofixRequest(t *testing.T) {
	request, err := ParseMailbox(strings.NewReader(`{
		"version": 1,
		"request_id": "req-20261001-001",
		"operation": "autofix",
		"target_branch": "web-gpt/example",
		"expected_head_sha": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if request.ExpectedHeadSHA != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("expected normalized SHA, got %q", request.ExpectedHeadSHA)
	}
}

func TestParseMailboxRejectsUnknownField(t *testing.T) {
	_, err := ParseMailbox(strings.NewReader(`{
		"version": 1,
		"request_id": "req-1",
		"operation": "autofix",
		"target_branch": "web-gpt/example",
		"expected_head_sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"command": "arbitrary"
	}`))
	assertValidationCode(t, err, "invalid_json")
}

func TestParseMailboxRejectsTrailingJSON(t *testing.T) {
	_, err := ParseMailbox(strings.NewReader(`{"version":1,"request_id":"req-1","operation":"autofix","target_branch":"web-gpt/example","expected_head_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {}`))
	assertValidationCode(t, err, "trailing_json")
}

func TestValidateOperationRejectsUnsafeBranchShapes(t *testing.T) {
	for _, branch := range []string{
		"main",
		"web-gpt/../main",
		"web-gpt//nested",
		"web-gpt/.hidden",
		"web-gpt/branch.lock",
		"web-gpt/trailing.",
		"web-gpt/branch with-space",
	} {
		err := ValidateOperation(OperationRequest{
			Operation:       OperationAutofix,
			TargetBranch:    branch,
			ExpectedHeadSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		})
		assertValidationCode(t, err, "invalid_target_branch")
	}
}

func TestValidateOperationEnforcesValidationModeAndScope(t *testing.T) {
	valid := []OperationRequest{
		{Operation: OperationValidate, TargetBranch: "web-gpt/example", ExpectedHeadSHA: strings.Repeat("a", 40), ValidationMode: ValidationRepositoryLint},
		{Operation: OperationValidate, TargetBranch: "web-gpt/example", ExpectedHeadSHA: strings.Repeat("b", 40), ValidationMode: ValidationFullGoTest},
		{Operation: OperationValidate, TargetBranch: "web-gpt/example", ExpectedHeadSHA: strings.Repeat("c", 40), ValidationMode: ValidationBuildVet},
		{Operation: OperationValidate, TargetBranch: "web-gpt/example", ExpectedHeadSHA: strings.Repeat("d", 40), ValidationMode: ValidationGoPackageTest, PackageScope: "./internal/workflow"},
	}
	for _, request := range valid {
		if err := ValidateOperation(request); err != nil {
			t.Fatalf("valid request rejected: %+v: %v", request, err)
		}
	}

	invalid := []struct {
		request OperationRequest
		code    string
	}{
		{OperationRequest{Operation: OperationValidate, TargetBranch: "web-gpt/example", ExpectedHeadSHA: strings.Repeat("a", 40), ValidationMode: "arbitrary"}, "invalid_validation_mode"},
		{OperationRequest{Operation: OperationValidate, TargetBranch: "web-gpt/example", ExpectedHeadSHA: strings.Repeat("a", 40), ValidationMode: ValidationRepositoryLint, PackageScope: "./internal/workflow"}, "unexpected_scope"},
		{OperationRequest{Operation: OperationValidate, TargetBranch: "web-gpt/example", ExpectedHeadSHA: strings.Repeat("a", 40), ValidationMode: ValidationGoPackageTest, PackageScope: "../outside"}, "invalid_package_scope"},
		{OperationRequest{Operation: OperationAutofix, TargetBranch: "web-gpt/example", ExpectedHeadSHA: strings.Repeat("a", 40), ValidationMode: ValidationRepositoryLint}, "unexpected_validation_fields"},
	}
	for _, tc := range invalid {
		assertValidationCode(t, ValidateOperation(tc.request), tc.code)
	}
}

func TestParseMailboxRejectsInvalidSchemaAndRequestID(t *testing.T) {
	_, err := ParseMailbox(strings.NewReader(`{"version":2,"request_id":"req-1","operation":"autofix","target_branch":"web-gpt/example","expected_head_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
	assertValidationCode(t, err, "invalid_schema_version")

	_, err = ParseMailbox(strings.NewReader(`{"version":1,"request_id":"bad request","operation":"autofix","target_branch":"web-gpt/example","expected_head_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
	assertValidationCode(t, err, "invalid_request_id")
}

func assertValidationCode(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected validation error %q", want)
	}
	if got := ValidationCode(err); got != want {
		t.Fatalf("validation code mismatch: got %q want %q err=%v", got, want, err)
	}
}
