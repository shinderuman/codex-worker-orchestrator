package shadoweval

import (
	"encoding/json"
	"fmt"
)

type ShadowFailure struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
}

type AgreementCount struct {
	Matching    int `json:"matching"`
	Denominator int `json:"denominator"`
}

type ComparisonThreshold struct {
	Threshold                         float64  `json:"threshold"`
	Candidates                        int      `json:"candidates"`
	Coverage                          float64  `json:"coverage"`
	KnownCorrectnessFalseNegatives    int      `json:"known_correctness_false_negatives"`
	UnknownReferenceCandidates        int      `json:"unknown_reference_candidates"`
	EstimatedSolVisibleInputReduction float64  `json:"estimated_sol_visible_input_reduction"`
	CandidateCallIDs                  []string `json:"candidate_call_ids"`
}

type ReductionGroup struct {
	OwnerCategory       string   `json:"owner_category"`
	DispositionCategory string   `json:"disposition_category"`
	CallIDs             []string `json:"call_ids"`
}

type ComparisonRun struct {
	Provider                string       `json:"provider"`
	ModelAlias              string       `json:"model_alias"`
	ModelEffort             string       `json:"model_effort"`
	DurationMS              int64        `json:"duration_ms"`
	DurationAPIMS           int64        `json:"duration_api_ms,omitempty"`
	TotalCostUSD            float64      `json:"total_cost_usd,omitempty"`
	InputBytes              int          `json:"input_bytes"`
	Usage                   UsageMetrics `json:"usage"`
	SettingEnvKeys          []string     `json:"setting_env_keys,omitempty"`
	SettingsSourceDisabled  bool         `json:"settings_source_disabled"`
	NoSessionPersistence    bool         `json:"no_session_persistence"`
	ToolsDisabled           bool         `json:"tools_disabled"`
	UserSettingsLoadedByCLI bool         `json:"user_settings_loaded_by_cli"`
	ControlAuthority        bool         `json:"control_authority"`
	FilterAuthority         bool         `json:"filter_authority"`
	AuditMutated            bool         `json:"audit_mutated"`
}

type DecisionWithReference struct {
	CallID    string          `json:"call_id"`
	Decision  *Decision       `json:"decision,omitempty"`
	Reference *ReferenceLabel `json:"reference"`
}

type Comparison struct {
	Schema                    string                  `json:"schema"`
	TaskID                    string                  `json:"bundle_task_id"`
	ItemsSHA256               string                  `json:"items_sha256"`
	InputItems                int                     `json:"input_items"`
	TypedSchemaValid          bool                    `json:"typed_schema_valid"`
	ValidationErrors          []string                `json:"validation_errors"`
	ShadowFailure             *ShadowFailure          `json:"shadow_failure,omitempty"`
	ReferenceBasis            string                  `json:"reference_basis,omitempty"`
	ReferenceKnown            int                     `json:"reference_known"`
	ReferenceUnknown          int                     `json:"reference_unknown"`
	DispositionAgreementKnown AgreementCount          `json:"disposition_agreement_known"`
	CorrectnessRiskBrierKnown *float64                `json:"correctness_risk_brier_known"`
	CalibrationNote           string                  `json:"calibration_note"`
	Thresholds                []ComparisonThreshold   `json:"thresholds"`
	ReductionGroups           []ReductionGroup        `json:"reduction_groups"`
	Run                       ComparisonRun           `json:"run"`
	DecisionsWithReference    []DecisionWithReference `json:"decisions_with_reference"`
}

const ComparisonSchema = "system-one-shadow-comparison/v1"

const comparisonProvider = "claude-cli-isolated-one-shot"

const reductionGroupNoiseFloor = 0.5

const reductionCandidateRiskCap = 0.1

var comparisonThresholds = []float64{0.5, 0.7, 0.8, 0.9}

