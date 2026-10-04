package workflow

import (
	"fmt"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/failurepathadvisory"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func TestResolveFailurePathAdvisoryMarksOnlyPacketVisibleVerifiedFindings(t *testing.T) {
	findings := make([]failurepathadvisory.Finding, 0, 11)
	findings = append(findings, failurepathadvisory.Finding{Status: failurepathadvisory.FindingStatusIndeterminate, Target: "indeterminate", Class: "resource-lifecycle"})
	for index := 0; index < 10; index++ {
		findings = append(findings, failurepathadvisory.Finding{
			Status: failurepathadvisory.FindingStatusVerified,
			Target: fmt.Sprintf("pkg/file.go:%d", index+1),
			Class:  "resource-lifecycle",
			Issue:  "verified issue",
		})
	}
	record, advisory := resolveFailurePathAdvisoryAttachment(packet.Result{Status: packet.StatusPass}, failurepathadvisory.Record{
		Outcome:  failurepathadvisory.OutcomeObserved,
		CallID:   "call-1",
		Findings: findings,
	})
	if advisory == nil || len(advisory.Findings) == 0 {
		t.Fatal("bounded advisory was not shown")
	}
	if record.Advisory == nil || record.Advisory.FindingsShown != len(advisory.Findings) {
		t.Fatalf("advisory accounting = %+v visible=%d", record.Advisory, len(advisory.Findings))
	}
	visible := 0
	for _, finding := range record.Findings {
		if finding.VisibleIndex == nil {
			continue
		}
		if finding.Status != failurepathadvisory.FindingStatusVerified {
			t.Fatalf("indeterminate finding gained visible index: %+v", finding)
		}
		if *finding.VisibleIndex != visible {
			t.Fatalf("visible index=%d want=%d", *finding.VisibleIndex, visible)
		}
		visible++
	}
	if visible != len(advisory.Findings) {
		t.Fatalf("visible identities=%d packet findings=%d", visible, len(advisory.Findings))
	}
	if visible >= 10 {
		t.Fatal("fixture did not exercise packet finding truncation")
	}
}
