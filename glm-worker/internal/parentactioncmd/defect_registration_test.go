package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestDefectRegistrationRequiresTaskAndPlanBinding(t *testing.T) {
	repo := t.TempDir()
	cfg := config.AppConfig{RepoRoot: repo, StateBase: t.TempDir(), RepoHash: "defect-registration"}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	source := "IMPLEMENTATION_TASKS/active.md"
	target := "IMPLEMENTATION_TASKS/follow-up.md"
	if err := st.Write("active-task", source); err != nil {
		t.Fatal(err)
	}
	writeDefectRegistrationFile(t, repo, source, defectRegistrationTask("active"))
	writeDefectRegistrationFile(t, repo, "IMPLEMENTATION_PLAN.local.md", defectRegistrationPlan(source, ""))

	var recorded bytes.Buffer
	if err := executeDefectRegistrationAction(cfg, []string{actionRecordDefectFinding, "--task", target}, &recorded); err != nil {
		t.Fatal(err)
	}
	var recordOutput defectRegistrationOutput
	if err := json.Unmarshal(recorded.Bytes(), &recordOutput); err != nil {
		t.Fatal(err)
	}
	if recordOutput.Status != "recorded" || recordOutput.Registration == nil || recordOutput.Registration.TaskPath != target || recordOutput.RequiredAction != state.ParentActionBindDefectTask {
		t.Fatalf("record output = %#v", recordOutput)
	}
	if err := executeDefectRegistrationAction(cfg, []string{actionBindDefectTask, "--task", target}, &bytes.Buffer{}); err == nil {
		t.Fatal("bind succeeded without task file and Plan entry")
	}
	if plan, err := st.ParentActionPlan(); err != nil || plan.RequiredAction != state.ParentActionBindDefectTask {
		t.Fatalf("failed bind released gate: plan=%#v err=%v", plan, err)
	}

	writeDefectRegistrationFile(t, repo, target, "# follow-up\n")
	writeDefectRegistrationFile(t, repo, "IMPLEMENTATION_PLAN.local.md", defectRegistrationPlan(source, target))
	if err := executeDefectRegistrationAction(cfg, []string{actionBindDefectTask, "--task", target}, &bytes.Buffer{}); err == nil {
		t.Fatal("bind succeeded with malformed task contract")
	}
	if plan, err := st.ParentActionPlan(); err != nil || plan.RequiredAction != state.ParentActionBindDefectTask {
		t.Fatalf("malformed task released gate: plan=%#v err=%v", plan, err)
	}

	writeDefectRegistrationFile(t, repo, target, defectRegistrationTask("follow-up"))
	var bound bytes.Buffer
	if err := executeDefectRegistrationAction(cfg, []string{actionBindDefectTask, "--task", target}, &bound); err != nil {
		t.Fatal(err)
	}
	var bindOutput defectRegistrationOutput
	if err := json.Unmarshal(bound.Bytes(), &bindOutput); err != nil {
		t.Fatal(err)
	}
	if bindOutput.Status != "bound" || bindOutput.Registration == nil || bindOutput.Registration.TaskPath != target || bindOutput.RequiredAction != state.ParentActionNone {
		t.Fatalf("bind output = %#v", bindOutput)
	}
	if registrations, err := st.CurrentPendingDefectRegistrations(); err != nil || len(registrations) != 0 {
		t.Fatalf("pending after bind = %#v err=%v", registrations, err)
	}

	var duplicate bytes.Buffer
	if err := executeDefectRegistrationAction(cfg, []string{actionRecordDefectFinding, "--task", target}, &duplicate); err != nil {
		t.Fatal(err)
	}
	var duplicateOutput defectRegistrationOutput
	if err := json.Unmarshal(duplicate.Bytes(), &duplicateOutput); err != nil {
		t.Fatal(err)
	}
	if duplicateOutput.Status != "already-registered" || duplicateOutput.Registration != nil {
		t.Fatalf("duplicate output = %#v", duplicateOutput)
	}
}

func TestDefectRegistrationRejectsCurrentTask(t *testing.T) {
	repo := t.TempDir()
	cfg := config.AppConfig{RepoRoot: repo, StateBase: t.TempDir(), RepoHash: "current-task-defect"}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	source := "IMPLEMENTATION_TASKS/active.md"
	if err := st.Write("active-task", source); err != nil {
		t.Fatal(err)
	}
	if err := executeDefectRegistrationAction(cfg, []string{actionRecordDefectFinding, "--task", source}, &bytes.Buffer{}); err == nil {
		t.Fatal("current-task finding was accepted as an independent defect task")
	}
	if registrations, err := st.CurrentPendingDefectRegistrations(); err != nil || len(registrations) != 0 {
		t.Fatalf("current-task rejection left pending state = %#v err=%v", registrations, err)
	}
}

func defectRegistrationPlan(active, next string) string {
	plan := "## ACTIVE\n\n- `" + active + "`\n\n## NEXT（優先順）\n\n"
	if next != "" {
		plan += "- `" + next + "`\n"
	}
	return plan + "\n## BLOCKED / USER_PERMISSION_WAIT\n"
}

func defectRegistrationTask(name string) string {
	return "# Task: " + name + "\n\n" +
		"## Original instruction\n\nfixture\n\n" +
		"## Amendments\n\nnone\n\n" +
		"## Purpose\n\nfixture\n\n" +
		"## External feasibility\n\nstatus: not-applicable\n\n" +
		"## Contract\n\nfixture\n\n" +
		"## Must not\n\nfixture\n\n" +
		"## Acceptance criteria\n\nfixture\n\n" +
		"## Historical invariants\n\nfixture\n\n" +
		"## Dependencies\n\nnone\n"
}

func writeDefectRegistrationFile(t *testing.T, repo, path, content string) {
	t.Helper()
	full := filepath.Join(repo, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
