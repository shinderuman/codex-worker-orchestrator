package parentactioncmd

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"testing"
)

func TestCompletionTerminalRequiresTypedObservation(t *testing.T) {
	for _, operation := range []controller.ParentCompletionOperation{{Operation: controller.PublicationOperationObserved}, {Operation: controller.PublicationOperationSuccessorStart}} {
		done, err := completionTerminalBoundary(operation)
		if err != nil || !done {
			t.Fatalf("typed completion was not terminal: %v", err)
		}
	}
	diagnostic := controller.ParentCompletionOperation{Operation: controller.PublicationOperationNone, Reason: "accepted candidate is already remotely observed"}
	if done, err := completionTerminalBoundary(diagnostic); err == nil || done {
		t.Fatal("diagnostic text was accepted as publication proof")
	}
}
