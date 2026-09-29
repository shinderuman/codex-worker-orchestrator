package failurepathadvisory

import "fmt"

type UsageComparisonStatus struct {
	Owner         string `json:"owner"`
	MeasuredTasks int    `json:"measured_tasks"`
	PendingTasks  int    `json:"pending_tasks"`
}

type MeasuredCount struct {
	MeasuredTasks int `json:"measured_tasks"`
	Total         int `json:"total"`
}

type Summary struct {
	Schema                       string                `json:"schema"`
	CohortSize                   int                   `json:"cohort_size"`
	CohortCap                    int                   `json:"cohort_cap"`
	CohortOpen                   bool                  `json:"cohort_open"`
	OutcomeCounts                map[string]int        `json:"outcome_counts"`
	TriggerClassCounts           map[string]int        `json:"trigger_class_counts"`
	AmbiguousRecords             int                   `json:"ambiguous_records"`
	CappedRecords                int                   `json:"capped_records"`
	ClassificationMissingRecords int                   `json:"classification_missing_records"`
	AddedGLM                     AddedGLMUsage         `json:"added_glm"`
	FindingsTotal                int                   `json:"findings_total"`
	IndeterminateFindings        int                   `json:"indeterminate_findings"`
	LabeledFindings              int                   `json:"labeled_findings"`
	UnlabeledFindings            int                   `json:"unlabeled_findings"`
	DispositionCounts            map[string]int        `json:"disposition_counts"`
	TruePositiveAdversarialOnly  int                   `json:"true_positive_adversarial_only"`
	FalseNegatives               MeasuredCount         `json:"false_negatives"`
	EscapedFindings              MeasuredCount         `json:"escaped_findings"`
	HumanInterventions           MeasuredCount         `json:"human_interventions"`
	AdvisoryShownRecords         int                   `json:"advisory_shown_records"`
	AdvisoryOmittedCounts        map[string]int        `json:"advisory_omitted_counts"`
	EarlyStopEligible            bool                  `json:"early_stop_eligible"`
	EarlyStopBasis               string                `json:"early_stop_basis"`
	UsageComparison              UsageComparisonStatus `json:"usage_comparison"`
	Records                      []Record              `json:"records"`
}

func BuildSummary(registry Registry) Summary {
	summary := Summary{
		Schema:                AdvisorySchema,
		CohortSize:            registry.CohortSize(),
		CohortCap:             CohortCap,
		CohortOpen:            registry.CohortSize() < CohortCap,
		OutcomeCounts:         map[string]int{},
		TriggerClassCounts:    map[string]int{},
		DispositionCounts:     map[string]int{},
		AdvisoryOmittedCounts: map[string]int{},
		UsageComparison:       UsageComparisonStatus{Owner: "parent"},
		Records:               registry.Records,
	}
	if summary.Records == nil {
		summary.Records = []Record{}
	}
	for _, record := range registry.Records {
		summary.OutcomeCounts[record.Outcome]++
		for _, class := range record.Classes {
			summary.TriggerClassCounts[class]++
		}
		switch record.Outcome {
		case OutcomeAmbiguous:
			summary.AmbiguousRecords++
		case OutcomeCapped:
			summary.CappedRecords++
		case OutcomeClassificationMissing:
			summary.ClassificationMissingRecords++
		}
		if !cohortOutcomes[record.Outcome] {
			continue
		}
		accumulateAddedGLM(&summary, record)
		accumulateFindings(&summary, record)
		accumulateAdvisory(&summary, record)
		accumulateAssessmentCounts(&summary, record)
	}
	if summary.CohortSize >= EarlyStopMinimumCohort &&
		summary.TruePositiveAdversarialOnly == 0 && summary.UnlabeledFindings == 0 {
		summary.EarlyStopEligible = true
		summary.EarlyStopBasis = fmt.Sprintf(
			"cohort=%d以上かつadversarial-only真陽性0かつ未label finding 0",
			summary.CohortSize,
		)
	}
	return summary
}

func accumulateAddedGLM(summary *Summary, record Record) {
	usage := record.AddedGLM
	if usage == nil {
		return
	}
	summary.AddedGLM.Calls++
	summary.AddedGLM.InputTokens += usage.InputTokens
	summary.AddedGLM.CacheCreationInputTokens += usage.CacheCreationInputTokens
	summary.AddedGLM.CacheReadInputTokens += usage.CacheReadInputTokens
	summary.AddedGLM.OutputTokens += usage.OutputTokens
	summary.AddedGLM.TotalCostUSD += usage.TotalCostUSD
	summary.AddedGLM.WallDurationMS += usage.WallDurationMS
	summary.AddedGLM.ClaudeAPIDurationMS += usage.ClaudeAPIDurationMS
}

func accumulateFindings(summary *Summary, record Record) {
	summary.FindingsTotal += len(record.Findings)
	for _, finding := range record.Findings {
		if finding.Status == FindingStatusIndeterminate {
			summary.IndeterminateFindings++
		}
		if finding.Label == nil {
			summary.UnlabeledFindings++
			continue
		}
		summary.LabeledFindings++
		summary.DispositionCounts[finding.Label.Disposition]++
		if finding.Label.Disposition == DispositionTruePositive {
			summary.TruePositiveAdversarialOnly++
		}
	}
	if record.Assessment != nil && record.Assessment.Usage != nil && record.Assessment.Usage.Measured {
		summary.UsageComparison.MeasuredTasks++
		return
	}
	if record.Outcome == OutcomeObserved {
		summary.UsageComparison.PendingTasks++
	}
}

func accumulateAdvisory(summary *Summary, record Record) {
	if record.Advisory == nil {
		return
	}
	if record.Advisory.Status == AdvisoryShown {
		summary.AdvisoryShownRecords++
		return
	}
	summary.AdvisoryOmittedCounts[record.Advisory.Status]++
}

func accumulateAssessmentCounts(summary *Summary, record Record) {
	if record.Assessment == nil {
		return
	}
	for _, count := range []struct {
		value       *int
		observation *MeasuredCount
	}{
		{record.Assessment.FalseNegatives, &summary.FalseNegatives},
		{record.Assessment.EscapedFindings, &summary.EscapedFindings},
		{record.Assessment.HumanInterventions, &summary.HumanInterventions},
	} {
		if count.value == nil {
			continue
		}
		count.observation.MeasuredTasks++
		count.observation.Total += *count.value
	}
}
