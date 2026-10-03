package app

import (
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestStatusRateLimitResumeMode(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	replaceZaiSelfResumeNow(t, func() time.Time { return now })

	cases := []struct {
		name  string
		reset time.Time
		want  string
	}{
		{name: "initial grace", reset: now.Add(time.Minute), want: zaiSelfResumeModeInitialGrace},
		{name: "post reset retry", reset: now.Add(-10 * time.Second), want: zaiSelfResumeModePostResetRetry},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := newAppConfig(t)
			st, err := state.NewStateStore(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.StartNewTask(); err != nil {
				t.Fatal(err)
			}
			checkpoint := state.ResumeCheckpoint{
				Stage:          state.ResumeStageWorker,
				Phase:          "worker-new",
				Role:           state.WorkerRole,
				Model:          "opus",
				Prompt:         "p",
				Request:        "req",
				StopKind:       state.ResumeStopRateLimited,
				ResetAtRFC3339: c.reset.Format(time.RFC3339),
			}
			if err := st.EnterStop(checkpoint); err != nil {
				t.Fatal(err)
			}

			output := executeStatusOutput(t, cfg)
			if got := output.RateLimited.ResumeMode; got != c.want {
				t.Fatalf("rate_limited.resume_mode = %q want %q", got, c.want)
			}
		})
	}
}
