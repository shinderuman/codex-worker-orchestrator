package parentactioncmd

import (
	"errors"
	"io"
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

func TestRepositoryAwareResumeKeepsMarkerlessForeignGuardRecoveryGeneric(t *testing.T) {
	cfg, st, record := newGuardRepairLifecycleState(t)
	persistReadyGuardRepair(t, cfg, st, &record)
	marker := installFailingNormalWorker(t)

	if err := executeRepositoryAwareResume(cfg, io.Discard, io.Discard, nil); err == nil {
		t.Fatal("generic resume worker failureを期待")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("markerless foreign resume did not execute the generic worker: %v", err)
	}
	got, err := st.LoadGuardRepairRecord()
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.GuardRepairReady || got.OriginalResumeObserved {
		t.Fatalf("foreign resume executed repository guard repair: %#v", got)
	}
}

func TestRepositoryAwareResumeUsesBoundedRepairWhenActivated(t *testing.T) {
	cfg, st, record := newGuardRepairLifecycleState(t)
	pinCompleteRepositoryHarnessActive(t, st)
	writeGuardRepairWorkerModule(t, cfg.RepoRoot, guardRepairLifecycleEvidenceWorkerSource(t, st, state.TaskStatusActive, false))
	persistReadyGuardRepair(t, cfg, st, &record)
	marker := installFailingNormalWorker(t)

	if err := executeRepositoryAwareResume(cfg, io.Discard, io.Discard, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("activated repository repair redispatched the broken generic worker")
	}
	got, err := st.LoadGuardRepairRecord()
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.GuardRepairComplete || !got.OriginalResumeObserved {
		t.Fatalf("activated repository repair did not resume the original task: %#v", got)
	}
}

func pinCompleteRepositoryHarnessActive(t *testing.T, st *state.StateStore) {
	t.Helper()
	if err := st.Write(repositoryharness.ActivationStateKey, repositoryharness.ActivationActiveValue); err != nil {
		t.Fatal(err)
	}
}
