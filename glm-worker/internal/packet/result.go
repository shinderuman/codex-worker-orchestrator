package packet

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

type Status string

type Risk string

type Result struct {
	Status              Status   `json:"status"`
	Risk                Risk     `json:"risk"`
	Summary             string   `json:"summary,omitempty"`
	RequirementCoverage string   `json:"requirement_coverage,omitempty"`
	Tests               string   `json:"tests,omitempty"`
	Unverified          string   `json:"unverified,omitempty"`
	Decision            string   `json:"decision,omitempty"`
	Evidence            string   `json:"evidence,omitempty"`
	Options             string   `json:"options,omitempty"`
	Recommendation      string   `json:"recommendation,omitempty"`
	TestObligations     string   `json:"test_obligations,omitempty"`
	Invariants          string   `json:"invariants,omitempty"`
	TestEvidence        string   `json:"test_evidence,omitempty"`
	Issues              string   `json:"issues,omitempty"`
	ResidualRisk        string   `json:"residual_risk,omitempty"`
	SolQuestion         string   `json:"sol_question,omitempty"`
	Targets             []string `json:"targets,omitempty"`
	Artifacts           []string `json:"artifacts,omitempty"`
}

type mismatchError struct {
	reason string
}

type contractField struct {
	machine string
	value   func(Result) string
}

const (
	MaxPacketBytes     = 6 * 1024
	MaxFieldBytes      = 1536
	MaxDiagnosticBytes = 6 * 1024
)

const (
	StatusImplemented      Status = "IMPLEMENTED"
	StatusNeedsSolDecision Status = "NEEDS_SOL_DECISION"
	StatusPass             Status = "PASS"
	StatusFixRequired      Status = "FIX_REQUIRED"
	StatusNeedsSolReview   Status = "NEEDS_SOL_REVIEW"
)

const (
	RiskLow  Risk = "LOW"
	RiskHigh Risk = "HIGH"
)

const ReportOnlyTargets = "PACKET"

const noneTargetsSentinel = "none"

var implementedContractFields = []contractField{
	{"summary", func(r Result) string { return r.Summary }},
	{"requirement_coverage", func(r Result) string { return r.RequirementCoverage }},
	{"tests", func(r Result) string { return r.Tests }},
	{"unverified", func(r Result) string { return r.Unverified }},
}

var needsSolDecisionContractFields = []contractField{
	{"decision", func(r Result) string { return r.Decision }},
	{"evidence", func(r Result) string { return r.Evidence }},
	{"options", func(r Result) string { return r.Options }},
	{"recommendation", func(r Result) string { return r.Recommendation }},
	{"test_obligations", func(r Result) string { return r.TestObligations }},
}

var reviewerContractFields = []contractField{
	{"summary", func(r Result) string { return r.Summary }},
	{"requirement_coverage", func(r Result) string { return r.RequirementCoverage }},
	{"invariants", func(r Result) string { return r.Invariants }},
	{"test_evidence", func(r Result) string { return r.TestEvidence }},
	{"issues", func(r Result) string { return r.Issues }},
	{"residual_risk", func(r Result) string { return r.ResidualRisk }},
}

var needsSolReviewContractFields = append(append([]contractField{}, reviewerContractFields...),
	contractField{"sol_question", func(r Result) string { return r.SolQuestion }})

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

func (r Result) contractFields() []contractField {
	switch r.Status {
	case StatusImplemented:
		return implementedContractFields
	case StatusNeedsSolDecision:
		return needsSolDecisionContractFields
	case StatusNeedsSolReview:
		return needsSolReviewContractFields
	default:
		return reviewerContractFields
	}
}

func (r Result) MachineJSON() ([]byte, error) {
	object := map[string]any{
		"status": string(r.Status),
		"risk":   string(r.Risk),
	}
	for _, field := range r.contractFields() {
		if value := field.value(r); value != "" {
			object[field.machine] = value
		}
	}
	if len(r.Targets) > 0 {
		object["targets"] = r.Targets
	}
	if len(r.Artifacts) > 0 {
		object["artifacts"] = r.Artifacts
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