func NewRun(metrics CallMetrics, settingEnvKeys []string) ComparisonRun {
	return ComparisonRun{
		Provider:               comparisonProvider,
		ModelAlias:             ModelAlias,
		ModelEffort:            ModelEffort,
		DurationMS:             metrics.DurationMS,
		DurationAPIMS:          metrics.DurationAPIMS,
		TotalCostUSD:           metrics.TotalCostUSD,
		InputBytes:             metrics.InputBytes,
		Usage:                  metrics.Usage,
		SettingEnvKeys:         settingEnvKeys,
		SettingsSourceDisabled: true,
		NoSessionPersistence:   true,
		ToolsDisabled:          true,
	}
}

func BuildComparison(
	input ShadowInput,
	decisions []Decision,
	validationErrors []string,
	failure *ShadowFailure,
	reference Reference,
	run ComparisonRun,
) Comparison {
	if validationErrors == nil {
		validationErrors = []string{}
	}
	byCall := make(map[string]Decision, len(decisions))
	for _, decision := range decisions {
		byCall[decision.CallID] = decision
	}
	labels := make(map[string]ReferenceLabel, len(reference.Labels))
	for _, label := range reference.Labels {
		labels[label.CallID] = label
	}

	comparison := Comparison{
		Schema:           ComparisonSchema,
		TaskID:           input.TaskID,
		ItemsSHA256:      input.ItemsSHA256,
		InputItems:       len(input.Items),
		TypedSchemaValid: len(validationErrors) == 0 && failure == nil,
		ValidationErrors: validationErrors,
		ShadowFailure:    failure,
		ReferenceBasis:   reference.Basis,
		ReferenceKnown:   len(labels),
		ReferenceUnknown: len(input.Items) - len(labels),
		Run:              run,
	}
	if comparison.ReferenceUnknown < 0 {
		comparison.ReferenceUnknown = 0
	}
	comparison.DispositionAgreementKnown = dispositionAgreement(input, byCall, labels)
	comparison.CorrectnessRiskBrierKnown = correctnessBrier(input, byCall, labels)
	comparison.CalibrationNote = fmt.Sprintf("correctness brier over %d known label(s)", len(labels))
	if failure == nil {
		comparison.Thresholds = thresholdRows(input, byCall, labels)
		comparison.ReductionGroups = reductionGroups(input, byCall)
	} else {
		comparison.Thresholds = zeroedThresholdRows()
		comparison.ReductionGroups = []ReductionGroup{}
	}
	comparison.DecisionsWithReference = decisionsWithReference(input, byCall, labels)
	return comparison
}

func dispositionAgreement(input ShadowInput, byCall map[string]Decision, labels map[string]ReferenceLabel) AgreementCount {
	count := AgreementCount{}
	for _, item := range input.Items {
		label, known := labels[item.CallID]
		decision, decided := byCall[item.CallID]
		if !known || !decided {
			continue
		}
		count.Denominator++
		if decision.DispositionCategory == label.Disposition {
			count.Matching++
		}
	}
	return count
}

func correctnessBrier(input ShadowInput, byCall map[string]Decision, labels map[string]ReferenceLabel) *float64 {
	sum := 0.0
	known := 0
	for _, item := range input.Items {
		label, hasLabel := labels[item.CallID]
		decision, decided := byCall[item.CallID]
		if !hasLabel || !decided {
			continue
		}
		target := 0.0
		if label.CorrectnessFinding {
			target = 1.0
		}
		sum += (decision.CorrectnessRiskProbability - target) * (decision.CorrectnessRiskProbability - target)
		known++
	}
	if known == 0 {
		return nil
	}
	brier := sum / float64(known)
	return &brier
}

func thresholdRows(input ShadowInput, byCall map[string]Decision, labels map[string]ReferenceLabel) []ComparisonThreshold {
	totalBytes := inputItemBytes(input)
	rows := make([]ComparisonThreshold, 0, len(comparisonThresholds))
	for _, threshold := range comparisonThresholds {
		rows = append(rows, thresholdRow(input, byCall, labels, threshold, totalBytes))
	}
	return rows
}

