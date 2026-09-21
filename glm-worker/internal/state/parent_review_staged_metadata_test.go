package state

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func TestRecordParentOutcomePreservesGLMReviewerCrossCuttingInvariant(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	recordPacket(t, st, packet.StatusNeedsSolReview, packet.RiskHigh, ParentReviewProducer{Role: "reviewer", Model: "haiku"})

	resolved, err := st.RecordParentOutcome(ParentOutcomeFix, ParentOriginGLMReviewer, ParentCauseCrossCuttingInvariant)
	if err != nil || !resolved {
		t.Fatalf("fix outcome resolution failed: resolved=%v err=%v", resolved, err)
	}
	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentFixOrigins[ParentOriginGLMReviewer] != 1 || stats.ParentFixOrigins[ParentOriginUnknown] != 0 {
		t.Fatalf("fix origins = %#v", stats.ParentFixOrigins)
	}
	logs, err := st.ReadModelCallLogs(st.ReadOr("task.id", ""))
	if err != nil || len(logs) != 1 {
		t.Fatalf("outcome telemetry = %#v err=%v", logs, err)
	}
	if logs[0].ParentOrigin != ParentOriginGLMReviewer || logs[0].ParentCause != ParentCauseCrossCuttingInvariant {
		t.Fatalf("outcome telemetry metadata = %#v", logs[0])
	}
}
