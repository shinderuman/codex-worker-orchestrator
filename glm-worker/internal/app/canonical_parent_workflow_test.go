package app

import (
	"io"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRejectedRetainedWorkflowDoesNotActivateController(t *testing.T) {
	cfg := newAppConfig(t)
	if _, err := state.NewStateStore(cfg); err != nil {
		t.Fatal(err)
	}

	exists, err := controller.Exists(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("controller exists before rejected parent action")
	}

	if err := Execute(Command{Mode: ModeDecision, Payload: "reject"}, cfg, nil, io.Discard, io.Discard); err == nil {
		t.Fatal("decision unexpectedly admitted")
	}

	exists, err = controller.Exists(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("rejected parent action activated canonical controller")
	}
}
