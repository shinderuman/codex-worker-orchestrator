package packet

import "fmt"

type machineField string

type machineFieldKind uint8

const (
	machineFieldString machineFieldKind = iota
	machineFieldStrings
	machineFieldStatus
	machineFieldRisk
	machineFieldParentValidationForm
)

const (
	fieldStatus                     machineField = "status"
	fieldRisk                       machineField = "risk"
	fieldSummary                    machineField = "summary"
	fieldRequirementCoverage        machineField = "requirement_coverage"
	fieldTests                      machineField = "tests"
	fieldUnverified                 machineField = "unverified"
	fieldParentValidation           machineField = "parent_validation"
	fieldParentValidationWorkingDir machineField = "parent_validation_working_dir"
	fieldParentValidationEvidence   machineField = "parent_validation_evidence"
	fieldDecision                   machineField = "decision"
	fieldEvidence                   machineField = "evidence"
	fieldOptions                    machineField = "options"
	fieldRecommendation             machineField = "recommendation"
	fieldTestObligations            machineField = "test_obligations"
	fieldInvariants                 machineField = "invariants"
	fieldTestEvidence               machineField = "test_evidence"
	fieldIssues                     machineField = "issues"
	fieldResidualRisk               machineField = "residual_risk"
	fieldSolQuestion                machineField = "sol_question"
	fieldTargets                    machineField = "targets"
	fieldArtifacts                  machineField = "artifacts"
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

const (
	ParentValidationGoTest     = "go-test"
	ParentValidationGoTestRace = "go-test-race"
)

type machineFieldContract struct {
	kind  machineFieldKind
	value func(Result) string
}

var modelFieldContracts = map[machineField]machineFieldContract{
	fieldStatus:                     {kind: machineFieldStatus},
	fieldRisk:                       {kind: machineFieldRisk},
	fieldSummary:                    {kind: machineFieldString, value: func(r Result) string { return r.Summary }},
	fieldRequirementCoverage:        {kind: machineFieldString, value: func(r Result) string { return r.RequirementCoverage }},
	fieldTests:                      {kind: machineFieldString, value: func(r Result) string { return r.Tests }},
	fieldUnverified:                 {kind: machineFieldString, value: func(r Result) string { return r.Unverified }},
	fieldParentValidation:           {kind: machineFieldParentValidationForm},
	fieldParentValidationWorkingDir: {kind: machineFieldString},
	fieldDecision:                   {kind: machineFieldString, value: func(r Result) string { return r.Decision }},
	fieldEvidence:                   {kind: machineFieldString, value: func(r Result) string { return r.Evidence }},
	fieldOptions:                    {kind: machineFieldString, value: func(r Result) string { return r.Options }},
	fieldRecommendation:             {kind: machineFieldString, value: func(r Result) string { return r.Recommendation }},
	fieldTestObligations:            {kind: machineFieldString, value: func(r Result) string { return r.TestObligations }},
	fieldInvariants:                 {kind: machineFieldString, value: func(r Result) string { return r.Invariants }},
	fieldTestEvidence:               {kind: machineFieldString, value: func(r Result) string { return r.TestEvidence }},
	fieldIssues:                     {kind: machineFieldString, value: func(r Result) string { return r.Issues }},
	fieldResidualRisk:               {kind: machineFieldString, value: func(r Result) string { return r.ResidualRisk }},
	fieldSolQuestion:                {kind: machineFieldString, value: func(r Result) string { return r.SolQuestion }},
	fieldTargets:                    {kind: machineFieldStrings},
	fieldArtifacts:                  {kind: machineFieldStrings},
}

type statusContract struct {
	risks        []Risk
	resultFields []machineField
	invalidRisk  func(Risk) string
}

var reviewerResultFields = []machineField{
	fieldSummary,
	fieldRequirementCoverage,
	fieldInvariants,
	fieldTestEvidence,
	fieldIssues,
	fieldResidualRisk,
}

var statusContracts = map[Status]statusContract{
	StatusImplemented: {
		risks: []Risk{RiskLow, RiskHigh},
		resultFields: []machineField{
			fieldSummary,
			fieldRequirementCoverage,
			fieldTests,
			fieldUnverified,
		},
		invalidRisk: invalidLowHighRisk,
	},
	StatusNeedsSolDecision: {
		risks: []Risk{RiskHigh},
		resultFields: []machineField{
			fieldDecision,
			fieldEvidence,
			fieldOptions,
			fieldRecommendation,
			fieldTestObligations,
		},
		invalidRisk: func(Risk) string { return "NEEDS_SOL_DECISIONのriskはHIGHにしてください" },
	},
	StatusPass: {
		risks:        []Risk{RiskLow},
		resultFields: reviewerResultFields,
		invalidRisk:  func(Risk) string { return "PASSのriskはLOWにしてください。高リスクならNEEDS_SOL_REVIEWを返してください" },
	},
	StatusFixRequired: {
		risks:        []Risk{RiskLow, RiskHigh},
		resultFields: reviewerResultFields,
		invalidRisk:  invalidLowHighRisk,
	},
	StatusNeedsSolReview: {
		risks: RiskSlice(RiskHigh),
		resultFields: append(
			append([]machineField(nil), reviewerResultFields...),
			fieldSolQuestion,
		),
		invalidRisk: func(Risk) string { return "NEEDS_SOL_REVIEWのriskはHIGHにしてください" },
	},
}

func RiskSlice(values ...Risk) []Risk {
	return values
}

type machineContract struct {
	name                 string
	modelFields          []machineField
	statuses             []Status
	strictRequiredStatus Status
}

var workerModelFields = []machineField{
	fieldStatus,
	fieldRisk,
	fieldSummary,
	fieldRequirementCoverage,
	fieldTests,
	fieldUnverified,
	fieldParentValidation,
	fieldParentValidationWorkingDir,
	fieldDecision,
	fieldEvidence,
	fieldOptions,
	fieldRecommendation,
	fieldTestObligations,
	fieldTargets,
	fieldArtifacts,
}

var reviewerModelFields = []machineField{
	fieldStatus,
	fieldRisk,
	fieldSummary,
	fieldRequirementCoverage,
	fieldInvariants,
	fieldTestEvidence,
	fieldIssues,
	fieldResidualRisk,
	fieldSolQuestion,
	fieldDecision,
	fieldEvidence,
	fieldOptions,
	fieldRecommendation,
	fieldTestObligations,
	fieldTargets,
	fieldArtifacts,
}

var workerMachineContract = machineContract{
	name:        "worker",
	modelFields: workerModelFields,
	statuses:    []Status{StatusImplemented, StatusNeedsSolDecision},
}

var reviewerMachineContract = machineContract{
	name:        "reviewer",
	modelFields: reviewerModelFields,
	statuses:    []Status{StatusPass, StatusFixRequired, StatusNeedsSolReview, StatusNeedsSolDecision},
}

var highFloorReviewerMachineContract = machineContract{
	name:        "reviewer",
	modelFields: reviewerModelFields,
	statuses:    []Status{StatusFixRequired, StatusNeedsSolReview, StatusNeedsSolDecision},
}

var riskFloorReviewerMachineContract = machineContract{
	name:                 "reviewer",
	modelFields:          reviewerModelFields,
	statuses:             []Status{StatusNeedsSolReview},
	strictRequiredStatus: StatusNeedsSolReview,
}

func invalidLowHighRisk(risk Risk) string {
	return fmt.Sprintf("riskはLOWまたはHIGHで指定してください: %q", string(risk))
}

func resultFieldsForStatus(status Status) []machineField {
	if contract, ok := statusContracts[status]; ok {
		return contract.resultFields
	}
	return reviewerResultFields
}

func machineFieldValue(result Result, field machineField) string {
	contract, ok := modelFieldContracts[field]
	if !ok || contract.value == nil {
		panic(fmt.Sprintf("machine field %qにresult value ownerがありません", field))
	}
	return contract.value(result)
}

func machineFieldSpec(field machineField) machineFieldContract {
	contract, ok := modelFieldContracts[field]
	if !ok {
		panic(fmt.Sprintf("machine field %qにcontractがありません", field))
	}
	return contract
}

func machineContractRisks(contract machineContract) []Risk {
	seen := make(map[Risk]struct{})
	var result []Risk
	for _, status := range contract.statuses {
		statusContract, ok := statusContracts[status]
		if !ok {
			panic(fmt.Sprintf("status %qにmachine contractがありません", status))
		}
		for _, risk := range statusContract.risks {
			if _, ok := seen[risk]; ok {
				continue
			}
			seen[risk] = struct{}{}
			result = append(result, risk)
		}
	}
	return result
}

func schemaRequiredFields(contract machineContract) []machineField {
	if contract.strictRequiredStatus == "" {
		return []machineField{fieldStatus, fieldRisk, fieldTargets, fieldArtifacts}
	}
	statusContract, ok := statusContracts[contract.strictRequiredStatus]
	if !ok {
		panic(fmt.Sprintf("strict required status %qにmachine contractがありません", contract.strictRequiredStatus))
	}
	result := []machineField{fieldStatus, fieldRisk}
	result = append(result, statusContract.resultFields...)
	result = append(result, fieldTargets, fieldArtifacts)
	return result
}

func machineContractAllowsStatus(contract machineContract, status Status) bool {
	for _, allowed := range contract.statuses {
		if status == allowed {
			return true
		}
	}
	return false
}

func statusAllowsRisk(contract statusContract, risk Risk) bool {
	for _, allowed := range contract.risks {
		if risk == allowed {
			return true
		}
	}
	return false
}

func validateMachineStatusRisk(result Result, contract machineContract) error {
	if !machineContractAllowsStatus(contract, result.Status) {
		return &mismatchError{reason: fmt.Sprintf("%s結果のstatusとして許容されません: %q", contract.name, string(result.Status))}
	}
	statusContract := statusContracts[result.Status]
	if statusAllowsRisk(statusContract, result.Risk) {
		return nil
	}
	return &constraintError{reason: statusContract.invalidRisk(result.Risk)}
}
