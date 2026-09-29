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

type FailurePathAdvisoryFinding struct {
	Target string `json:"target"`
	Class  string `json:"class"`
	Issue  string `json:"issue"`
}

type FailurePathAdvisoryIndeterminate struct {
	Target string `json:"target"`
	Class  string `json:"class"`
}

type FailurePathAdvisory struct {
	CallID        string                             `json:"call_id"`
	Findings      []FailurePathAdvisoryFinding       `json:"findings"`
	Indeterminate []FailurePathAdvisoryIndeterminate `json:"indeterminate,omitempty"`
	Truncated     bool                               `json:"truncated,omitempty"`
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
	FailurePathAdvisory        *FailurePathAdvisory      `json:"failure_path_advisory,omitempty"`
}

type mismatchError struct {
	reason string
}

const (
	MaxPacketBytes     = 6 * 1024
	MaxFieldBytes      = 1536
	MaxDiagnosticBytes = 6 * 1024
)

const (
	advisoryFindingsMax      = 8
	advisoryIndeterminateMax = 8
	advisoryIssueBytes       = 200
	advisoryTargetBytes      = 256
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
	if r.FailurePathAdvisory != nil && AdvisoryVisibleStatus(r.Status) {
		object[string(fieldFailurePathAdvisory)] = r.FailurePathAdvisory
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

func AdvisoryVisibleStatus(status Status) bool {
	switch status {
	case StatusPass, StatusNeedsSolReview, StatusNeedsSolDecision:
		return true
	default:
		return false
	}
}

func BoundFailurePathAdvisory(base Result, advisory *FailurePathAdvisory) *FailurePathAdvisory {
	if advisory == nil || len(advisory.Findings) == 0 {
		return nil
	}
	bounded := &FailurePathAdvisory{
		CallID:    advisory.CallID,
		Truncated: advisory.Truncated,
	}
	for _, finding := range advisory.Findings {
		if len(bounded.Findings) >= advisoryFindingsMax {
			bounded.Truncated = true
			break
		}
		bounded.Findings = append(bounded.Findings, FailurePathAdvisoryFinding{
			Target: boundAdvisoryText(finding.Target, advisoryTargetBytes),
			Class:  finding.Class,
			Issue:  boundAdvisoryText(finding.Issue, advisoryIssueBytes),
		})
	}
	for _, item := range advisory.Indeterminate {
		if len(bounded.Indeterminate) >= advisoryIndeterminateMax {
			bounded.Truncated = true
			break
		}
		bounded.Indeterminate = append(bounded.Indeterminate, FailurePathAdvisoryIndeterminate{
			Target: boundAdvisoryText(item.Target, advisoryTargetBytes),
			Class:  item.Class,
		})
	}
	return fitFailurePathAdvisory(base, bounded)
}

func fitFailurePathAdvisory(base Result, advisory *FailurePathAdvisory) *FailurePathAdvisory {
	candidate := base
	candidate.FailurePathAdvisory = advisory
	if candidate.ByteSize() <= MaxPacketBytes {
		return advisory
	}
	for {
		switch {
		case len(advisory.Indeterminate) > 0:
			advisory.Indeterminate = advisory.Indeterminate[:len(advisory.Indeterminate)-1]
		case len(advisory.Findings) > 1:
			advisory.Findings = advisory.Findings[:len(advisory.Findings)-1]
		default:
			return nil
		}
		advisory.Truncated = true
		candidate.FailurePathAdvisory = advisory
		if candidate.ByteSize() <= MaxPacketBytes {
			return advisory
		}
	}
}

func boundAdvisoryText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	tail := []byte(text)
	start := len(tail) - limit
	for start < len(tail) && tail[start]&0xC0 == 0x80 {
		start++
	}
	return string(tail[start:])
}
