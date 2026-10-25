package app

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestTypedCompletionCanRejectAfterAcceptWithoutCreatingCandidate(t *testing.T) {
	binary := typedCompletionWorkerBinary(t)
	cfg, runtime, _ := typedCompletionWorkflowFixture(t, binary, false)
	taskID := runtime.ReadOr("task.id", "")
	var output bytes.Buffer
	if err := Execute(Command{Mode: ModeHandoff}, cfg, nil, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	var machine struct {
		parentHandoffOutput
		Specs map[string]parentHandoffActionSpec `json:"action_specs"`
	}
	if err := json.Unmarshal(output.Bytes(), &machine); err != nil {
		t.Fatal(err)
	}
	spec := machine.Specs[controllerPublicationSurface]
	if len(spec.OptionalParameters) != 1 || spec.OptionalParameters[0] != "operation" || !strings.Contains(strings.Join(spec.Choices["operation"], ","), controller.PublicationOperationReopen) {
		t.Fatal("handoff removed the semantic rejection choice")
	}
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runTypedParentActionCLI(t, cfg, append(spec.PrepareCommand[1:], "--operation", "publish")...); code == 0 {
		t.Fatal("prepare selected a publication operation outside the live acceptance boundary")
	}
	code, result, diagnostics := runTypedPublicationCLI(t, cfg, machine.RequiredActionParameters, map[string]string{"message": "parent rejects before candidate fixation"}, "--operation", controller.PublicationOperationReopen)
	if code != 0 {
		t.Fatal(diagnostics)
	}
	if runtime.TaskStatus() != state.TaskStatusWaitingSolReview || runtime.ReadOr("task.id", "") != taskID {
		t.Fatal("rejection did not reopen the same Runtime Task")
	}
	after, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if after.AcceptedCandidateRef != nil || after.LiveAttemptID != before.LiveAttemptID {
		t.Fatal("rejection sealed a candidate or replaced the attempt")
	}
	var envelope struct {
		Terminal struct {
			Source controller.EvidenceObjectRef `json:"source_evidence"`
		} `json:"terminal"`
	}
	if err := json.Unmarshal([]byte(result), &envelope); err != nil {
		t.Fatal(err)
	}
	data, err := store.LoadEvidenceObject(envelope.Terminal.Source)
	if err != nil {
		t.Fatal(err)
	}
	var retained struct {
		Source struct {
			Review state.ParentReviewState `json:"review"`
		} `json:"source"`
	}
	if err := json.Unmarshal(data, &retained); err != nil {
		t.Fatal(err)
	}
	if retained.Source.Review.Completion == nil || retained.Source.Review.Completion.Terminal != state.SessionRotationTerminalAccept {
		t.Fatal("semantic rejection discarded the original acceptance")
	}
}
