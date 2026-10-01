package webgptdispatch

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

type Request struct {
	Version         int     `json:"version"`
	RequestID       string  `json:"request_id"`
	Operation       string  `json:"operation"`
	TargetBranch    string  `json:"target_branch"`
	ExpectedHeadSHA string  `json:"expected_head_sha"`
	ValidationMode  *string `json:"validation_mode,omitempty"`
	PackageScope    *string `json:"package_scope,omitempty"`
}

type OperationRequest struct {
	Operation       string
	TargetBranch    string
	ExpectedHeadSHA string
	ValidationMode  string
	PackageScope    string
}

type ValidationError struct {
	Code string
}

const (
	MailboxIssueNumber = 1236
	AuthorizedActor    = "shinderuman"
	SchemaVersion      = 1
)

const (
	OperationAutofix  = "autofix"
	OperationValidate = "validate"
)

const (
	ValidationRepositoryLint = "repository-lint"
	ValidationGoPackageTest  = "go-package-test"
	ValidationFullGoTest     = "full-go-test"
	ValidationBuildVet       = "build-vet"
)

var (
	requestIDPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
	shaPattern          = regexp.MustCompile(`^[0-9a-f]{40}$`)
	branchPattern       = regexp.MustCompile(`^web-gpt/[A-Za-z0-9._/-]+$`)
	packageScopePattern = regexp.MustCompile(`^\./[A-Za-z0-9_][A-Za-z0-9_.-]*(/[A-Za-z0-9_][A-Za-z0-9_.-]*)*$`)
)

func (e ValidationError) Error() string {
	return e.Code
}

func ParseMailbox(r io.Reader) (Request, error) {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	var request Request
	if err := decoder.Decode(&request); err != nil {
		return Request{}, ValidationError{Code: "invalid_json"}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Request{}, ValidationError{Code: "trailing_json"}
	}
	if request.Version != SchemaVersion {
		return Request{}, ValidationError{Code: "invalid_schema_version"}
	}
	if !requestIDPattern.MatchString(request.RequestID) {
		return Request{}, ValidationError{Code: "invalid_request_id"}
	}
	operation := OperationRequest{
		Operation:       request.Operation,
		TargetBranch:    request.TargetBranch,
		ExpectedHeadSHA: request.ExpectedHeadSHA,
	}
	if request.ValidationMode != nil {
		operation.ValidationMode = *request.ValidationMode
	}
	if request.PackageScope != nil {
		operation.PackageScope = *request.PackageScope
	}
	if err := ValidateOperation(operation); err != nil {
		return Request{}, err
	}
	request.ExpectedHeadSHA, _ = NormalizeSHA(operation.ExpectedHeadSHA)
	return request, nil
}

func ValidateOperation(request OperationRequest) error {
	if err := validateTargetBranch(request.TargetBranch); err != nil {
		return err
	}
	if _, err := NormalizeSHA(request.ExpectedHeadSHA); err != nil {
		return err
	}
	switch request.Operation {
	case OperationAutofix:
		if request.ValidationMode != "" || request.PackageScope != "" {
			return ValidationError{Code: "unexpected_validation_fields"}
		}
	case OperationValidate:
		if err := validateValidationFields(request.ValidationMode, request.PackageScope); err != nil {
			return err
		}
	default:
		return ValidationError{Code: "invalid_operation"}
	}
	return nil
}

func NormalizeSHA(value string) (string, error) {
	normalized := strings.ToLower(value)
	if !shaPattern.MatchString(normalized) {
		return "", ValidationError{Code: "invalid_expected_head_sha"}
	}
	return normalized, nil
}

func ValidationCode(err error) string {
	var validationErr ValidationError
	if errors.As(err, &validationErr) {
		return validationErr.Code
	}
	return "unexpected_failure"
}

func validateTargetBranch(branch string) error {
	if !branchPattern.MatchString(branch) || strings.Contains(branch, "..") || strings.Contains(branch, "//") {
		return ValidationError{Code: "invalid_target_branch"}
	}
	parts := strings.Split(strings.TrimPrefix(branch, "web-gpt/"), "/")
	for _, part := range parts {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, ".lock") {
			return ValidationError{Code: "invalid_target_branch"}
		}
	}
	return nil
}

func validateValidationFields(mode, scope string) error {
	switch mode {
	case ValidationRepositoryLint, ValidationFullGoTest, ValidationBuildVet:
		if scope != "" {
			return ValidationError{Code: "unexpected_scope"}
		}
	case ValidationGoPackageTest:
		if !packageScopePattern.MatchString(scope) {
			return ValidationError{Code: "invalid_package_scope"}
		}
	default:
		return ValidationError{Code: "invalid_validation_mode"}
	}
	return nil
}
