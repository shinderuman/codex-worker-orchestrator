package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestControllerTransportReturnsTerminalAndWaitSurvivesTaskHandover(t *testing.T) {
	cfg := newControllerActionTestConfig(t, false)
	cfg.RepositoryLockPath = filepath.Join(t.TempDir(), "repository.workflow.lock")
	bin := t.TempDir()
	handoff := `{"projection":"recovery","consistent":true,"controller":{"canonical_active":true,"head":{"repository_identity":"repo"}},"task_id":null,"task_status":null,"required_action":"start","allowed_actions":["start"]}`
	stub := "#!/bin/sh\ncase \"$1\" in\n --authority) cat;;\n --handoff) printf '%s\\n' '" + handoff + "' ;;\n *) exit 9;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "glm-worker"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	prepared, err := parentaction.Prepare(cfg.RepoRoot, "controller-publication")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte("__GLM_PARENT_ACTION_PAYLOAD__"), []byte(`{"action":"adopt","expected_generation":1}`), 1)
	if err := os.WriteFile(prepared.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := executeWithTerminalEnvelope(cfg, []string{prepared.Action, prepared.Token}, &stdout, &stderr); err != nil {
		t.Fatalf("controller action: %v %s", err, stderr.String())
	}
	var envelope parentActionTerminalEnvelopePayload
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Status != "parent_action_terminal" || len(envelope.Terminal) == 0 || len(envelope.Handoff) == 0 {
		t.Fatalf("lost terminal/handoff: %s", stdout.String())
	}
	cfg.RepoHash = "successor-runtime"
	stdout.Reset()
	if err := executeParentWait(cfg, []string{"wait"}, &stdout, &stderr); err != nil {
		t.Fatalf("post-handover wait: %v %s", err, stderr.String())
	}
	var recovered parentWaitOutput
	if err := json.Unmarshal(stdout.Bytes(), &recovered); err != nil {
		t.Fatal(err)
	}
	if recovered.Status != parentWaitStatusReleased || recovered.OwnerLost {
		t.Fatalf("unexpected recovery: %#v", recovered)
	}
	if state.AttachStateStore(cfg).Exists("task.id") {
		t.Fatal("controller wait initialized successor Runtime")
	}
}

func TestParentWaitPreservesControllerInconsistency(t *testing.T) {
	raw := []byte(`{"projection":"recovery","consistent":false,"inconsistency":"pending journal integrity mismatch","controller":{"canonical_active":true,"head":{"repository_identity":"repo"}},"task_id":null,"task_status":null,"required_action":null,"allowed_actions":[]}`)
	if err := validateParentWaitRecoveryHandoff(raw); err != nil {
		t.Fatalf("controller diagnostic was masked by Runtime validation: %v", err)
	}
	raw = bytes.Replace(raw, []byte(`"pending journal integrity mismatch"`), []byte(`null`), 1)
	if err := validateParentWaitRecoveryHandoff(raw); err == nil {
		t.Fatal("inconsistent recovery without diagnostic was accepted")
	}
}
