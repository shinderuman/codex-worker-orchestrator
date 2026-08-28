from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    if text.count(old) != 1:
        raise SystemExit(f"{path}: anchor count {text.count(old)} != 1")
    p.write_text(text.replace(old, new, 1))


replace_once(
    "glm-worker/internal/packet/schema_test.go",
    '''\tif strings.Join(values, ",") != "PASS,FIX_REQUIRED,NEEDS_SOL_REVIEW" {
\t\tt.Fatalf("reviewer status enum = %v", values)
\t}
\tfor _, want := range []string{"invariants", "test_evidence", "issues", "residual_risk", "sol_question", "targets", "artifacts"} {
\t\tif _, ok := properties[want]; !ok {
\t\t\tt.Fatalf("reviewer schemaに%sがありません", want)
\t\t}
\t}
\tif _, ok := properties["decision"]; ok {
\t\tt.Fatal("reviewer schemaはworker専用fieldを持ってはいけません")
\t}
''',
    '''\tif strings.Join(values, ",") != "PASS,FIX_REQUIRED,NEEDS_SOL_REVIEW,NEEDS_SOL_DECISION" {
\t\tt.Fatalf("reviewer status enum = %v", values)
\t}
\tfor _, want := range []string{
\t\t"invariants", "test_evidence", "issues", "residual_risk", "sol_question",
\t\t"decision", "evidence", "options", "recommendation", "test_obligations", "targets", "artifacts",
\t} {
\t\tif _, ok := properties[want]; !ok {
\t\t\tt.Fatalf("reviewer schemaに%sがありません", want)
\t\t}
\t}
''',
)

replace_once(
    "glm-worker/internal/workflow/workflow_test.go",
    '''\terr := w.ExecuteNewTask("request")
\tif err == nil || !strings.Contains(err.Error(), "reviewer結果のstatusとして許容されません") {
\t\tt.Fatalf("reviewer role status error = %v", err)
\t}
''',
    '''\terr := w.ExecuteNewTask("request")
\tif err == nil || !strings.Contains(err.Error(), "without an active task decision boundary") {
\t\tt.Fatalf("reviewer role status error = %v", err)
\t}
''',
)

