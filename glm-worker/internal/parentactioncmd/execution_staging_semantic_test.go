package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

func TestExecutionReasonStagingBindsOnlyDeclaredTypedOperations(t *testing.T) {
	cases := []struct {
		name, action, field, label, reason string
	}{
		{"runtime", "dispose-runtime", "runtime_disposition", "RUNTIME_DISPOSITION_REASON", "旧Runtimeを明示終了する"},
		{"execution", "user-terminate-execution", "execution_termination", "EXECUTION_TERMINATION_REASON", "explicit: $HOME, shell quote"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			command, err := json.Marshal(map[string]any{"action": tc.action, "expected_generation": 7, tc.field: map[string]any{"expected_generation": 7, "reason": "", "source_attempt_id": "exact-source"}})
			if err != nil {
				t.Fatal(err)
			}
			payload := fmt.Sprintf("%s: %s\nCOMMAND_TEMPLATE: %s\n", tc.label, tc.reason, command)
			actual, err := mergeExecutionStagedPayload([]byte(payload))
			if err != nil {
				t.Fatal(err)
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(actual, &object); err != nil {
				t.Fatal(err)
			}
			var action string
			if err := json.Unmarshal(object["action"], &action); err != nil || action != tc.action {
				t.Fatalf("operation changed: %v %s", err, actual)
			}
			var target map[string]any
			if err := json.Unmarshal(object[tc.field], &target); err != nil {
				t.Fatal(err)
			}
			if target["reason"] != tc.reason || target["source_attempt_id"] != "exact-source" || target["expected_generation"] != float64(7) {
				t.Fatalf("reason or authority changed: %#v", target)
			}
		})
	}
}

func TestExecutionReasonStagingFailsClosed(t *testing.T) {
	tests := []string{
		"UNKNOWN: x\nCOMMAND_TEMPLATE: {\"action\":\"user-terminate-execution\"}\n",
		"EXECUTION_TERMINATION_REASON: reason\nCOMMAND_TEMPLATE: {\"action\":\"dispose-runtime\",\"execution_termination\":{}}\n",
		"RUNTIME_DISPOSITION_REASON: reason\nCOMMAND_TEMPLATE: {\"action\":\"user-terminate-execution\",\"runtime_disposition\":{}}\n",
		"EXECUTION_TERMINATION_REASON: reason\nCOMMAND_TEMPLATE: {\"action\":\"user-terminate-execution\"}\n",
		"EXECUTION_TERMINATION_REASON: reason\nEXECUTION_TERMINATION_REASON: another\nCOMMAND_TEMPLATE: {\"action\":\"user-terminate-execution\",\"execution_termination\":{}}\n",
		"RUNTIME_DISPOSITION_REASON: reason\nEXECUTION_TERMINATION_REASON: other\nCOMMAND_TEMPLATE: {\"action\":\"user-terminate-execution\",\"execution_termination\":{},\"runtime_disposition\":{}}\n",
		"EXECUTION_TERMINATION_REASON: reason\nCOMMAND_TEMPLATE: {\"action\":\"user-terminate-execution\",\"execution_termination\":{\"reason\":\"existing\"}}\n",
	}
	for _, input := range tests {
		if got, err := mergeExecutionStagedPayload([]byte(input)); err == nil {
			t.Fatalf("unbound reason accepted: %s", got)
		}
	}
}

