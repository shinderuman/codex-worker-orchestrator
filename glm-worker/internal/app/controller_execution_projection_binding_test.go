package app

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestExecutionProjectionUsesCanonicalWorkflowState(t *testing.T) {
	cfg, _, _, _, _ := parentOnlyCompletionFixture(t, false)
	cfg.RepoHash = config.RepoHashFor(cfg.RepoRoot)
	cfg.RepoShort = cfg.RepoHash[:12]
	st := startParentHandoffTask(t, cfg)
	if err := st.Write("active-task", "IMPLEMENTATION_TASKS/unrelated.md"); err != nil {
		t.Fatal(err)
	}
	load := func() (config.AppConfig, error) { return cfg, nil }
	var output, diagnostics bytes.Buffer
	if err := runEntry([]string{"--handoff"}, load, nil, nil, &output, &diagnostics); err != nil {
		t.Fatal(err)
	}
	var handoff parentHandoffOutput
	if err := json.Unmarshal(output.Bytes(), &handoff); err != nil {
		t.Fatal(err)
	}
	projectedTransportCommand(t, handoff, "materialize-parent-only", "")
	output.Reset()
	if err := runEntry([]string{"--authority", "controller-execution-plan"}, load, nil, nil, &output, &diagnostics); err != nil {
		t.Fatal(err)
	}
	var projection controllerExecutionProjection
	if err := json.Unmarshal(output.Bytes(), &projection); err != nil {
		t.Fatal(err)
	}
	if !projection.Consistent || projection.RequiredAction == nil || *projection.RequiredAction != *handoff.RequiredAction ||
		projection.Parameters["controller_command_json"] != handoff.RequiredActionParameters["controller_command_json"] {
		t.Fatalf("execution projection differs from canonical handoff: %#v", projection)
	}
}
