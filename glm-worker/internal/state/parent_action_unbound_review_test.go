package state

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func TestUnboundParentReviewDoesNotAdvertiseAccept(t *testing.T) {
	st, _, _ := newBoundParentReviewTestStore(t)
	if err := st.RecordSolResult(packet.Result{
		Status: packet.StatusNeedsSolReview,
		Risk:   packet.RiskHigh,
	}, ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}

	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionReview {
		t.Fatalf("unbound review required action = %s", plan.RequiredAction)
	}
	if plan.Allows(ParentActionAccept) || plan.AdmitsCommand(ParentActionAccept) {
		t.Fatalf("unbound review advertised accept = %#v", plan)
	}
	if !plan.Allows(ParentActionFix) || !plan.Allows(ParentActionPark) {
		t.Fatalf("unbound review lost fix/park = %#v", plan)
	}
}
