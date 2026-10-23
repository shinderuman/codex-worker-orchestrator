package controller

import (
	"os"
	"reflect"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type runtimeDispositionFixture struct {
	fixture   findingAcceptanceFixture
	policy    PublicationPolicy
	head      RepositoryControllerHead
	candidate AcceptedCandidate
	runtime   *state.StateStore
}

func interruptedRuntimeDisposition(t *testing.T) runtimeDispositionFixture {
	t.Helper()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "implementation\n")
	promoted, candidate := promoteTerminalTestSource(t, fixture, policy, source)
	runtime := fixture.store.dispositionRuntime(fixture.child)
	if err := os.MkdirAll(runtime.Path("."), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.WaitForDecision(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Write("last-request", "old unfinished request"); err != nil {
		t.Fatal(err)
	}
	input, err := fixture.store.PreviewRuntimeDisposition(promoted.Head)
	if err != nil || input == nil {
		t.Fatalf("preview: %v", err)
	}
	input.Reason = "explicit termination preserving unfinished evidence"
	if err := os.Mkdir(runtime.Path(state.RuntimeDispositionFile), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.DisposeUnfinishedRuntime(*input); err == nil {
		t.Fatal("injected disposition write failure ignored")
	}
	head, err := fixture.store.LoadHead()
	if err != nil || head.PendingTransitionID == "" {
		t.Fatalf("pending disposition: %v", err)
	}
	return runtimeDispositionFixture{fixture: fixture, policy: policy, head: head, candidate: candidate, runtime: runtime}
}

func TestUnfinishedRuntimeDispositionRecoveryPreservesEvidence(t *testing.T) {
	f := interruptedRuntimeDisposition(t)
	if f.runtime.TaskStatus() != state.TaskStatusWaitingDecision {
		t.Fatal("failed disposition completed Runtime")
	}
	if pending, err := f.fixture.store.PendingCompletionRecovery(f.head); err != nil || !pending {
		t.Fatalf("missing journal recovery: %v", err)
	}
	if err := os.Remove(f.runtime.Path(state.RuntimeDispositionFile)); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(f.fixture.store.config)
	if err != nil {
		t.Fatal(err)
	}
	result, err := reopened.RecoverExecutionOperation(f.head.PendingTransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.RecoverExecutionOperation(f.head.PendingTransitionID); err != nil {
		t.Fatalf("lost-result replay: %v", err)
	}
	if result.Head.RootTaskRef.TaskPath != f.candidate.TaskRef.TaskPath || f.runtime.TaskStatus() != state.TaskStatusUserTerminated || !f.runtime.Exists("pending-decision") {
		t.Fatal("disposition changed Task scope or fabricated completion")
	}
	published, err := reopened.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: result.Head.ControllerGeneration, CandidateID: f.candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := reopened.LoadEvidenceHead(*published.Head.EvidenceHeadRef)
	if err != nil {
		t.Fatal(err)
	}
	preserved := false
	for _, task := range evidence.TaskHeads {
		revision, err := reopened.LoadTaskIndexRevision(task.RevisionRef)
		if err != nil {
			t.Fatal(err)
		}
		if revision.TaskRef != f.fixture.child {
			continue
		}
		bundle, err := reopened.BuildTaskEvidenceBundle(task.RevisionRef)
		if err != nil {
			t.Fatal(err)
		}
		for _, object := range bundle.Objects {
			if object.Ref.Kind == "runtime-state" && string(object.Data) == "old unfinished request\n" {
				preserved = true
			}
		}
	}
	if !preserved {
		t.Fatal("published evidence graph lost original Runtime request")
	}
	if f.runtime.TaskStatus() != state.TaskStatusUserTerminated || f.runtime.Exists("worker.id") {
		t.Fatal("publication started successor Runtime")
	}
}

func TestUnfinishedRuntimeDispositionRejectsChangedSource(t *testing.T) {
	f := interruptedRuntimeDisposition(t)
	if err := os.Remove(f.runtime.Path(state.RuntimeDispositionFile)); err != nil {
		t.Fatal(err)
	}
	if err := f.runtime.Write("last-request", "changed after preparation"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.fixture.store.RecoverExecutionOperation(f.head.PendingTransitionID); err == nil {
		t.Fatal("changed Runtime inherited termination approval")
	}
	current, err := f.fixture.store.LoadHead()
	if err != nil || !reflect.DeepEqual(current, f.head) || f.runtime.TaskStatus() != state.TaskStatusWaitingDecision {
		t.Fatal("rejected recovery changed authority")
	}
	if observeTestRemote(t, f.fixture.store.identity.PrimaryRoot, f.policy) != f.candidate.BaseOID {
		t.Fatal("rejected recovery changed remote")
	}
}

func TestStaleActiveRuntimeNeedsExplicitDisposition(t *testing.T) {
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "implementation\n")
	promoted, _ := promoteTerminalTestSource(t, fixture, policy, source)
	runtime := fixture.store.dispositionRuntime(fixture.child)
	if err := os.MkdirAll(runtime.Path("."), 0700); err != nil {
		t.Fatal(err)
	}
	id, err := runtime.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	input, err := fixture.store.PreviewRuntimeDisposition(promoted.Head)
	if err != nil || input == nil || input.RuntimeTaskID != id {
		t.Fatalf("stale active Runtime lost disposition: %v", err)
	}
	target := promoted.Head
	target.RootTaskRef = &fixture.child
	if _, err := fixture.store.QuiescentRuntimeAdmission(target); err == nil {
		t.Fatal("stale active Runtime admitted a fresh task")
	}
}
