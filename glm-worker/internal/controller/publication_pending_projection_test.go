package controller

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestPendingCompletionProjectionValidatesJournalBeforeHandoff(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "pending-projection.txt", "pending completion\n")
	acceptedCompletionRuntime(t, fixture.store, source)
	promoted, candidate := promoteTerminalTestSource(t, fixture, policy, source)
	retired, _ := prepareCompletionTestCandidate(t, fixture.store, promoted, candidate)
	head, err := fixture.store.LoadHead()
	if err != nil || head.PendingTransitionID != retired.TransitionID {
		t.Fatalf("missing pending completion journal: %#v err=%v", head, err)
	}
	projected, err := fixture.store.ProjectParentCompletion()
	if err != nil || projected.Operation != PublicationOperationRecover ||
		projected.Surface != ExecutionOperationSurface || projected.Command != nil ||
		projected.Execution == nil || projected.Execution.Action != PublicationOperationRecover ||
		projected.Execution.ExpectedGeneration != head.ControllerGeneration ||
		projected.Execution.TransitionID != head.PendingTransitionID {
		t.Fatalf("pending completion not projected as typed execution recovery: %#v err=%v", projected, err)
	}
	path := fixture.store.executionOperationPath(head.PendingTransitionID)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var operation ExecutionOperation
	if err := json.Unmarshal(original, &operation); err != nil {
		t.Fatal(err)
	}
	operation.Transition.CommittedGeneration++
	stale, err := json.Marshal(operation)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name string
		data []byte
	}{
		{name: "stale-generation", data: stale},
		{name: "corrupt-journal", data: []byte("{")},
		{name: "missing-journal"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if scenario.data == nil {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, scenario.data, 0o600); err != nil {
				t.Fatal(err)
			}
			if operation, err := fixture.store.ProjectParentCompletion(); err == nil {
				t.Fatalf("invalid pending journal admitted operation: %#v", operation)
			}
			after, err := fixture.store.LoadHead()
			if err != nil || !reflect.DeepEqual(after, head) {
				t.Fatalf("read-only projection modified Controller: %#v err=%v", after, err)
			}
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
		})
	}
	restored, err := fixture.store.ProjectParentCompletion()
	if err != nil || !reflect.DeepEqual(restored, projected) {
		t.Fatalf("journal restoration lost deterministic recovery: %#v err=%v", restored, err)
	}
}
