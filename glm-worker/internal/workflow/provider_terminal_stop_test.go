package workflow

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestTerminalZaiProviderFailurePersistsResumableStop(t *testing.T) {
	tests := []struct {
		name  string
		class runner.ProviderFailureClass
		want  string
	}{
		{
			name: "long quota",
			class: runner.ProviderFailureClass{
				Kind:         runner.ProviderFailureZaiLongQuota,
				BusinessCode: "1317",
			},
			want: "zai-long-quota:1317",
		},
		{
			name: "action required",
			class: runner.ProviderFailureClass{
				Kind:         runner.ProviderFailureZaiActionRequired,
				BusinessCode: "1309",
			},
			want: "zai-action-required:1309",
		},
		{
			name: "unknown safe stop",
			class: runner.ProviderFailureClass{
				Kind:         runner.ProviderFailureZaiUnknownSafeStop,
				BusinessCode: "1999",
			},
			want: "zai-unknown-safe-stop:1999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _ := newReviewerDiffWorkflow(t, "")
			checkpoint := state.ResumeCheckpoint{
				Stage: state.ResumeStageWorker,
				Phase: "worker-new",
				Role:  state.WorkerRole,
				Model: "test-model",
			}
			startedAt := time.Now().UTC().Add(-time.Second)
			completedAt := time.Now().UTC()
			outputPath := filepath.Join(t.TempDir(), "output.log")

			err := w.saveTerminalProviderStop(
				checkpoint,
				tt.class,
				runner.RunResult{},
				startedAt,
				completedAt,
				errors.New("provider failure"),
				outputPath,
			)
			var stopped *runner.ProviderUnavailableError
			if !errors.As(err, &stopped) {
				t.Fatalf("terminal provider error = %v", err)
			}
			if stopped.Classification != tt.want || stopped.Probes != 1 {
				t.Fatalf("terminal provider error = %#v", stopped)
			}
			if got := w.state.TaskStatus(); got != state.TaskStatusProviderUnavailable {
				t.Fatalf("task status = %s", got)
			}
			stored, loadErr := w.state.LoadResumeCheckpoint()
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if stored.StopKind != state.ResumeStopProviderUnavailable || stored.ProviderUnavailableClassification != tt.want || stored.ProviderUnavailableProbes != 1 {
				t.Fatalf("stored checkpoint = %#v", stored)
			}
			plan, planErr := w.state.ParentActionPlan()
			if planErr != nil {
				t.Fatal(planErr)
			}
			if plan.RequiredAction != state.ParentActionResume || !plan.Allows(state.ParentActionResume) {
				t.Fatalf("parent action plan = %#v", plan)
			}
		})
	}
}

func TestTerminalZaiProviderClassesDoNotUseTransientRetry(t *testing.T) {
	for _, class := range []runner.ProviderFailureClass{
		{Kind: runner.ProviderFailureZaiLongQuota, BusinessCode: "1317"},
		{Kind: runner.ProviderFailureZaiActionRequired, BusinessCode: "1309"},
		{Kind: runner.ProviderFailureZaiUnknownSafeStop, BusinessCode: "1999"},
	} {
		if !isTerminalProviderFailureClass(class) {
			t.Fatalf("terminal class not recognized: %#v", class)
		}
		if class.Kind == runner.ProviderFailureTransient || class.Kind == runner.ProviderFailureZaiFiveHour {
			t.Fatalf("terminal class entered retry/self-resume class: %#v", class)
		}
	}
}
