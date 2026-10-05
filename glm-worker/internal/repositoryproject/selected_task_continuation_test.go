package repositoryproject

import "testing"

func TestSelectedTaskParentRequestProjection(t *testing.T) {
	const task = "IMPLEMENTATION_TASKS/current.md"
	cases := []struct {
		name  string
		input SelectedTaskContinuationInput
		want  Continuation
	}{
		{
			name:  "current task",
			input: SelectedTaskContinuationInput{Task: task, RequiredAction: "none"},
			want:  Continuation{State: ContinuationContinueNow, Task: task, RequiredAction: "none", Reason: ReasonCurrentTask},
		},
		{
			name:  "not started",
			input: SelectedTaskContinuationInput{Task: task, RequiredAction: "none", StartRequired: true},
			want:  Continuation{State: ContinuationContinueNow, Task: task, RequiredAction: ActionStart, Reason: ReasonCurrentTask},
		},
		{
			name:  "rate limited",
			input: SelectedTaskContinuationInput{Task: task, RequiredAction: "resume", TemporaryBlockReason: "rate-limited"},
			want:  Continuation{State: ContinuationBlocked, Task: task, RequiredAction: "resume", Reason: "rate-limited"},
		},
		{
			name:  "provider unavailable",
			input: SelectedTaskContinuationInput{Task: task, RequiredAction: "resume", TemporaryBlockReason: "provider-unavailable"},
			want:  Continuation{State: ContinuationBlocked, Task: task, RequiredAction: "resume", Reason: "provider-unavailable"},
		},
		{
			name:  "interrupted",
			input: SelectedTaskContinuationInput{Task: task, RequiredAction: "resume", Interrupted: true},
			want:  Continuation{State: ContinuationExplicitStop, Task: task, RequiredAction: "resume", Reason: ReasonUserInterruption},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projection := SelectedTaskParentRequestProjection(tc.input)
			if projection.CompletionAdmitted || projection.StopAdmitted || projection.Continuation != tc.want {
				t.Fatalf("projection = %#v want continuation %#v", projection, tc.want)
			}
		})
	}
}

func TestSelectedTaskParentRequestProjectionFailsClosedWithoutTask(t *testing.T) {
	projection := SelectedTaskParentRequestProjection(SelectedTaskContinuationInput{RequiredAction: "none"})
	if projection.CompletionAdmitted || projection.StopAdmitted ||
		projection.Continuation.State != ContinuationUnknown ||
		projection.Continuation.Reason != ReasonActiveTaskUnresolved {
		t.Fatalf("projection = %#v", projection)
	}
}
