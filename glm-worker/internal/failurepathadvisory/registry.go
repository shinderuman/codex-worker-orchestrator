package failurepathadvisory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Finding struct {
	Target       string        `json:"target"`
	Class        string        `json:"class"`
	Issue        string        `json:"issue,omitempty"`
	Evidence     string        `json:"evidence,omitempty"`
	Status       string        `json:"status"`
	VisibleIndex *int          `json:"visible_index,omitempty"`
	Label        *FindingLabel `json:"label,omitempty"`
}

type FindingLabel struct {
	Disposition string `json:"disposition"`
}

type AdvisoryOutcome struct {
	Status        string `json:"status"`
	FindingsShown int    `json:"findings_shown,omitempty"`
	Indeterminate int    `json:"indeterminate,omitempty"`
	Truncated     bool   `json:"truncated,omitempty"`
}

type AddedGLMUsage struct {
	Calls                    int     `json:"calls"`
	InputTokens              int64   `json:"input_tokens"`
	CacheCreationInputTokens int64   `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64   `json:"cache_read_input_tokens"`
	OutputTokens             int64   `json:"output_tokens"`
	TotalCostUSD             float64 `json:"total_cost_usd"`
	WallDurationMS           int64   `json:"wall_duration_ms"`
	ClaudeAPIDurationMS      int64   `json:"claude_api_duration_ms"`
}

type UsageComparison struct {
	CodexTokens int64  `json:"codex_tokens,omitempty"`
	SolTokens   int64  `json:"sol_tokens,omitempty"`
	Measured    bool   `json:"measured"`
	Note        string `json:"note,omitempty"`
}

type SolAssessment struct {
	AvoidedReviewFixWaves int              `json:"avoided_review_fix_waves,omitempty"`
	FalseNegatives        *int             `json:"false_negatives,omitempty"`
	EscapedFindings       *int             `json:"escaped_findings,omitempty"`
	HumanInterventions    *int             `json:"human_interventions,omitempty"`
	QualityDeltaNote      string           `json:"quality_delta_note,omitempty"`
	Usage                 *UsageComparison `json:"usage,omitempty"`
}

type Record struct {
	TaskID             string           `json:"task_id"`
	ReviewNumber       int              `json:"review_number"`
	Phase              string           `json:"phase,omitempty"`
	Outcome            string           `json:"outcome"`
	Classes            []string         `json:"classes,omitempty"`
	AmbiguousClasses   []string         `json:"ambiguous_classes,omitempty"`
	TriggerPaths       []string         `json:"trigger_paths,omitempty"`
	CallID             string           `json:"call_id,omitempty"`
	ReviewPacketStatus string           `json:"review_packet_status,omitempty"`
	ReviewIssues       string           `json:"review_issues,omitempty"`
	ReviewTargets      []string         `json:"review_targets,omitempty"`
	Findings           []Finding        `json:"findings,omitempty"`
	Advisory           *AdvisoryOutcome `json:"advisory,omitempty"`
	AddedGLM           *AddedGLMUsage   `json:"added_glm,omitempty"`
	Assessment         *SolAssessment   `json:"sol_assessment,omitempty"`
	Detail             string           `json:"detail,omitempty"`
	RecordedAt         string           `json:"recorded_at"`
}

type Registry struct {
	Schema  string   `json:"schema"`
	Records []Record `json:"records"`
}

type LabelInput struct {
	Schema                string                    `json:"schema"`
	TaskID                string                    `json:"task_id"`
	ReviewNumber          int                       `json:"review_number"`
	FindingDispositions   []FindingDispositionInput `json:"finding_dispositions"`
	AvoidedReviewFixWaves int                       `json:"avoided_review_fix_waves,omitempty"`
	FalseNegatives        *int                      `json:"false_negatives,omitempty"`
	EscapedFindings       *int                      `json:"escaped_findings,omitempty"`
	HumanInterventions    *int                      `json:"human_interventions,omitempty"`
	QualityDeltaNote      string                    `json:"quality_delta_note,omitempty"`
	Usage                 *UsageComparison          `json:"usage,omitempty"`
}

type FindingDispositionInput struct {
	Index       int    `json:"index"`
	Disposition string `json:"disposition"`
}

const AdvisorySchema = "failure-path-advisory/v1"

const LabelsSchema = "failure-path-advisory-labels/v1"

const RegistryFile = "failure-path-advisory.json"

const CohortCap = 20

const EarlyStopMinimumCohort = 10

const (
	OutcomeObserved              = "observed"
	OutcomeMissingProvider       = "missing-provider"
	OutcomeMissingSchemaInvalid  = "missing-schema-invalid"
	OutcomeMissingDeadline       = "missing-deadline"
	OutcomeMissingPartial        = "missing-partial"
	OutcomeMissingInterrupted    = "missing-interrupted"
	OutcomeMissingRunUnavailable = "missing-run-unavailable"
	OutcomeMissingDiff           = "missing-diff"
	OutcomeCapped                = "capped"
	OutcomeAmbiguous             = "ambiguous"
	OutcomeClassificationMissing = "classification-missing"
)

const (
	FindingStatusVerified      = "finding"
	FindingStatusIndeterminate = "indeterminate"
)

const (
	AdvisoryShown           = "shown"
	AdvisoryOmittedNoFind   = "omitted-no-findings"
	AdvisoryOmittedFailOpen = "omitted-fail-open"
	AdvisoryOmittedBound    = "omitted-packet-bound"
	AdvisoryOmittedRegistry = "omitted-registry-failure"
)

const (
	DispositionTruePositive      = "true-positive"
	DispositionReviewerDuplicate = "reviewer-duplicate"
	DispositionNewSemantic       = "new-semantic-judgment"
	DispositionFalsePositive     = "false-positive"
)

const (
	detailTextBoundBytes  = 2048
	findingTextBoundBytes = 1024
	findingsMaxItems      = 32
)

var cohortOutcomes = map[string]bool{
	OutcomeObserved:              true,
	OutcomeMissingProvider:       true,
	OutcomeMissingSchemaInvalid:  true,
	OutcomeMissingDeadline:       true,
	OutcomeMissingPartial:        true,
	OutcomeMissingInterrupted:    true,
	OutcomeMissingRunUnavailable: true,
}

var dispositionValues = map[string]bool{
	DispositionTruePositive:      true,
	DispositionReviewerDuplicate: true,
	DispositionNewSemantic:       true,
	DispositionFalsePositive:     true,
}

func LoadRegistry(path string) (Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Registry{}, nil
		}
		return Registry{}, fmt.Errorf("failure-path advisory registryを読めません: %w", err)
	}
	var registry Registry
	if err := json.Unmarshal(data, &registry); err != nil {
		return Registry{}, fmt.Errorf("failure-path advisory registryを解析できません: %w", err)
	}
	if registry.Schema != AdvisorySchema {
		return Registry{}, fmt.Errorf("failure-path advisory registryのschemaが不正です: %q", registry.Schema)
	}
	return registry, nil
}

func SaveRegistry(path string, registry Registry) error {
	registry.Schema = AdvisorySchema
	if registry.Records == nil {
		registry.Records = []Record{}
	}
	data, err := json.Marshal(registry)
	if err != nil {
		return fmt.Errorf("failure-path advisory registryをJSON化できません: %w", err)
	}
	temporary := path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("failure-path advisory registry dirを作成できません: %w", err)
	}
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("failure-path advisory registryを書けません: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("failure-path advisory registryを保存できません: %w", err)
	}
	return nil
}

func (r Registry) HasTaskRecord(taskID string) bool {
	for _, record := range r.Records {
		if record.TaskID == taskID {
			return true
		}
	}
	return false
}

func (r Registry) HasTaskReviewRecord(taskID string, reviewNumber int) bool {
	for _, record := range r.Records {
		if record.TaskID == taskID && record.ReviewNumber == reviewNumber {
			return true
		}
	}
	return false
}

func (r Registry) CohortSize() int {
	size := 0
	for _, record := range r.Records {
		if cohortOutcomes[record.Outcome] {
			size++
		}
	}
	return size
}

func (r Registry) WithRecord(record Record) Registry {
	updated := r
	updated.Records = append(append([]Record(nil), r.Records...), record)
	return updated
}

func LoadLabels(path string) (LabelInput, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return LabelInput{}, fmt.Errorf("failure-path advisory labelsを読めません: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var input LabelInput
	if err := decoder.Decode(&input); err != nil {
		return LabelInput{}, fmt.Errorf("failure-path advisory labelsを解析できません: %w", err)
	}
	if err := validateLabels(input); err != nil {
		return LabelInput{}, err
	}
	return input, nil
}

func validateLabels(input LabelInput) error {
	if input.Schema != LabelsSchema {
		return fmt.Errorf("failure-path advisory labelsのschemaが不正です: %q", input.Schema)
	}
	if input.TaskID == "" {
		return fmt.Errorf("failure-path advisory labelsのtask_idが空です")
	}
	if input.ReviewNumber <= 0 {
		return fmt.Errorf("failure-path advisory labelsのreview_numberが不正です: %d", input.ReviewNumber)
	}
	seen := make(map[int]struct{}, len(input.FindingDispositions))
	for index, disposition := range input.FindingDispositions {
		if disposition.Index < 0 {
			return fmt.Errorf("failure-path advisory labels[%d]のindexが負です", index)
		}
		if _, duplicate := seen[disposition.Index]; duplicate {
			return fmt.Errorf("failure-path advisory labelsのindex %dが重複しています", disposition.Index)
		}
		seen[disposition.Index] = struct{}{}
		if !dispositionValues[disposition.Disposition] {
			return fmt.Errorf("failure-path advisory labels[%d]のdispositionが不正です: %q", index, disposition.Disposition)
		}
	}
	for _, count := range []struct {
		name  string
		value *int
	}{
		{"false_negatives", input.FalseNegatives},
		{"escaped_findings", input.EscapedFindings},
		{"human_interventions", input.HumanInterventions},
	} {
		if count.value != nil && *count.value < 0 {
			return fmt.Errorf("failure-path advisory labelsの%sが負です: %d", count.name, *count.value)
		}
	}
	return nil
}

func ApplyLabels(registry Registry, input LabelInput) (Registry, error) {
	if err := validateLabels(input); err != nil {
		return Registry{}, err
	}
	recordIndex := -1
	for index := len(registry.Records) - 1; index >= 0; index-- {
		if registry.Records[index].TaskID == input.TaskID && registry.Records[index].ReviewNumber == input.ReviewNumber {
			recordIndex = index
			break
		}
	}
	if recordIndex < 0 {
		return Registry{}, fmt.Errorf("failure-path advisory labelsのtask %s review %dがregistryに存在しません", input.TaskID, input.ReviewNumber)
	}
	record := registry.Records[recordIndex]
	if record.Outcome != OutcomeObserved {
		return Registry{}, fmt.Errorf("failure-path advisory labelsはobserved record以外へ適用できません: %s", record.Outcome)
	}
	for _, disposition := range input.FindingDispositions {
		rawIndex, ok := visibleFindingRawIndex(record, disposition.Index)
		if !ok {
			return Registry{}, fmt.Errorf("failure-path advisory labelsのindex %dがSol-visible findings範囲外です", disposition.Index)
		}
		record.Findings[rawIndex].Label = &FindingLabel{Disposition: disposition.Disposition}
	}
	record.Assessment = &SolAssessment{
		AvoidedReviewFixWaves: input.AvoidedReviewFixWaves,
		FalseNegatives:        input.FalseNegatives,
		EscapedFindings:       input.EscapedFindings,
		HumanInterventions:    input.HumanInterventions,
		QualityDeltaNote:      boundText(input.QualityDeltaNote, detailTextBoundBytes),
		Usage:                 input.Usage,
	}
	updated := Registry{Schema: registry.Schema}
	updated.Records = append([]Record(nil), registry.Records...)
	updated.Records[recordIndex] = record
	return updated, nil
}

func visibleFindingRawIndex(record Record, visibleIndex int) (int, bool) {
	for index, finding := range record.Findings {
		if finding.VisibleIndex != nil && *finding.VisibleIndex == visibleIndex {
			return index, true
		}
	}
	if record.Advisory == nil || record.Advisory.Status != AdvisoryShown || visibleIndex >= record.Advisory.FindingsShown {
		return 0, false
	}
	visible := 0
	for index, finding := range record.Findings {
		if finding.Status != FindingStatusVerified {
			continue
		}
		if visible == visibleIndex {
			return index, true
		}
		visible++
		if visible >= record.Advisory.FindingsShown {
			break
		}
	}
	return 0, false
}

func boundText(text string, limit int) string {
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
