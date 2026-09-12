package app

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
)

func TestRateLimitDetailDerivesCSTFromCanonicalResetTime(t *testing.T) {
	detail := rateLimitDetail(runner.ZaiRateLimitError{
		Phase: "worker",
		Limit: runner.ZaiFiveHourLimit{
			ResetAtCST:     "2099-01-01 00:00:00",
			ResetAtRFC3339: "2026-09-12T06:06:34Z",
		},
	})

	got, ok := detail["reset_at_cst"].(*string)
	if !ok || got == nil {
		t.Fatalf("reset_at_cst detail = %#v", detail["reset_at_cst"])
	}
	if *got != "2026-09-12 14:06:34" {
		t.Fatalf("reset_at_cst = %q", *got)
	}
}