func TestExecutionTerminationMachineBindingPreventsTamperingAndReplay(t *testing.T) {
	cfg := newControllerActionTestConfig(t, true)
	bin := t.TempDir()
	command := "{\"action\":\"user-terminate-execution\",\"expected_generation\":1,\"execution_termination\":{\"expected_generation\":1,\"reason\":\"\"}}"
	projection, err := json.Marshal(map[string]any{"consistent": true, "required_action": controllerExecutionAction, "required_action_parameters": map[string]string{"controller_command_json": command, "semantic_slot": executionTerminationReasonSlot}})
	if err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$2\" = controller-execution-plan ]; then printf '%%s\\n' '%s'; else cat; fi\n", projection)
	if err := execfile.Write(filepath.Join(bin, "glm-worker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	staged, selected, err := executionStagedTemplate(cfg)
	if err != nil || !selected || len(staged.Slots) != 1 || staged.Slots[0].Name != executionTerminationReasonSlot {
		t.Fatalf("typed termination not staged: selected=%v err=%v slots=%#v", selected, err, staged.Slots)
	}
	prepared, err := parentaction.PrepareStaged(cfg.RepoRoot, controllerExecutionAction, staged)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := execute(cfg, []string{controllerExecutionAction, prepared.Token}, &output, &bytes.Buffer{}); err == nil {
		t.Fatal("unfilled reason unexpectedly executed")
	}
	original, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatal(err)
	}
	reason := "明示された終了理由"
	valid := bytes.Replace(original, []byte(parentaction.ExecutionTerminationReasonPlaceholder), []byte(reason), 1)
	tampered := bytes.Replace(valid, []byte("\"expected_generation\":1"), []byte("\"expected_generation\":2"), 1)
	if err := os.WriteFile(prepared.Path, tampered, 0600); err != nil {
		t.Fatal(err)
	}
	if err := execute(cfg, []string{controllerExecutionAction, prepared.Token}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("modified machine command was executed")
	}
	if _, err := parentaction.Peek(cfg.RepoRoot, controllerExecutionAction, prepared.Token); err != nil {
		t.Fatalf("rejected stage consumed: %v", err)
	}
	if err := os.WriteFile(prepared.Path, valid, 0600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := execute(cfg, []string{controllerExecutionAction, prepared.Token}, &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), reason) {
		t.Fatalf("typed reason not delivered over pipe: %s", output.String())
	}
	if err := execute(cfg, []string{controllerExecutionAction, prepared.Token}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("consumed execution token replayed")
	}
}

func TestExecutionRawJSONTransportIsNotAdmitted(t *testing.T) {
	cfg := newControllerActionTestConfig(t, true)
	prepared, err := parentaction.Prepare(cfg.RepoRoot, controllerExecutionAction)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte("__GLM_PARENT_ACTION_PAYLOAD__"), []byte("{\"action\":\"materialize\",\"expected_generation\":1}"), 1)
	if err := os.WriteFile(prepared.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeControllerStaging(cfg, controllerExecutionAction, "", []byte("{\"action\":\"materialize\",\"expected_generation\":1}"), nil); err == nil {
		t.Fatal("free JSON execution command was admitted")
	}
	if _, err := parentaction.Peek(cfg.RepoRoot, controllerExecutionAction, prepared.Token); err != nil {
		t.Fatalf("refused command consumed stage: %v", err)
	}
}

func TestSelectedExecutionTokenCannotBecomeOrdinaryToken(t *testing.T) {
	cfg := newControllerActionTestConfig(t, true)
	bin := t.TempDir()
	const selected = `{"action":"gc-suspension","expected_generation":1,"suspension_id":"selected"}`
	const ordinary = `{"action":"materialize","expected_generation":1,"suspension_id":"ordinary"}`
	projection, err := json.Marshal(map[string]any{
		"consistent": true, "required_action": controllerExecutionAction,
		"required_action_parameters": map[string]string{"controller_command_json": ordinary},
	})
	if err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$2\" = controller-execution-plan ]; then printf '%%s\\n' '%s'; else cat; fi\n", projection)
	if err := execfile.Write(filepath.Join(bin, "glm-worker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	stage, err := parentaction.PrepareStaged(cfg.RepoRoot, controllerExecutionAction, parentaction.StagedTemplate{
		Payload: []byte(publicationCommandTemplatePrefix + selected + "\n"), BindTemplate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(stage.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stage.Path, bytes.Replace(raw, []byte(selected), []byte(ordinary), 1), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = execute(cfg, []string{controllerExecutionAction, stage.Token}, &out, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "token and explicit operation selection") || out.Len() != 0 {
		t.Fatalf("selected token was interpreted as ordinary execution: %v, %s", err, out.Bytes())
	}
	if _, err := os.Stat(stage.Path); err != nil {
		t.Fatalf("refused execution consumed the stage: %v", err)
	}
}
