package parentactioncmd

import (
	"os"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestCompleteIgnoresMarkerlessForeignProjectProtocol(t *testing.T) {
	cases := []struct {
		name string
		plan string
	}{
		{name: "valid coincidental plan", plan: completeInitialPlan() + "\nforeign metadata\n"},
		{name: "malformed coincidental plan", plan: "foreign plan\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newCompleteFixture(t)
			if err := os.Remove(fixture.st.Path(repositoryharness.ActivationStateKey)); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(fixture.st.Path("active-task")); err != nil {
				t.Fatal(err)
			}
			writePushBindingFile(t, fixture.repo, "IMPLEMENTATION_PLAN.local.md", tc.plan)
			runFinalizationGit(t, fixture.repo, "add", "IMPLEMENTATION_PLAN.local.md")
			runFinalizationGit(t, fixture.repo, "commit", "-q", "-m", "foreign coincidental plan")
			runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")

			output := runCompleteCommand(t, fixture)
			if output.Status != completeStatusComplete || !output.Completed || output.ParentRequest != nil {
				t.Fatalf("foreign completion = %#v", output)
			}
			if output.RemoteSync == nil || output.RemoteSync.State != completeRemoteStateVerified || !output.RemoteSync.PostconditionMet {
				t.Fatalf("foreign remote sync = %#v", output.RemoteSync)
			}
		})
	}
}

func TestCompleteFailsClosedWhenTaskPinLacksActivationPin(t *testing.T) {
	fixture := newCompleteFixture(t)
	if err := os.Remove(fixture.st.Path(repositoryharness.ActivationStateKey)); err != nil {
		t.Fatal(err)
	}
	fixture.commitParentMetadataSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusAwaiting || output.Completed || output.ParentRequest != nil {
		t.Fatalf("activation mismatch completion = %#v", output)
	}
	if output.Failure == nil || output.Failure.Stage != "metadata" || output.Failure.Reason != "completion_transition_invalid" ||
		!strings.Contains(output.Failure.Detail, "activation pin") {
		t.Fatalf("activation mismatch failure = %#v", output.Failure)
	}
}

func pinCompleteRepositoryHarnessActive(t *testing.T, st *state.StateStore) {
	t.Helper()
	if err := st.Write(repositoryharness.ActivationStateKey, repositoryharness.ActivationActiveValue); err != nil {
		t.Fatal(err)
	}
}
