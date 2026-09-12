package app

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
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

func TestStatusRateLimitDerivesCSTFromCanonicalResetTime(t *testing.T) {
	st, err := state.NewStateStore(config.AppConfig{
		StateBase: t.TempDir(),
		RepoHash:  "rate-limit-reset-authority",
		RepoRoot:  t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:          state.ResumeStageWorker,
		Phase:          "worker",
		Model:          "opus",
		StopKind:       state.ResumeStopRateLimited,
		ResetAtCST:     "2099-01-01 00:00:00",
		ResetAtRFC3339: "2026-09-12T06:06:34Z",
	}); err != nil {
		t.Fatal(err)
	}

	var output statusOutput
	if !fillStatusCheckpoint(st, &output) {
		t.Fatal("rate-limit checkpoint must be resumable")
	}
	if output.RateLimited.ResetAtCST != "2026-09-12 14:06:34" {
		t.Fatalf("status reset_at_cst = %q", output.RateLimited.ResetAtCST)
	}
	if output.RateLimited.ResetAtRFC3339 == nil || *output.RateLimited.ResetAtRFC3339 != "2026-09-12T06:06:34Z" {
		t.Fatalf("status reset_at_rfc3339 = %#v", output.RateLimited.ResetAtRFC3339)
	}
}
