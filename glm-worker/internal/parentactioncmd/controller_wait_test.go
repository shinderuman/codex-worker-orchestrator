package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestControllerTransportReturnsTerminalAndWaitSurvivesTaskHandover(t *testing.T) {
	cfg := newControllerActionTestConfig(t, false)
	cfg.RepositoryLockPath = filepath.Join(t.TempDir(), "repository.workflow.lock")
	bin := t.TempDir()
	handoff := `{"projection":"recovery","consistent":true,"controller":{"canonical_active":true,"head":{"repository_identity":"repo"}},"task_id":null,"task_status":null,"required_action":"start","allowed_actions":["start"]}`
	command := `{"action":"adopt","expected_generation":1}`
	projected, err := json.Marshal(map[string]any{"consistent": true, "required_action": controllerExecutionAction, "required_action_parameters": map[string]string{"controller_command_json": command}})
	if err != nil {
		t.Fatal(err)
	}
	stub := fmt.Sprintf(`#!/bin/sh
case "$1" in
 --authority) if [ "$2" = controller-execution-plan ]; then printf '%%s\n' '%s'; else cat; fi;;
 --handoff) printf '%%s\n' '%s';;
 *) exit 9;;
esac
`, projected, handoff)
	if err := execfile.Write(filepath.Join(bin, "glm-worker"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	prepared, err := parentaction.PrepareStaged(cfg.RepoRoot, controllerExecutionAction, parentaction.StagedTemplate{Payload: []byte("COMMAND_TEMPLATE: " + command + "\n"), Slots: []parentaction.PreparedSlot{}})
	if err != nil {
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
