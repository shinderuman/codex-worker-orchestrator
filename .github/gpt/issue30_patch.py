from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    if text.count(old) != 1:
        raise SystemExit(f"{path}: anchor count {text.count(old)} != 1")
    p.write_text(text.replace(old, new, 1))


replace_once(
    "glm-worker/internal/packet/schema.go",
    '''\t\t"sol_question":         stringProperty(),
\t\t"targets":              stringsProperty(),
''',
    '''\t\t"sol_question":         stringProperty(),
\t\t"decision":             stringProperty(),
\t\t"evidence":             stringProperty(),
\t\t"options":              stringProperty(),
\t\t"recommendation":       stringProperty(),
\t\t"test_obligations":     stringProperty(),
\t\t"targets":              stringsProperty(),
''',
)
replace_once(
    "glm-worker/internal/packet/schema.go",
    '''\t\t\tstringProperty(string(StatusPass), string(StatusFixRequired), string(StatusNeedsSolReview)),
''',
    '''\t\t\tstringProperty(string(StatusPass), string(StatusFixRequired), string(StatusNeedsSolReview), string(StatusNeedsSolDecision)),
''',
)

replace_once(
    "glm-worker/internal/packet/validate.go",
    '''\tcase StatusNeedsSolReview:
\t\tif result.Risk != RiskHigh {
\t\t\treturn &constraintError{reason: "NEEDS_SOL_REVIEWのriskはHIGHにしてください"}
\t\t}
\tdefault:
''',
    '''\tcase StatusNeedsSolReview:
\t\tif result.Risk != RiskHigh {
\t\t\treturn &constraintError{reason: "NEEDS_SOL_REVIEWのriskはHIGHにしてください"}
\t\t}
\tcase StatusNeedsSolDecision:
\t\tif result.Risk != RiskHigh {
\t\t\treturn &constraintError{reason: "NEEDS_SOL_DECISIONのriskはHIGHにしてください"}
\t\t}
\tdefault:
''',
)

replace_once(
    "glm-worker/internal/workflow/rule_activation.go",
    '''func (w *Workflow) withCurrentRuleContext(prompt string) (string, error) {
\trequired, err := w.currentRequiredWorkerRules()
\tif err != nil {
\t\treturn "", err
\t}
\treturn w.appendRuleContext(prompt, required)
}
''',
    '''func (w *Workflow) withCurrentRuleContext(prompt string) (string, error) {
\trequired, err := w.currentRequiredWorkerRules()
\tif err != nil {
\t\treturn "", err
\t}
\tprompt, err = w.appendRuleContext(prompt, required)
\tif err != nil {
\t\treturn "", err
\t}
\tboundary, err := w.reviewerDecisionBoundaryContext(w.readActiveTaskState())
\tif err != nil {
\t\treturn "", err
\t}
\tif boundary == "" {
\t\treturn prompt, nil
\t}
\treturn strings.TrimRight(prompt, "\\n") + boundary, nil
}
''',
)

replace_once(
    "glm-worker/internal/workflow/workflow.go",
    '''func (*Workflow) parseModelCallResult(checkpoint state.ResumeCheckpoint, runResult runner.RunResult) (packet.Result, error) {
\tresult, err := packet.ParseStructured(runResult.StructuredOutput)
\tif err != nil {
\t\treturn packet.Result{}, err
\t}
\tif checkpoint.Role == state.ReviewerRole {
\t\terr = packet.ValidateReviewerResult(result)
\t} else {
''',
    '''func (w *Workflow) parseModelCallResult(checkpoint state.ResumeCheckpoint, runResult runner.RunResult) (packet.Result, error) {
\tresult, err := packet.ParseStructured(runResult.StructuredOutput)
\tif err != nil {
\t\treturn packet.Result{}, err
\t}
\tif checkpoint.Role == state.ReviewerRole {
\t\terr = packet.ValidateReviewerResult(result)
\t\tif err == nil && result.Status == packet.StatusNeedsSolDecision {
\t\t\terr = w.validateReviewerDecisionBoundary()
\t\t}
\t} else {
''',
)
replace_once(
    "glm-worker/internal/workflow/workflow.go",
    '''\tswitch reviewResult.Status {
\tcase packet.StatusPass:
''',
    '''\tswitch reviewResult.Status {
\tcase packet.StatusNeedsSolDecision:
\t\treturn w.finishReviewerDecision(reviewResult)
\tcase packet.StatusPass:
''',
)

