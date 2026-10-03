package parentactioncmd

import (
	"os"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/failurepathadvisory"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

func TestMachineAcceptanceBlocksPublicationAndCompletionWithoutObservedEvidence(t *testing.T) {
	fixture := newMachineAcceptanceCompleteFixture(t, taskcontract.MachineFactFailurePathAdvisoryObserved)
	candidate, err := fixture.st.LoadPublicationCandidate()
	if err != nil {
		t.Fatal(err)
	}
	failure := verifyPromotedPublicationReadiness(fixture.cfg, fixture.st, candidate)
	if failure == nil || failure.Reason != publicationFailureRemoteNotReady || !strings.Contains(failure.Detail, publicationMachineAcceptanceGateName) {
		t.Fatalf("publication failure = %#v", failure)
	}

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusAwaiting || output.Completed {
		t.Fatalf("completion output = %#v", output)
	}
	if output.Failure == nil || output.Failure.Reason != publicationFailureRemoteNotReady || !strings.Contains(output.Failure.Detail, "normal-review-observed") {
		t.Fatalf("completion failure = %#v", output.Failure)
	}
}

func TestMachineAcceptanceBindsObservedEvidenceToCurrentTask(t *testing.T) {
	fixture := newMachineAcceptanceCompleteFixture(t, taskcontract.MachineFactFailurePathAdvisoryObserved)
	candidate, err := fixture.st.LoadPublicationCandidate()
	if err != nil {
		t.Fatal(err)
	}
	writeMachineAcceptanceRegistry(t, fixture, failurepathadvisory.Record{
		TaskID:  "other-task",
		Outcome: failurepathadvisory.OutcomeObserved,
		CallID:  "other-call",
	})
	if failure := verifyPromotedPublicationReadiness(fixture.cfg, fixture.st, candidate); failure == nil || !strings.Contains(failure.Detail, "unproven") {
		t.Fatalf("other-task evidence admitted = %#v", failure)
	}

	taskID, err := fixture.st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	writeMachineAcceptanceRegistry(t, fixture, failurepathadvisory.Record{
		TaskID:  taskID,
		Outcome: failurepathadvisory.OutcomeObserved,
		CallID:  "current-call",
	})
	if failure := verifyPromotedPublicationReadiness(fixture.cfg, fixture.st, candidate); failure != nil {
		t.Fatalf("current observed evidence rejected = %#v", failure)
	}
}

func TestMachineAcceptanceDistinguishesObservedFromSolVisibleAdvisory(t *testing.T) {
	fixture := newMachineAcceptanceCompleteFixture(t, taskcontract.MachineFactFailurePathAdvisoryShown)
	candidate, err := fixture.st.LoadPublicationCandidate()
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := fixture.st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	writeMachineAcceptanceRegistry(t, fixture, failurepathadvisory.Record{
		TaskID:  taskID,
		Outcome: failurepathadvisory.OutcomeObserved,
		CallID:  "observed-only",
		Advisory: &failurepathadvisory.AdvisoryOutcome{
			Status: failurepathadvisory.AdvisoryOmittedNoFind,
		},
	})
	if failure := verifyPromotedPublicationReadiness(fixture.cfg, fixture.st, candidate); failure == nil || !strings.Contains(failure.Detail, "sol-visible-advisory") {
		t.Fatalf("non-visible advisory admitted = %#v", failure)
	}

	writeMachineAcceptanceRegistry(t, fixture, failurepathadvisory.Record{
		TaskID:  taskID,
		Outcome: failurepathadvisory.OutcomeObserved,
		CallID:  "shown-call",
		Advisory: &failurepathadvisory.AdvisoryOutcome{
			Status:        failurepathadvisory.AdvisoryShown,
			FindingsShown: 1,
		},
	})
	if failure := verifyPromotedPublicationReadiness(fixture.cfg, fixture.st, candidate); failure != nil {
		t.Fatalf("Sol-visible advisory rejected = %#v", failure)
	}
}

func TestMachineAcceptanceFailsClosedOnCorruptEvidence(t *testing.T) {
	fixture := newMachineAcceptanceCompleteFixture(t, taskcontract.MachineFactFailurePathAdvisoryObserved)
	candidate, err := fixture.st.LoadPublicationCandidate()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.st.Path(failurepathadvisory.RegistryFile), []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	failure := verifyPromotedPublicationReadiness(fixture.cfg, fixture.st, candidate)
	if failure == nil || !strings.Contains(failure.Detail, "unreadable") {
		t.Fatalf("corrupt evidence failure = %#v", failure)
	}
}

func TestMachineAcceptanceAllowsCompletionAfterCurrentEvidenceExists(t *testing.T) {
	fixture := newMachineAcceptanceCompleteFixture(t, taskcontract.MachineFactFailurePathAdvisoryObserved)
	taskID, err := fixture.st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	writeMachineAcceptanceRegistry(t, fixture, failurepathadvisory.Record{
		TaskID:  taskID,
		Outcome: failurepathadvisory.OutcomeObserved,
		CallID:  "current-call",
	})
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusComplete || !output.Completed {
		t.Fatalf("completion output = %#v", output)
	}
}

func newMachineAcceptanceCompleteFixture(t *testing.T, fact taskcontract.MachineAcceptanceFact) *completeFixture {
	t.Helper()
	fixture := newCompleteRepositoryFixture(t)
	content := machineAcceptanceTaskContent(fact)
	writePushBindingFile(t, fixture.repo, "IMPLEMENTATION_TASKS/active.md", content)
	runFinalizationGit(t, fixture.repo, "add", "IMPLEMENTATION_TASKS/active.md")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-m", "declare machine acceptance")
	if err := fixture.st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/active.md", []byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := fixture.st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := fixture.st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.st.AcceptParentReview(); err != nil {
		t.Fatal(err)
	}
	fixture.commitParentMetadataSync(t)
	ensureCompleteFixturePublicationAuthority(t, fixture)
	return fixture
}

func machineAcceptanceTaskContent(fact taskcontract.MachineAcceptanceFact) string {
	return "# active\n\n" +
		"## Dependencies\n\n" +
		"## External feasibility\n\nstatus: not-applicable\n\n" +
		"## Machine-verifiable acceptance\n\n" +
		"{\"schema\":\"task-machine-acceptance/v1\",\"requirements\":[{\"id\":\"" + machineAcceptanceRequirementID(fact) + "\",\"fact\":\"" + string(fact) + "\"}]}\n"
}

func machineAcceptanceRequirementID(fact taskcontract.MachineAcceptanceFact) string {
	if fact == taskcontract.MachineFactFailurePathAdvisoryShown {
		return "sol-visible-advisory"
	}
	return "normal-review-observed"
}

func writeMachineAcceptanceRegistry(t *testing.T, fixture *completeFixture, record failurepathadvisory.Record) {
	t.Helper()
	if err := failurepathadvisory.SaveRegistry(fixture.st.Path(failurepathadvisory.RegistryFile), failurepathadvisory.Registry{
		Records: []failurepathadvisory.Record{record},
	}); err != nil {
		t.Fatal(err)
	}
}