Path("glm-worker/internal/workflow/decision_boundary_review_test.go").write_text(r'''package workflow

import (
    "bytes"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "testing"

    "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
    "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestDecisionBoundaryReviewContextOnlyForUnresolvedAxes(t *testing.T) {
    authority := semanticDecisionAuthority{fixed: map[semanticDecisionAxis]string{
        decisionAxisResponsibility:      "existing telemetry responsibility",
        decisionAxisDependencyDirection: "workflow to telemetry",
        decisionAxisCompatibility:       "preserve compatibility",
        decisionAxisValidationError:     "existing validation semantics",
    }}
    block := decisionBoundaryReviewContextBlock("IMPLEMENTATION_TASKS/Task011.md", authority)
    if !strings.Contains(block, "UNRESOLVED_AXES: public-surface") ||
        !strings.Contains(block, "actual git diff") ||
        !strings.Contains(block, "NEEDS_SOL_DECISION") {
        t.Fatalf("review boundary block = %s", block)
    }

    authority.fixed[decisionAxisPublicSurface] = "no new public surface"
    if got := decisionBoundaryReviewContextBlock("IMPLEMENTATION_TASKS/Task011.md", authority); got != "" {
        t.Fatalf("all-fixed authority must not add review boundary context: %s", got)
    }
}

func TestReviewerDecisionBoundaryRejectsAllFixedAuthority(t *testing.T) {
    st := newStateStoreT(t)
    r := &scriptedRunner{}
    w := newWorkflowT(t, st, r)
    task := writeDecisionBoundaryTask(t, w.config.RepoRoot, true)
    if err := st.Write(activeTaskStateKey, task); err != nil {
        t.Fatal(err)
    }
    if err := w.validateReviewerDecisionBoundary(); err == nil {
        t.Fatal("reviewer decision must be rejected when every axis is fixed")
    }

    task = writeDecisionBoundaryTask(t, w.config.RepoRoot, false)
    if err := st.Write(activeTaskStateKey, task); err != nil {
        t.Fatal(err)
    }
    if err := w.validateReviewerDecisionBoundary(); err != nil {
        t.Fatalf("unresolved public-surface must allow reviewer decision verdict: %v", err)
    }
}

func TestTask011PublicSurfaceChoiceRoutesReviewerDecisionWithoutExtraCall(t *testing.T) {
    repo := t.TempDir()
    gitRun(t, repo, "init", "-q")
    gitRun(t, repo, "config", "user.email", "test@example.com")
    gitRun(t, repo, "config", "user.name", "test")

    task := writeDecisionBoundaryTask(t, repo, false)
    timeline := filepath.Join(repo, "internal", "timeline", "timeline.go")
    if err := os.MkdirAll(filepath.Dir(timeline), 0o755); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(timeline, []byte("package timeline\n\nfunc fields() []string { return []string{\"status\"} }\n"), 0o644); err != nil {
        t.Fatal(err)
    }
    gitRun(t, repo, "add", ".")
    gitRun(t, repo, "commit", "-qm", "baseline")
    baseline := strings.TrimSpace(gitRun(t, repo, "rev-parse", "HEAD"))

    if err := os.WriteFile(timeline, []byte("package timeline\n\nfunc fields() []string { return []string{\"status\", \"operations\", \"operation_totals\"} }\n"), 0o644); err != nil {
        t.Fatal(err)
    }

    st := newStateStoreT(t)
    if err := st.Write("baseline-head", baseline); err != nil {
        t.Fatal(err)
    }
    if err := st.Write(activeTaskStateKey, task); err != nil {
        t.Fatal(err)
    }
    var output bytes.Buffer
    r := &scriptedRunner{}
    w := newWorkflowTWithOutput(t, st, r, &output)
    w.config.RepoRoot = repo
    w.collectChangedPaths = collectChangedPaths
    w.temp = t.TempDir()

    workerResult := packet.Result{
        Status:              packet.StatusImplemented,
        Risk:                packet.RiskLow,
        Summary:             "operation telemetry implemented",
        RequirementCoverage: "covered",
        Tests:               "pass",
        Unverified:          "none",
    }
    checkpoint, _, _, err := w.buildReviewCheckpoint("add operation telemetry", workerResult, 1, 0)
    if err != nil {
        t.Fatal(err)
    }
    if len(r.prompts) != 0 {
        t.Fatalf("building boundary context must not add a model call: phases=%v", r.phases)
    }
    if !strings.Contains(checkpoint.Prompt, solDecisionBoundaryReviewMarker) ||
        !strings.Contains(checkpoint.Prompt, "UNRESOLVED_AXES: public-surface") ||
        !strings.Contains(checkpoint.Prompt, "internal/timeline/timeline.go") {
        t.Fatalf("reviewer did not receive actual-diff boundary context: %s", checkpoint.Prompt)
    }

    reviewResult := packet.Result{
        Status:          packet.StatusNeedsSolDecision,
        Risk:            packet.RiskHigh,
        Decision:        "public-surface semantics are unresolved",
        Evidence:        "timeline diff adds operations and operation_totals",
        Options:         "keep fields internal or expose them",
        Recommendation:  "ask Sol before accepting the public contract",
        TestObligations: "verify the selected public contract",
        Targets:         []string{"internal/timeline/timeline.go"},
    }
    if err := w.validateReviewerDecisionBoundary(); err != nil {
        t.Fatal(err)
    }
    if err := w.handleReviewResult("add operation telemetry", workerResult, reviewResult, 1, 0); err != nil {
        t.Fatal(err)
    }
    if st.TaskStatus() != state.TaskStatusWaitingDecision || !st.Exists("pending-decision") {
        t.Fatalf("decision route status=%s pending=%v", st.TaskStatus(), st.Exists("pending-decision"))
    }
    if !strings.Contains(output.String(), `"status":"NEEDS_SOL_DECISION"`) {
        t.Fatalf("output = %s", output.String())
    }
}

func writeDecisionBoundaryTask(t *testing.T, repo string, fixPublicSurface bool) string {
    t.Helper()
    rel := filepath.ToSlash(filepath.Join("IMPLEMENTATION_TASKS", "Task011.md"))
    path := filepath.Join(repo, filepath.FromSlash(rel))
    if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
        t.Fatal(err)
    }
    lines := []string{
        "# Task011",
        "",
        "## Sol decision authority",
        "- responsibility: existing telemetry responsibility",
        "- dependency-direction: workflow to telemetry",
        "- compatibility: preserve compatibility",
        "- validation-error-semantics: existing validation semantics",
    }
    if fixPublicSurface {
        lines = append(lines, "- public-surface: no new public surface")
    }
    if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
        t.Fatal(err)
    }
    return rel
}

func gitRun(t *testing.T, dir string, args ...string) string {
    t.Helper()
    cmd := exec.Command("git", args...)
    cmd.Dir = dir
    data, err := cmd.CombinedOutput()
    if err != nil {
        t.Fatalf("git %v: %v: %s", args, err, data)
    }
    return string(data)
}
''')
