package report

import (
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestStageTimelineOpenAttemptDoesNotCreateFalseParentWaitGap(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	open := state.ParentActionAttemptRecord{EnteredAt: base}
	next := state.ParentActionAttemptRecord{EnteredAt: base.Add(30 * time.Second), ExitedAt: base.Add(40 * time.Second)}
	start, end := base.Add(10*time.Second), base.Add(20*time.Second)
	for _, order := range [][]state.ParentActionAttemptRecord{{open, next}, {next, open}} {
		if got := classifyStageTimelineGap(start, end, order, nil); got != stageTimelineGapUnattributed {
			t.Fatalf("open attempt + successor falsely classified as %s parent wait", got)
		}
	}
}

func TestStageTimelineGapClassificationPreservesClosedAttemptSemantics(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	prior := state.ParentActionAttemptRecord{EnteredAt: base, ExitedAt: base.Add(10 * time.Second)}
	next := state.ParentActionAttemptRecord{EnteredAt: base.Add(30 * time.Second), ExitedAt: base.Add(40 * time.Second)}
	open := state.ParentActionAttemptRecord{EnteredAt: base.Add(4 * time.Second)}
	cases := []struct {
		name     string
		start    time.Time
		end      time.Time
		attempts []state.ParentActionAttemptRecord
		want     string
	}{
		{"gap between completed attempts", base.Add(11 * time.Second), base.Add(29 * time.Second), []state.ParentActionAttemptRecord{prior, next}, stageTimelineGapParentWait},
		{"gap with completed predecessor and open attempt", base.Add(11 * time.Second), base.Add(29 * time.Second), []state.ParentActionAttemptRecord{open, prior, next}, stageTimelineGapParentWait},
		{"gap contained in closed attempt", base.Add(2 * time.Second), base.Add(8 * time.Second), []state.ParentActionAttemptRecord{prior, next}, stageTimelineGapInAttempt},
		{"gap with no ended predecessor", base.Add(11 * time.Second), base.Add(29 * time.Second), []state.ParentActionAttemptRecord{open, next}, stageTimelineGapUnattributed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyStageTimelineGap(tc.start, tc.end, tc.attempts, nil); got != tc.want {
				t.Fatalf("classification=%s want=%s", got, tc.want)
			}
		})
	}
}
