package shadoweval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type Decision struct {
	CallID                     string  `json:"call_id"`
	NoiseProbability           float64 `json:"noise_probability"`
	NoiseConfidence            float64 `json:"noise_confidence"`
	LegitimateRetryProbability float64 `json:"legitimate_retry_probability"`
	LegitimateRetryConfidence  float64 `json:"legitimate_retry_confidence"`
	CorrectnessRiskProbability float64 `json:"correctness_risk_probability"`
	CorrectnessRiskConfidence  float64 `json:"correctness_risk_confidence"`
	OwnerCategory              string  `json:"owner_category"`
	OwnerProbability           float64 `json:"owner_probability"`
	OwnerConfidence            float64 `json:"owner_confidence"`
	DispositionCategory        string  `json:"disposition_category"`
	DispositionProbability     float64 `json:"disposition_probability"`
	DispositionConfidence      float64 `json:"disposition_confidence"`
	SolEscalationProbability   float64 `json:"sol_escalation_probability"`
	SolEscalationConfidence    float64 `json:"sol_escalation_confidence"`
}

type probabilityField struct {
	name  string
	value float64
}

const (
	ShadowFailureProvider      = "provider-failure"
	ShadowFailureSchemaInvalid = "schema-invalid"
)

var ownerCategories = map[string]bool{
	"worker":            true,
	"reviewer":          true,
	"parent":            true,
	"test-scenario":     true,
	"production-wiring": true,
	"external-provider": true,
	"unknown":           true,
}

var dispositionCategories = map[string]bool{
	"noise":               true,
	"legitimate-retry":    true,
	"needs-investigation": true,
	"fix":                 true,
	"accept":              true,
	"unknown":             true,
}

func ParseDecisions(raw json.RawMessage, input ShadowInput) ([]Decision, []string) {
	if len(raw) == 0 {
		return nil, []string{"decision出力がありません"}
	}
	items, err := decodeDecisionsEnvelope(raw)
	if err != nil {
		return nil, []string{err.Error()}
	}
	var problems []string
	if items == nil {
		problems = append(problems, "decisions配列がありません")
	}
	decisions := make([]Decision, 0, len(items))
	for index, item := range items {
		decision, itemProblems := decodeDecision(item, index)
		if len(itemProblems) > 0 {
			problems = append(problems, itemProblems...)
			continue
		}
		decisions = append(decisions, decision)
	}
	valid, validationProblems := validateDecisions(decisions, input)
	return valid, append(problems, validationProblems...)
}

func decodeDecision(item json.RawMessage, index int) (Decision, []string) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(item, &fields); err != nil {
		return Decision{}, []string{fmt.Sprintf("index-%d: decision構造が不正です(%v)", index, err)}
	}
	var missing []string
	for _, field := range decisionRequiredFields() {
		if raw, present := fields[field]; !present || string(raw) == "null" {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		return Decision{}, []string{fmt.Sprintf("index-%d: 必須fieldが欠落またはnullです(%s)", index, strings.Join(missing, ", "))}
	}
	decoder := json.NewDecoder(bytes.NewReader(item))
	decoder.DisallowUnknownFields()
	var decision Decision
	if err := decoder.Decode(&decision); err != nil {
		return Decision{}, []string{fmt.Sprintf("index-%d: decision構造が不正です(%v)", index, err)}
	}
	return decision, nil
}

func decodeDecisionsEnvelope(raw json.RawMessage) ([]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var envelope struct {
		Decisions []json.RawMessage `json:"decisions"`
	}
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decision出力の構造が不正です: %w", err)
	}
	return envelope.Decisions, nil
}

