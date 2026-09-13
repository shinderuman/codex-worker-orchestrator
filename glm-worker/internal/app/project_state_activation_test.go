package app

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestProjectStateIngressIgnoresMarkerlessForeignPlan(t *testing.T) {
	cfg := newAppConfig(t)
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, state.ParentPlanFile), []byte("not this repository plan protocol\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.RepoRoot, "IMPLEMENTATION_TASKS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, "IMPLEMENTATION_TASKS", "foreign.md"), []byte("foreign\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := executeReadOnlyInspection(Command{Mode: ModeProjectState}, cfg, &stdout); err != nil {
		t.Fatal(err)
	}
	var output projectStateOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.PlanPresent || output.Goal != nil || output.Schedule != nil || output.NextRunnable != nil || output.Completion != nil {
		t.Fatalf("inactive project-state = %#v", output)
	}
	if output.Version != projectStateVersion || len(output.Dependencies) != 0 || len(output.Blockers) != 0 || output.Continuation.Reason != projectContinuationReasonPlanAbsent {
		t.Fatalf("inactive project-state contract = %#v", output)
	}
}

func TestProjectStateIngressPreservesActivatedRepositoryProjection(t *testing.T) {
	cfg := newAppConfig(t)
	marker := filepath.Join(cfg.RepoRoot, repositoryharness.MarkerPath)
	if err := os.WriteFile(marker, []byte(repositoryharness.MarkerContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", cfg.RepoRoot, "add", "--", repositoryharness.MarkerPath).CombinedOutput(); err != nil {
		t.Fatalf("git add marker: %v: %s", err, output)
	}
	writeProjectStateRepoFile(t, cfg.RepoRoot, state.ParentPlanFile, "# Plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/current.md`\n\n## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n")
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/current.md", projectStateTaskBody("none"))

	var stdout bytes.Buffer
	if err := executeReadOnlyInspection(Command{Mode: ModeProjectState}, cfg, &stdout); err != nil {
		t.Fatal(err)
	}
	var output projectStateOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if !output.PlanPresent || output.Schedule == nil || len(output.Schedule.Active) != 1 || output.Schedule.Active[0] != "IMPLEMENTATION_TASKS/current.md" {
		t.Fatalf("activated project-state = %#v", output)
	}
}

func TestProjectStateIngressFailsClosedWhenTaskPinLacksActivationPin(t *testing.T) {
	cfg := newAppConfig(t)
	st := state.AttachStateStore(cfg)
	if err := st.Write("active-task", "IMPLEMENTATION_TASKS/current.md"); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := executeReadOnlyInspection(Command{Mode: ModeProjectState}, cfg, &stdout)
	if err == nil || !strings.Contains(err.Error(), "repository harness activation pinが欠落しています") {
		t.Fatalf("project-state activation error = %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("failed project-state wrote output: %q", stdout.String())
	}
}
