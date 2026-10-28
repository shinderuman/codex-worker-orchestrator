package parentactioncmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

func TestPublicationRequiredValidationsSemanticSlotMerge(t *testing.T) {
	t.Parallel()
	command := map[string]json.RawMessage{"action": json.RawMessage(`"complete"`)}
	declaration := `{"forms":[{"form":"go-test","working_dir":"glm-worker"},{"form":"go-test-race","working_dir":"glm-worker"}],"probes":[{"probe_id":"docker-worker-path","verifies":"worker Bash path uses Docker","argv":["/bin/sh","-c","exit 0"]}]}`
	if err := applyPublicationSemanticValue(command, controller.CompletionSlotRequiredValidations, declaration); err != nil {
		t.Fatal(err)
	}
	var merged controller.RequiredValidationDeclaration
	if err := json.Unmarshal(command["required_validations"], &merged); err != nil || len(merged.Forms) != 2 || len(merged.Probes) != 1 {
		t.Fatalf("declared set was not merged verbatim: %s %v", command["required_validations"], err)
	}
	if err := applyPublicationSemanticValue(command, controller.CompletionSlotRequiredValidations, parentaction.PublicationRequiredValidationsPlaceholder); err == nil || !strings.Contains(err.Error(), "was not supplied") {
		t.Fatalf("unsubstituted declaration slot accepted: %v", err)
	}
	if err := applyPublicationSemanticValue(command, controller.CompletionSlotRequiredValidations, "go-test"); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("non-JSON declaration accepted: %v", err)
	}
}
