package workflow

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExecutionProgressBandUsesCoarseMilestonePosition(t *testing.T) {
	cases := []struct {
		name         string
		currentIndex int
		phaseStage   string
		want         string
	}{
		{name: "second of ten", currentIndex: 1, phaseStage: "implementation", want: "early-to-middle"},
		{name: "fourth of ten", currentIndex: 3, phaseStage: "review", want: "early-to-middle"},
		{name: "fifth of ten", currentIndex: 4, phaseStage: "implementation", want: "middle"},
		{name: "eighth of ten", currentIndex: 7, phaseStage: "implementation", want: "middle-to-late"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := executionProgressBand(tc.currentIndex, 10, tc.phaseStage, state.TaskStatusActive); got != tc.want {
				t.Fatalf("band = %q want %q", got, tc.want)
			}
		})
	}
}
