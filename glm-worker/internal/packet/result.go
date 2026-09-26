package packet

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

type Status string

type Risk string

type ParentValidationRequest struct {
	Form       string
	WorkingDir string
}

type ParentValidationEvidence struct {
	ValidationRunID string `json:"validation_run_id"`
	Form            string `json:"form"`
	Repository      string `json:"repository"`
	WorkingDir      string `json:"working_dir"`
	Head            string `json:"head"`
	IndexDigest     string `json:"index_digest"`
	WorktreeDigest  string `json:"worktree_digest"`
	Status          string `json:"status"`
	ExitCode        int    `json:"exit_code"`
	DurationMS      int64  `json:"duration_ms"`
	Log             string `json:"log"`
}

type Result struct {
	Status                     Status                    `json:"status"`
	Risk                       Risk                      `json:"risk"`
	Summary                    string                    `json:"summary,omitempty"`
	RequirementCoverage        string                    `json:"requirement_coverage,omitempty"`
	Tests                      string                    `json:"tests,omitempty"`
	Unverified                 string                    `json:"unverified,omitempty"`
	ParentValidation           string                    `json:"parent_validation,omitempty"`
	ParentValidationWorkingDir string                    `json:"parent_validation_working_dir,omitempty"`
	ParentValidationEvidence   *ParentValidationEvidence `json:"parent_validation_evidence,omitempty"`
	Decision                   string                    `json:"decision,omitempty"`
	Evidence                   string                    `json:"evidence,omitempty"`
	Options                    string                    `json:"options,omitempty"`
	Recommendation             string                    `json:"recommendation,omitempty"`
	TestObligations            string                    `json:"test_obligations,omitempty"`
	Invariants                 string                    `json:"invariants,omitempty"`
	TestEvidence               string                    `json:"test_evidence,omitempty"`
	Issues                     string                    `json:"issues,omitempty"`
	ResidualRisk               string                    `json:"residual_risk,omitempty"`
	SolQuestion                string                    `json:"sol_question,omitempty"`
	Targets                    []string                  `json:"targets,omitempty"`
	Artifacts                  []string                  `json:"artifacts,omitempty"`
}

type mismatchError struct {
	reason string
}

const (
	MaxPacketBytes     = 6 * 1024
	MaxFieldBytes      = 1536
	MaxDiagnosticBytes = 6 * 1024
)

const ReportOnlyTargets = "PACKET"

const noneTargetsSentinel = "none"

func (e *mismatchError) Error() string {
	return e.reason
}

func IsMismatchError(err error) bool {
	var target *mismatchError
	return errors.As(err, &target)
}

func ParseStructured(data []byte) (Result, error) {
	if len(bytes.TrimSpace(data)) == 0 || string(bytes.TrimSpace(data)) == "null" {
		return Result{}, &mismatchError{reason: "result eventにstructured_outputがありません"}
	}
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		return Result{}, &mismatchError{reason: fmt.Sprintf("structured_outputをResultへ解析できません: %v", err)}
	}
	if result.Status == "" {
		return Result{}, &mismatchError{reason: "structured_outputのstatusが空です"}
	}
	return result, nil
}

func (r Result) ParentValidationRequest() *ParentValidationRequest {
	if r.ParentValidation == "" && r.ParentValidationWorkingDir == "" {
		return nil
	}
	return &ParentValidationRequest{Form: r.ParentValidation, WorkingDir: r.ParentValidationWorkingDir}
}

func (r *Result) SetParentValidationRequest(request *ParentValidationRequest) {
	if request == nil {
		r.ParentValidation = ""
		r.ParentValidationWorkingDir = ""
		return
	}
	r.ParentValidation = request.Form
	r.ParentValidationWorkingDir = request.WorkingDir
}

func (e *ParentValidationEvidence) ResolvedFor(form string) bool {
	if e == nil || e.Status != "pass" || e.Form != form {
		return false
	}
	if form != ParentValidationGoTest && form != ParentValidationGoTestRace {
		return false
	}
	return e.ValidationRunID != "" &&
		e.Repository != "" &&
		e.WorkingDir != "" &&
		e.Head != "" &&
		e.IndexDigest != "" &&
		e.WorktreeDigest != "" &&
		e.Log != ""
}

func (r Result) MachineJSON() ([]byte, error) {
	object := map[string]any{
		string(fieldStatus): string(r.Status),
		string(fieldRisk):   string(r.Risk),
	}
	for _, field := range resultFieldsForStatus(r.Status) {
		if value := machineFieldValue(r, field); value != "" {
			object[string(field)] = value
		}
	}
	if r.Status == StatusImplemented && r.ParentValidation != "" {
		object[string(fieldParentValidation)] = r.ParentValidation
		object[string(fieldParentValidationWorkingDir)] = r.ParentValidationWorkingDir
	}
	if r.Status == StatusImplemented && r.ParentValidationEvidence != nil {
		object[string(fieldParentValidationEvidence)] = r.ParentValidationEvidence
	}
	if len(r.Targets) > 0 {
		object[string(fieldTargets)] = r.Targets
	}
	if len(r.Artifacts) > 0 {
		object[string(fieldArtifacts)] = r.Artifacts
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(object); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func (r Result) ByteSize() int {
	data, err := r.MachineJSON()
	if err != nil {
		return 0
	}
	return len(data)
}
