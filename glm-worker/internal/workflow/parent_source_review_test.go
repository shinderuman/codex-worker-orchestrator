package workflow

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"reflect"
	"testing"
)

func TestReenteredReviewUsesSavedImplementedPacketWithoutAnotherWorkerCall(t *testing.T) {
	w, _ := acceptedResultRecoveryFixture(t)
	taskID, err := w.state.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	before, err := w.state.ReadModelCallLogs(taskID)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := w.state.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	result, err := w.previousImplementedReviewContext()
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != packet.StatusImplemented || result.Summary != "accepted" {
		t.Fatalf("saved packet differs: %#v", result)
	}
	after, err := w.state.ReadModelCallLogs(taskID)
	if err != nil {
		t.Fatal(err)
	}
	current, err := w.state.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) || !reflect.DeepEqual(current, checkpoint) {
		t.Fatal("context retrieval changed recovery state or invoked a model")
	}
	call := before[len(before)-1]
	if savedWorkerReviewContextMatches(call, "different-task") {
		t.Fatal("different Task context accepted")
	}
	call.ResponseSHA256 = "wrong-digest"
	if savedWorkerReviewContextMatches(call, taskID) {
		t.Fatal("corrupt response context accepted")
	}
}

func TestReenteredReviewPreparationRejectsExistingRecoveryCheckpoint(t *testing.T) {
	w, _ := acceptedResultRecoveryFixture(t)
	before := w.state.TaskStatus()
	if err := w.PrepareReenteredReview(); err == nil {
		t.Fatal("existing recovery replaced by review")
	}
	if w.state.TaskStatus() != before || before != state.TaskStatusResultFormatRecoverable {
		t.Fatal("rejected preparation changed terminal recovery")
	}
}