p = Path("glm-worker/internal/workflow/decision_boundary_review.go")
text = p.read_text()
text = text.replace(
    '''import (
\t"fmt"
\t"strings"
)
''',
    '''import (
\t"fmt"
\t"strings"

\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)
''',
    1,
)
text += '''
func (w *Workflow) validateReviewerDecisionBoundary() error {
\tactiveTaskPath := w.readActiveTaskState()
\tif activeTaskPath == "" {
\t\treturn fmt.Errorf("reviewer returned NEEDS_SOL_DECISION without an active task decision boundary")
\t}
\tauthority, err := loadSemanticDecisionAuthority(w.config.RepoRoot, activeTaskPath)
\tif err != nil {
\t\treturn err
\t}
\tif len(authority.unresolved()) == 0 {
\t\treturn fmt.Errorf("reviewer returned NEEDS_SOL_DECISION but all Sol decision axes are fixed")
\t}
\treturn nil
}

func (w *Workflow) finishReviewerDecision(result packet.Result) error {
\tif err := w.state.Touch("pending-decision"); err != nil {
\t\treturn err
\t}
\treturn w.finishReview(state.TaskStatusWaitingDecision, result)
}
'''
p.write_text(text)

Path("glm-worker/internal/packet/reviewer_decision_test.go").write_text(r'''package packet

import (
    "strings"
    "testing"
)

func TestReviewerSchemaAllowsNeedsSolDecision(t *testing.T) {
    schema, err := ReviewerSchemaJSON()
    if err != nil {
        t.Fatal(err)
    }
    for _, value := range []string{
        string(StatusNeedsSolDecision),
        `"decision"`, `"evidence"`, `"options"`, `"recommendation"`, `"test_obligations"`,
    } {
        if !strings.Contains(schema, value) {
            t.Fatalf("reviewer schema missing %s: %s", value, schema)
        }
    }

    riskFloor, err := RiskFloorReviewerSchemaJSON()
    if err != nil {
        t.Fatal(err)
    }
    if strings.Contains(riskFloor, string(StatusNeedsSolDecision)) {
        t.Fatalf("risk-floor schema must stay NEEDS_SOL_REVIEW-only: %s", riskFloor)
    }
}

func TestValidateReviewerNeedsSolDecision(t *testing.T) {
    result := Result{
        Status:          StatusNeedsSolDecision,
        Risk:            RiskHigh,
        Decision:        "public-surface semantics are unresolved",
        Evidence:        "actual diff adds externally visible fields",
        Options:         "keep internal-only or expose fields",
        Recommendation:  "ask Sol before accepting the semantic choice",
        TestObligations: "verify selected public contract",
        Targets:         []string{"internal/timeline/timeline.go"},
    }
    if err := ValidateReviewerResult(result); err != nil {
        t.Fatalf("valid reviewer decision rejected: %v", err)
    }
    result.Risk = RiskLow
    if err := ValidateReviewerResult(result); err == nil {
        t.Fatal("LOW reviewer NEEDS_SOL_DECISION must be rejected")
    }
}
''')

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
    r := &scriptedRunner{steps: []runnerStep{{structured: needsSolDecisionPacket()}}}
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
    if err := w.reviewUntilStable("add operation telemetry", workerResult, 1, 0, "worker-new"); err != nil {
        t.Fatal(err)
    }
    if len(r.prompts) != 1 {
        t.Fatalf("decision boundary added model calls: phases=%v", r.phases)
    }
    prompt := r.prompts[0]
    if !strings.Contains(prompt, solDecisionBoundaryReviewMarker) ||
        !strings.Contains(prompt, "UNRESOLVED_AXES: public-surface") ||
        !strings.Contains(prompt, "internal/timeline/timeline.go") {
        t.Fatalf("reviewer did not receive actual-diff boundary context: %s", prompt)
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
