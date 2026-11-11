package workflow

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestScheduledPauseWithSecondaryErrorsKeepsResumeCheckpoint(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		secondary error
	}{
		{
			name:      "retry exhausted",
			secondary: &runner.StructuredOutputError{Subtype: "error_max_structured_output_retries"},
		},
		{
			name:      "output write failure",
			secondary: &os.PathError{Op: "write", Path: "output.log", Err: os.ErrPermission},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st := newStateStoreT(t)
			if err := st.Write("worker.id", "test-session"); err != nil {
				t.Fatal(err)
			}
			resumeAt := time.Now().Add(time.Hour).Truncate(time.Second)
			paused := &runner.ScheduledPauseError{ResumeAt: resumeAt, Started: true}
			r := &scriptedRunner{steps: []runnerStep{{
				result: runner.RunResult{SessionID: "test-session"},
				runErr: errors.Join(paused, tc.secondary),
			}}}
			w := newWorkflowT(t, st, r)
			w.temp = t.TempDir()

			_, err := w.runModel(workerCheckpoint())
			var gotPause *runner.ScheduledPauseError
			if !errors.As(err, &gotPause) || !gotPause.ResumeAt.Equal(resumeAt) {
				t.Fatalf("workflow discarded scheduled pause: %v", err)
			}
			cp, loadErr := st.LoadResumeCheckpoint()
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if cp.StopKind != state.ResumeStopScheduledPause || cp.ScheduledResumeAt != resumeAt.Format(time.RFC3339) ||
				cp.Phase != "worker-new" || cp.Role != state.WorkerRole || cp.Model != "opus" || cp.Request != "req" {
				t.Fatalf("scheduled resume checkpoint was not retained: %#v", cp)
			}
			if st.TaskStatus() != state.TaskStatusScheduledPause || !st.Exists("worker.ready") ||
				st.ReadOr("worker.id", "") != "test-session" {
				t.Fatalf("scheduled resume status/session were lost: status=%q ready=%t id=%q",
					st.TaskStatus(), st.Exists("worker.ready"), st.ReadOr("worker.id", ""))
			}
			logs := taskLogs(t, st)
			found := false
			for _, log := range logs {
				if log.CallType == state.CallTypeTask && log.Outcome == "scheduled_pause" {
					found = true
					if !strings.Contains(log.Error, tc.secondary.Error()) {
						t.Fatalf("secondary failure was lost from scheduled-pause diagnostics: %q", log.Error)
					}
				}
			}
			if !found {
				t.Fatalf("scheduled pause call outcome was not recorded: %#v", logs)
			}
			if !strings.Contains(err.Error(), "GLM invocation paused") {
				t.Fatalf("scheduled pause not surfaced: %v", err)
			}
		})
	}
}

func TestNonPauseStructuredFailureDoesNotCreateScheduledCheckpoint(t *testing.T) {
	t.Parallel()
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{
		runErr: &runner.StructuredOutputError{Subtype: "error_max_structured_output_retries"},
	}}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(workerCheckpoint())
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) {
		t.Fatalf("non-pause structured failure not reported: %v", err)
	}
	if _, loadErr := st.LoadResumeCheckpoint(); loadErr == nil {
		t.Fatal("non-pause structured failure retained a scheduled checkpoint")
	}
	if st.TaskStatus() == state.TaskStatusScheduledPause {
		t.Fatalf("non-pause error changed task status to scheduled-pause: %s", st.TaskStatus())
	}
}
