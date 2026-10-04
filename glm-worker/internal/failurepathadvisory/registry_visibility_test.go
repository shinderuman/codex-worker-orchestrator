package failurepathadvisory

import "testing"

func TestRegistryHasTaskReviewRecordScopesOneShotToRound(t *testing.T) {
	registry := Registry{Records: []Record{{TaskID: "task-a", ReviewNumber: 2}}}
	if !registry.HasTaskReviewRecord("task-a", 2) {
		t.Fatal("matching task review record was not found")
	}
	if registry.HasTaskReviewRecord("task-a", 3) {
		t.Fatal("earlier task record suppressed a later review round")
	}
}

func TestApplyLabelsUsesSolVisibleFindingIndexes(t *testing.T) {
	zero, one := 0, 1
	registry := Registry{
		Schema: AdvisorySchema,
		Records: []Record{{
			TaskID:       "task-a",
			ReviewNumber: 4,
			Outcome:      OutcomeObserved,
			Findings: []Finding{
				{Status: FindingStatusVerified, VisibleIndex: &zero},
				{Status: FindingStatusIndeterminate},
				{Status: FindingStatusVerified, VisibleIndex: &one},
				{Status: FindingStatusVerified},
			},
			Advisory: &AdvisoryOutcome{Status: AdvisoryShown, FindingsShown: 2, Truncated: true},
		}},
	}
	updated, err := ApplyLabels(registry, LabelInput{
		Schema: LabelsSchema,
		TaskID: "task-a",
		FindingDispositions: []FindingDispositionInput{
			{Index: 0, Disposition: DispositionTruePositive},
			{Index: 1, Disposition: DispositionFalsePositive},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	findings := updated.Records[0].Findings
	if findings[0].Label == nil || findings[0].Label.Disposition != DispositionTruePositive {
		t.Fatalf("visible finding 0 label = %+v", findings[0].Label)
	}
	if findings[2].Label == nil || findings[2].Label.Disposition != DispositionFalsePositive {
		t.Fatalf("visible finding 1 label = %+v", findings[2].Label)
	}
	if findings[1].Label != nil || findings[3].Label != nil {
		t.Fatalf("non-visible findings were labeled: %+v", findings)
	}

	_, err = ApplyLabels(registry, LabelInput{
		Schema:              LabelsSchema,
		TaskID:              "task-a",
		FindingDispositions: []FindingDispositionInput{{Index: 2, Disposition: DispositionTruePositive}},
	})
	if err == nil {
		t.Fatal("packet-truncated finding index was accepted")
	}
}

func TestApplyLabelsUsesLatestTaskReviewRecord(t *testing.T) {
	zero := 0
	registry := Registry{
		Schema: AdvisorySchema,
		Records: []Record{
			{TaskID: "task-a", ReviewNumber: 1, Outcome: OutcomeObserved, Findings: []Finding{{Status: FindingStatusVerified, VisibleIndex: &zero}}, Advisory: &AdvisoryOutcome{Status: AdvisoryShown, FindingsShown: 1}},
			{TaskID: "task-a", ReviewNumber: 2, Outcome: OutcomeObserved, Findings: []Finding{{Status: FindingStatusVerified, VisibleIndex: &zero}}, Advisory: &AdvisoryOutcome{Status: AdvisoryShown, FindingsShown: 1}},
		},
	}
	updated, err := ApplyLabels(registry, LabelInput{
		Schema:              LabelsSchema,
		TaskID:              "task-a",
		FindingDispositions: []FindingDispositionInput{{Index: 0, Disposition: DispositionTruePositive}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Records[0].Findings[0].Label != nil {
		t.Fatal("older review record was labeled")
	}
	if updated.Records[1].Findings[0].Label == nil {
		t.Fatal("latest review record was not labeled")
	}
}