func validateDecisions(decisions []Decision, input ShadowInput) ([]Decision, []string) {
	known := make(map[string]bool, len(input.Items))
	for _, item := range input.Items {
		known[item.CallID] = true
	}
	var problems []string
	valid := make([]Decision, 0, len(decisions))
	seen := make(map[string]bool, len(decisions))
	for index, decision := range decisions {
		identity := decision.CallID
		if identity == "" {
			identity = fmt.Sprintf("index-%d", index)
			problems = append(problems, fmt.Sprintf("%s: call_idが空です", identity))
			continue
		}
		if !known[decision.CallID] {
			problems = append(problems, fmt.Sprintf("%s: 入力に存在しないcall_idです", decision.CallID))
			continue
		}
		if seen[decision.CallID] {
			problems = append(problems, fmt.Sprintf("%s: call_idが重複しています", decision.CallID))
			continue
		}
		if fieldProblem := decisionFieldProblem(decision); fieldProblem != "" {
			problems = append(problems, fmt.Sprintf("%s: %s", decision.CallID, fieldProblem))
			continue
		}
		seen[decision.CallID] = true
		valid = append(valid, decision)
	}
	for _, item := range input.Items {
		if !seen[item.CallID] {
			problems = append(problems, fmt.Sprintf("%s: decisionが欠落しています", item.CallID))
		}
	}
	return valid, problems
}

func decisionFieldProblem(decision Decision) string {
	fields := []probabilityField{
		{"noise_probability", decision.NoiseProbability},
		{"noise_confidence", decision.NoiseConfidence},
		{"legitimate_retry_probability", decision.LegitimateRetryProbability},
		{"legitimate_retry_confidence", decision.LegitimateRetryConfidence},
		{"correctness_risk_probability", decision.CorrectnessRiskProbability},
		{"correctness_risk_confidence", decision.CorrectnessRiskConfidence},
		{"owner_probability", decision.OwnerProbability},
		{"owner_confidence", decision.OwnerConfidence},
		{"disposition_probability", decision.DispositionProbability},
		{"disposition_confidence", decision.DispositionConfidence},
		{"sol_escalation_probability", decision.SolEscalationProbability},
		{"sol_escalation_confidence", decision.SolEscalationConfidence},
	}
	for _, field := range fields {
		if field.value < 0 || field.value > 1 {
			return fmt.Sprintf("%sが0..1の範囲外です(%v)", field.name, field.value)
		}
	}
	if !ownerCategories[decision.OwnerCategory] {
		return fmt.Sprintf("owner_categoryが不正です(%q)", decision.OwnerCategory)
	}
	if !dispositionCategories[decision.DispositionCategory] {
		return fmt.Sprintf("disposition_categoryが不正です(%q)", decision.DispositionCategory)
	}
	return ""
}

func DecisionsJSONSchema(itemCount int) string {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"decisions": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"properties":           decisionProperties(),
					"required":             decisionRequiredFields(),
					"additionalProperties": false,
				},
				"minItems": itemCount,
				"maxItems": itemCount,
			},
		},
		"required":             []string{"decisions"},
		"additionalProperties": false,
	}
	data, _ := json.Marshal(schema)
	return string(data)
}

func probabilityProperty() map[string]any {
	return map[string]any{"type": "number", "minimum": 0, "maximum": 1}
}

func decisionProperties() map[string]any {
	return map[string]any{
		"call_id":                      map[string]any{"type": "string"},
		"noise_probability":            probabilityProperty(),
		"noise_confidence":             probabilityProperty(),
		"legitimate_retry_probability": probabilityProperty(),
		"legitimate_retry_confidence":  probabilityProperty(),
		"correctness_risk_probability": probabilityProperty(),
		"correctness_risk_confidence":  probabilityProperty(),
		"owner_category":               map[string]any{"type": "string", "enum": sortedEnum(ownerCategories)},
		"owner_probability":            probabilityProperty(),
		"owner_confidence":             probabilityProperty(),
		"disposition_category":         map[string]any{"type": "string", "enum": sortedEnum(dispositionCategories)},
		"disposition_probability":      probabilityProperty(),
		"disposition_confidence":       probabilityProperty(),
		"sol_escalation_probability":   probabilityProperty(),
		"sol_escalation_confidence":    probabilityProperty(),
	}
}

func decisionRequiredFields() []string {
	return []string{
		"call_id",
		"noise_probability",
		"noise_confidence",
		"legitimate_retry_probability",
		"legitimate_retry_confidence",
		"correctness_risk_probability",
		"correctness_risk_confidence",
		"owner_category",
		"owner_probability",
		"owner_confidence",
		"disposition_category",
		"disposition_probability",
		"disposition_confidence",
		"sol_escalation_probability",
		"sol_escalation_confidence",
	}
}

func sortedEnum(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
