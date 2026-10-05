package app

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestCanonicalParentAcceptanceRequiresCurrentAttemptAndProjectsPublication(t *testing.T) {
	cfg := newCanonicalAppConfig(t)
	st := startParentHandoffTask(t, cfg)
	admission, err := controller.Activate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []string{"", "unrelated-attempt"} {
		if err := st.Write(state.ControllerAttemptStateFile, attempt); err != nil {
			t.Fatal(err)
		}
		err := Execute(Command{Mode: ModeAccept}, cfg, nil, io.Discard, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "current controller attempt") {
			t.Fatalf("unbound acceptance: %v", err)
		}
		if st.OpenParentReviewLabel() != string(packet.StatusPass) {
			t.Fatal("rejected acceptance consumed review")
		}
	}
	if err := st.Write(state.ControllerAttemptStateFile, admission.Attempt.AttemptID); err != nil {
		t.Fatal(err)
	}
	if err := Execute(Command{Mode: ModeAccept}, cfg, nil, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	handoff := buildParentHandoffWithConfig(cfg, st)
	if !handoff.Consistent || handoff.Controller == nil || handoff.RequiredAction == nil || *handoff.RequiredAction != "controller-publication" {
		t.Fatalf("canonical publication handoff: %#v", handoff)
	}
	raw, err := json.Marshal(handoff)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		ActionSpecs map[string]parentHandoffActionSpec `json:"action_specs"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	spec := decoded.ActionSpecs["controller-publication"]
	if spec.Kind != "staged" || len(spec.PrepareCommand) != 3 {
		t.Fatalf("publication transport: %#v", spec)
	}
	for _, retired := range []string{"complete", "install", "park", "unpark"} {
		if containsParentAction(handoff.AllowedActions, retired) {
			t.Fatalf("retired action advertised: %s", retired)
		}
	}
}

func TestExistingParentResumeDoesNotBootstrapController(t *testing.T) {
	cfg := newCanonicalAppConfig(t)
	st := startParentHandoffTask(t, cfg)
	checkpoint := state.ResumeCheckpoint{Stage: state.ResumeStageWorker, Phase: "worker-new", Role: state.WorkerRole, Model: "worker", Request: "saved", Prompt: "saved", OriginalPrompt: "saved", StopKind: state.ResumeStopInterrupted}
	if err := st.EnterStop(checkpoint); err != nil {
		t.Fatal(err)
	}
	err := Execute(Command{Mode: ModeResume}, cfg, nil, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "existing workflow requires canonical") {
		t.Fatalf("resume admitted legacy state: %v", err)
	}
	if exists, err := controller.Exists(cfg); err != nil || exists {
		t.Fatalf("rejected resume minted controller: %v %v", exists, err)
	}
	if got, err := st.LoadResumeCheckpoint(); err != nil || got.Request != "saved" {
		t.Fatalf("saved request lost: %#v %v", got, err)
	}
}