func zeroedThresholdRows() []ComparisonThreshold {
	rows := make([]ComparisonThreshold, 0, len(comparisonThresholds))
	for _, threshold := range comparisonThresholds {
		rows = append(rows, ComparisonThreshold{Threshold: threshold, CandidateCallIDs: []string{}})
	}
	return rows
}

func thresholdRow(input ShadowInput, byCall map[string]Decision, labels map[string]ReferenceLabel, threshold float64, totalBytes int) ComparisonThreshold {
	row := ComparisonThreshold{Threshold: threshold, CandidateCallIDs: []string{}}
	candidateBytes := 0
	for _, item := range input.Items {
		decision, decided := byCall[item.CallID]
		if !decided || !noiseReductionCandidate(decision, threshold) {
			continue
		}
		row.Candidates++
		row.CandidateCallIDs = append(row.CandidateCallIDs, item.CallID)
		candidateBytes += itemByteLen(item)
		tallyCandidateReference(&row, labels, item.CallID)
	}
	if len(input.Items) > 0 {
		row.Coverage = float64(row.Candidates) / float64(len(input.Items))
	}
	if totalBytes > 0 {
		row.EstimatedSolVisibleInputReduction = float64(candidateBytes) / float64(totalBytes)
	}
	return row
}

func noiseReductionCandidate(decision Decision, threshold float64) bool {
	return decision.DispositionCategory == "noise" &&
		decision.NoiseProbability >= threshold &&
		decision.NoiseConfidence >= threshold &&
		decision.CorrectnessRiskProbability <= reductionCandidateRiskCap &&
		decision.SolEscalationProbability <= reductionCandidateRiskCap
}

func tallyCandidateReference(row *ComparisonThreshold, labels map[string]ReferenceLabel, callID string) {
	if label, known := labels[callID]; known {
		if label.CorrectnessFinding {
			row.KnownCorrectnessFalseNegatives++
		}
		return
	}
	row.UnknownReferenceCandidates++
}

func reductionGroups(input ShadowInput, byCall map[string]Decision) []ReductionGroup {
	type groupKey struct {
		owner       string
		disposition string
	}
	order := make([]groupKey, 0)
	groups := make(map[groupKey][]string)
	for _, item := range input.Items {
		decision, decided := byCall[item.CallID]
		if !decided || !noiseReductionCandidate(decision, reductionGroupNoiseFloor) {
			continue
		}
		key := groupKey{owner: decision.OwnerCategory, disposition: decision.DispositionCategory}
		if _, exists := groups[key]; !exists {
			order = append(order, key)
		}
		groups[key] = append(groups[key], item.CallID)
	}
	result := make([]ReductionGroup, 0, len(order))
	for _, key := range order {
		result = append(result, ReductionGroup{
			OwnerCategory:       key.owner,
			DispositionCategory: key.disposition,
			CallIDs:             groups[key],
		})
	}
	return result
}

func decisionsWithReference(input ShadowInput, byCall map[string]Decision, labels map[string]ReferenceLabel) []DecisionWithReference {
	rows := make([]DecisionWithReference, 0, len(input.Items))
	for _, item := range input.Items {
		row := DecisionWithReference{CallID: item.CallID}
		if decision, decided := byCall[item.CallID]; decided {
			decision := decision
			row.Decision = &decision
		}
		if label, known := labels[item.CallID]; known {
			row.Reference = &label
		}
		rows = append(rows, row)
	}
	return rows
}

func inputItemBytes(input ShadowInput) int {
	total := 0
	for _, item := range input.Items {
		total += itemByteLen(item)
	}
	return total
}

func itemByteLen(item InputItem) int {
	data, err := json.Marshal(item)
	if err != nil {
		return 0
	}
	return len(data)
}
