package workflow

import (
	"io"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestGenericRuleContextCarriesConflictBoundary(t *testing.T) {
	root, baseline := newRuleActivationRepo(t)
	writeGitTestFile(t, root, "internal/app/options.go", "package app\n")
	cfg, st := newRuleActivationWorkflowConfig(t, root, baseline)
	writeRuleFile(t, cfg.CodexConfigDir, "go.md", "GO CONTRACT")
	writeRuleFile(t, cfg.CodexConfigDir, "cli.md", "CLI CONTRACT")

	workflow := NewWorkflow(cfg, st, nil, io.Discard)
	checkpoint := state.ResumeCheckpoint{Prompt: "primary", OriginalPrompt: "primary"}
	got, _, err := workflow.activateCheckpointRules(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	boundary := renderInstructionConflictBoundary(defaultInstructionConflictBoundary())
	if !strings.Contains(got.Prompt, boundary) {
		t.Fatalf("generic rule context lacks wrapper conflict boundary: %s", got.Prompt)
	}
	if strings.Index(got.Prompt, boundary) > strings.Index(got.Prompt, "CLI CONTRACT") {
		t.Fatalf("conflict boundary must precede generic rule prose: %s", got.Prompt)
	}
}

func TestRuleActivationCorrectionPreservesPinnedPrimaryAuthority(t *testing.T) {
	root, baseline := newRuleActivationRepo(t)
	cfg, st := newRuleActivationWorkflowConfig(t, root, baseline)
	writeRuleFile(t, cfg.CodexConfigDir, "cli.md", "CLI CONTRACT")
	if err := st.Write(activeTaskStateKey, "IMPLEMENTATION_TASKS/task.md"); err != nil {
		t.Fatal(err)
	}

	workflow := NewWorkflow(cfg, st, nil, io.Discard)
	parent := state.ResumeCheckpoint{
		Phase:              "worker",
		Prompt:             "old prompt",
		OriginalPrompt:     "old prompt",
		Request:            "add only --long-option",
		Decision:           "none",
		ActivatedRuleFiles: nil,
	}
	got, err := workflow.ruleActivationCorrectionCheckpoint(parent, []workerRule{ruleCLI}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Prompt, "ACTIVE_TASK_FILE: IMPLEMENTATION_TASKS/task.md") {
		t.Fatalf("rule correction lost pinned primary authority: %s", got.Prompt)
	}
	if !strings.Contains(got.Prompt, "PUBLIC_SURFACE_EXPANSION: requires-primary-authority") {
		t.Fatalf("rule correction lacks public-surface conflict boundary: %s", got.Prompt)
	}
	if strings.Index(got.Prompt, "ACTIVE_TASK_FILE:") > strings.Index(got.Prompt, "--- cli.md ---") {
		t.Fatalf("primary authority must precede generic rule text: %s", got.Prompt)
	}
}
