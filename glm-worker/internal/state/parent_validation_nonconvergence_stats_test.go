package state

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func TestFinishParentValidationNonConvergencePreservesStatsMarker(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusActive); err != nil {
		t.Fatal(err)
	}

	result := packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh}
	producer := ParentReviewProducer{Role: string(WorkerRole), Model: "opus"}
	if err := st.FinishParentValidationNonConvergence(result, producer); err != nil {
		t.Fatal(err)
	}

	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentReviewOpen == nil {
		t.Fatal("stats lost the open parent review")
	}
	if !stats.ParentReviewOpen.ParentValidationNonConvergence {
		t.Fatalf("stats lost non-convergence marker: %#v", stats.ParentReviewOpen)
	}
	if stats.ParentReviewOpen.PacketStatus != string(packet.StatusNeedsSolReview) ||
		stats.ParentReviewOpen.Role != string(WorkerRole) || stats.ParentReviewOpen.ModelAlias != "opus" {
		t.Fatalf("stats parent review metadata changed while preserving marker: %#v", stats.ParentReviewOpen)
	}
}
