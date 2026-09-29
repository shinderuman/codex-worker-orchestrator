package packet

import (
	"encoding/json"
	"strings"
	"testing"
)

func advisoryCarrierResult(status Status) Result {
	result := Result{Status: status, Targets: []string{"glm-worker/internal/workflow/review_flow.go:attachFailurePathAdvisory"}}
	switch status {
	case StatusPass:
		result.Risk = RiskLow
		result.Summary = "s"
		result.RequirementCoverage = "c"
		result.Invariants = "i"
		result.TestEvidence = "e"
		result.Issues = "none"
		result.ResidualRisk = "none"
	case StatusNeedsSolReview:
		result.Risk = RiskHigh
		result.Summary = "s"
		result.RequirementCoverage = "c"
		result.Invariants = "i"
		result.TestEvidence = "e"
		result.Issues = "i"
		result.ResidualRisk = "r"
		result.SolQuestion = "q"
	case StatusNeedsSolDecision:
		result.Risk = RiskHigh
		result.Decision = "d"
		result.Evidence = "e"
		result.Options = "o"
		result.Recommendation = "r"
		result.TestObligations = "t"
	case StatusFixRequired:
		result.Risk = RiskHigh
		result.Summary = "s"
		result.RequirementCoverage = "c"
		result.Invariants = "i"
		result.TestEvidence = "e"
		result.Issues = "i"
		result.ResidualRisk = "r"
	case StatusImplemented:
		result.Risk = RiskLow
		result.Summary = "s"
		result.RequirementCoverage = "c"
		result.Tests = "t"
		result.Unverified = "u"
	}
	return result
}

func TestValidateRejectsModelEmittedFailurePathAdvisory(t *testing.T) {
	review := advisoryCarrierResult(StatusNeedsSolReview)
	review.FailurePathAdvisory = &FailurePathAdvisory{CallID: "c", Findings: []FailurePathAdvisoryFinding{{Target: "a:1", Class: "external-model-invocation", Issue: "i"}}}
	if err := ValidateReviewerResult(review); err == nil || !strings.Contains(err.Error(), "machine専有") {
		t.Fatalf("reviewer由来advisoryの拒否 = %v", err)
	}
	worker := advisoryCarrierResult(StatusImplemented)
	worker.FailurePathAdvisory = review.FailurePathAdvisory
	if err := ValidateWorkerResult(worker); err == nil || !strings.Contains(err.Error(), "machine専有") {
		t.Fatalf("worker由来advisoryの拒否 = %v", err)
	}
}

func TestMachineJSONProjectsAdvisoryOnlyForSolVisibleStatuses(t *testing.T) {
	advisory := &FailurePathAdvisory{
		CallID: "advisory-call",
		Findings: []FailurePathAdvisoryFinding{
			{Target: "glm-worker/internal/runner/probe.go:90", Class: "external-model-invocation", Issue: "deadlineなし"},
		},
		Indeterminate: []FailurePathAdvisoryIndeterminate{
			{Target: "glm-worker/internal/state/stats.go:9", Class: "metric-reduction-accounting"},
		},
	}
	for _, status := range []Status{StatusPass, StatusNeedsSolReview, StatusNeedsSolDecision} {
		result := advisoryCarrierResult(status)
		result.FailurePathAdvisory = advisory
		data, err := result.MachineJSON()
		if err != nil {
			t.Fatal(err)
		}
		encoded := string(data)
		if !strings.Contains(encoded, "failure_path_advisory") || !strings.Contains(encoded, "advisory-call") {
			t.Fatalf("%sへadvisoryが投影されていません: %s", status, encoded)
		}
		if size := result.ByteSize(); size > MaxPacketBytes {
			t.Fatalf("%sのpacket size = %d", status, size)
		}
	}
	for _, status := range []Status{StatusFixRequired, StatusImplemented} {
		result := advisoryCarrierResult(status)
		result.FailurePathAdvisory = advisory
		data, err := result.MachineJSON()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "failure_path_advisory") {
			t.Fatalf("%sへadvisoryが漏出しています: %s", status, data)
		}
	}
}

func TestBoundFailurePathAdvisoryCapsAndDropsToNil(t *testing.T) {
	var findings []FailurePathAdvisoryFinding
	for index := 0; index < advisoryFindingsMax+3; index++ {
		findings = append(findings, FailurePathAdvisoryFinding{
			Target: "glm-worker/internal/runner/probe.go:90",
			Class:  "external-model-invocation",
			Issue:  "issue",
		})
	}
	bounded := BoundFailurePathAdvisory(advisoryCarrierResult(StatusNeedsSolReview), &FailurePathAdvisory{CallID: "c", Findings: findings})
	if bounded == nil || len(bounded.Findings) != advisoryFindingsMax || !bounded.Truncated {
		t.Fatalf("findings上限 = %+v", bounded)
	}
	if bounded := BoundFailurePathAdvisory(advisoryCarrierResult(StatusNeedsSolReview), &FailurePathAdvisory{CallID: "c"}); bounded != nil {
		t.Fatalf("findingsなし = %+v", bounded)
	}
	if bounded := BoundFailurePathAdvisory(advisoryCarrierResult(StatusNeedsSolReview), nil); bounded != nil {
		t.Fatalf("nil advisory = %+v", bounded)
	}

	bloated := advisoryCarrierResult(StatusNeedsSolReview)
	bloated.Summary = strings.Repeat("x", MaxFieldBytes)
	bloated.Issues = strings.Repeat("y", MaxFieldBytes)
	bloated.ResidualRisk = strings.Repeat("z", MaxFieldBytes)
	var wide []FailurePathAdvisoryFinding
	for index := 0; index < advisoryFindingsMax; index++ {
		wide = append(wide, FailurePathAdvisoryFinding{
			Target: strings.Repeat("t", advisoryTargetBytes),
			Class:  "external-model-invocation",
			Issue:  strings.Repeat("i", advisoryIssueBytes),
		})
	}
	fitted := BoundFailurePathAdvisory(bloated, &FailurePathAdvisory{CallID: "c", Findings: wide})
	candidate := bloated
	candidate.FailurePathAdvisory = fitted
	if fitted != nil && candidate.ByteSize() > MaxPacketBytes {
		t.Fatalf("bound後packet size超過 = %d", candidate.ByteSize())
	}

	huge := advisoryCarrierResult(StatusNeedsSolReview)
	huge.Summary = strings.Repeat("x", MaxFieldBytes)
	huge.RequirementCoverage = strings.Repeat("y", MaxFieldBytes)
	huge.Issues = strings.Repeat("z", MaxFieldBytes)
	huge.ResidualRisk = strings.Repeat("w", MaxFieldBytes)
	huge.TestEvidence = strings.Repeat("v", MaxFieldBytes)
	if bounded := BoundFailurePathAdvisory(huge, &FailurePathAdvisory{CallID: "c", Findings: wide}); bounded != nil {
		t.Fatalf("収まらない場合はnil = %+v", bounded)
	}
}

func TestFailurePathAdvisoryJSONShape(t *testing.T) {
	advisory := &FailurePathAdvisory{
		CallID:   "call-1",
		Findings: []FailurePathAdvisoryFinding{{Target: "a:1", Class: "c", Issue: "i"}},
	}
	data, err := json.Marshal(advisory)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(data)
	for _, fragment := range []string{`"call_id":"call-1"`, `"target":"a:1"`, `"class":"c"`, `"issue":"i"`} {
		if !strings.Contains(encoded, fragment) {
			t.Fatalf("advisory JSON %s に %s がありません", encoded, fragment)
		}
	}
}
