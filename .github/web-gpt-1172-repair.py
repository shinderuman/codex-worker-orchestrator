from pathlib import Path
import re


def read(path):
    return Path(path).read_text()


def write(path, text):
    Path(path).write_text(text)


def func_span(text, signature):
    start = text.index(signature)
    nxt = text.find("\nfunc ", start + len(signature))
    end = len(text) if nxt < 0 else nxt + 1
    return start, end


def replace_func(path, signature, new):
    text = read(path)
    start, end = func_span(text, signature)
    write(path, text[:start] + new.rstrip() + "\n\n" + text[end:])


def remove_func(path, signature):
    text = read(path)
    start, end = func_span(text, signature)
    write(path, text[:start] + text[end:])


write("glm-worker/internal/workflow/internal_review_targets.go", '''package workflow

import (
\t"fmt"
\t"sort"
\t"strings"

\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"
)

const (
\tinternalReviewNoTarget     = "none"
\tinternalReviewPacketTarget = "PACKET"
)

func canonicalReviewTargets(values []string) []string {
\ttargets := make([]string, 0, len(values))
\tseen := make(map[string]struct{}, len(values))
\tfor _, raw := range values {
\t\tvalue := strings.TrimSpace(raw)
\t\tif value == "" || value == internalReviewNoTarget || value == internalReviewPacketTarget {
\t\t\tcontinue
\t\t}
\t\ttarget := value
\t\tif _, _, err := reviewtarget.Parse(target); err != nil {
\t\t\tif !strings.Contains(value, "/") && !strings.Contains(value, ".") {
\t\t\t\tcontinue
\t\t\t}
\t\t\ttarget = fmt.Sprintf("%s:%s", value, reviewtarget.WholeFileDiffLocator)
\t\t\tif _, _, parseErr := reviewtarget.Parse(target); parseErr != nil {
\t\t\t\tcontinue
\t\t\t}
\t\t}
\t\tif _, duplicate := seen[target]; duplicate {
\t\t\tcontinue
\t\t}
\t\tseen[target] = struct{}{}
\t\ttargets = append(targets, target)
\t}
\treturn targets
}

func (w *Workflow) currentReviewDiffTargets() ([]string, error) {
\tif w.collectChangedPaths == nil {
\t\treturn nil, fmt.Errorf("current task review targets: changed-path collector is unavailable")
\t}
\tpaths, err := w.collectChangedPaths(w.config.RepoRoot, w.state.ReadOr("baseline-head", ""))
\tif err != nil {
\t\treturn nil, fmt.Errorf("current task review targets: %w", err)
\t}
\tpaths = append([]string(nil), paths...)
\tsort.Strings(paths)
\ttargets := canonicalReviewTargets(paths)
\tif len(targets) == 0 {
\t\treturn nil, fmt.Errorf("current task review targets: current task has no changed paths")
\t}
\treturn targets, nil
}
''')

workflow = "glm-worker/internal/workflow/workflow.go"
replace_func(workflow, "func (w *Workflow) riskFloorReviewTargets()", '''func (w *Workflow) riskFloorReviewTargets() ([]string, error) {
\treturn w.currentReviewDiffTargets()
}''')
replace_func(workflow, "func (w *Workflow) failClosedSnapshot(", '''func (w *Workflow) failClosedSnapshot(stage state.SnapshotStage, workerEnd, reviewStart state.GitSnapshot, reason string, cause error) error {
\tw.recordSnapshotEvent(state.ReviewerRole, stage, workerEnd, reviewStart, reason, cause)
\ttargets, err := w.currentReviewDiffTargets()
\tif err != nil {
\t\treturn fmt.Errorf("snapshot fail-closed review targets: %w", err)
\t}
\treturn w.failClosedStopped(stage, reason, cause, func(stage state.SnapshotStage, reason string) packet.Result {
\t\treturn snapshotFailClosedResult(stage, reason, targets)
\t})
}''')
replace_func(workflow, "func (w *Workflow) failClosedReportOnlySnapshot(", '''func (w *Workflow) failClosedReportOnlySnapshot(stage state.SnapshotStage, start, current state.GitSnapshot, reason string, cause error) error {
\tw.recordSnapshotEvent(state.WorkerRole, stage, start, current, reason, cause)
\ttargets, err := w.currentReviewDiffTargets()
\tif err != nil {
\t\treturn fmt.Errorf("report-only snapshot fail-closed review targets: %w", err)
\t}
\treturn w.failClosedStopped(stage, reason, cause, func(stage state.SnapshotStage, reason string) packet.Result {
\t\treturn reportOnlySnapshotFailClosedResult(stage, reason, targets)
\t})
}''')

text = read(workflow)
start, end = func_span(text, "func snapshotFailClosedResult(")
fn = text[start:end]
old_header = "func snapshotFailClosedResult(stage state.SnapshotStage, reason string) packet.Result {"
assert old_header in fn
fn = fn.replace(old_header, "func snapshotFailClosedResult(stage state.SnapshotStage, reason string, targets []string) packet.Result {", 1)
old_target = 'Targets:             []string{"glm-worker/internal/state/snapshot.go:@diff"},'
assert old_target in fn
fn = fn.replace(old_target, "Targets:             targets,", 1)
write(workflow, text[:start] + fn + text[end:])

text = read(workflow)
start, end = func_span(text, "func reportOnlySnapshotFailClosedResult(")
fn = text[start:end]
old_header = "func reportOnlySnapshotFailClosedResult(stage state.SnapshotStage, reason string) packet.Result {"
assert old_header in fn
fn = fn.replace(old_header, "func reportOnlySnapshotFailClosedResult(stage state.SnapshotStage, reason string, targets []string) packet.Result {", 1)
old_target = 'Targets:             []string{"glm-worker/internal/workflow/workflow.go:@diff"},'
assert old_target in fn
fn = fn.replace(old_target, "Targets:             targets,", 1)
write(workflow, text[:start] + fn + text[end:])

text = read(workflow)
start, end = func_span(text, "func nonConvergedResult(")
fn = text[start:end]
old_header = "func nonConvergedResult(reviewResult packet.Result) packet.Result {"
assert old_header in fn
fn = fn.replace(old_header, "func buildNonConvergedResult(reviewResult packet.Result, targets []string) packet.Result {", 1)
fn, count = re.subn(r'\n\ttargets := reviewTargetsOrFallback\(\n\t\treviewResult\.Targets,\n\t\t\[\]string\{"glm-worker/internal/workflow/review_flow\.go:@diff"\},\n\t\)\n', '\n', fn, count=1)
assert count == 1
method = '''func (w *Workflow) nonConvergedResult(reviewResult packet.Result) (packet.Result, error) {
\ttargets, err := w.currentReviewDiffTargets()
\tif err != nil {
\t\treturn packet.Result{}, fmt.Errorf("non-convergence review targets: %w", err)
\t}
\treturn buildNonConvergedResult(reviewResult, targets), nil
}

'''
write(workflow, text[:start] + method + fn + text[end:])

review_flow = "glm-worker/internal/workflow/review_flow.go"
text = read(review_flow)
old = '''\tif autoFixes >= w.config.MaxAutoFixRounds {
\t\treturn w.finishReview(state.TaskStatusWaitingSolReview, nonConvergedResult(reviewResult))
\t}'''
new = '''\tif autoFixes >= w.config.MaxAutoFixRounds {
\t\tresult, err := w.nonConvergedResult(reviewResult)
\t\tif err != nil {
\t\t\treturn err
\t\t}
\t\treturn w.finishReview(state.TaskStatusWaitingSolReview, result)
\t}'''
assert text.count(old) == 1
write(review_flow, text.replace(old, new, 1))

parent_validation = "glm-worker/internal/workflow/parent_validation.go"
text = read(parent_validation)
old = "\tresult := parentValidationNonConvergedResult(failure)"
new = '''\tresult, err := w.parentValidationNonConvergedResult(failure)
\tif err != nil {
\t\treturn err
\t}'''
assert text.count(old) == 1
write(parent_validation, text.replace(old, new, 1))

text = read(parent_validation)
start, end = func_span(text, "func parentValidationNonConvergedResult(")
fn = text[start:end]
old_header = "func parentValidationNonConvergedResult(failure packet.Result) packet.Result {"
assert old_header in fn
fn = fn.replace(old_header, "func buildParentValidationNonConvergedResult(failure packet.Result, targets []string) packet.Result {", 1)
fn, count = re.subn(r'\n\ttargets := reviewTargetsOrFallback\(\n\t\tfailure\.Targets,\n\t\t\[\]string\{"glm-worker/internal/workflow/parent_validation\.go:@diff"\},\n\t\)\n', '\n', fn, count=1)
assert count == 1
method = '''func (w *Workflow) parentValidationNonConvergedResult(failure packet.Result) (packet.Result, error) {
\ttargets, err := w.currentReviewDiffTargets()
\tif err != nil {
\t\treturn packet.Result{}, fmt.Errorf("parent validation non-convergence review targets: %w", err)
\t}
\treturn buildParentValidationNonConvergedResult(failure, targets), nil
}

'''
write(parent_validation, text[:start] + method + fn + text[end:])

quality_gate = "glm-worker/internal/workflow/quality_gate.go"
replace_func(quality_gate, "func (w *Workflow) failClosedQualitySurface(", '''func (w *Workflow) failClosedQualitySurface(phase, reason string, cause error) error {
\ttargets, err := w.currentReviewDiffTargets()
\tif err != nil {
\t\treturn fmt.Errorf("quality-surface review targets: %w", err)
\t}
\tif err := w.state.WaitForQualitySurfaceReview(phase); err != nil {
\t\treturn err
\t}
\tif cause != nil {
\t\treason = fmt.Sprintf("%s: %v", reason, cause)
\t}
\treturn w.emitResult(qualitySurfaceFailClosedResult(phase, reason, targets))
}''')
remove_func(quality_gate, "func (w *Workflow) currentQualitySurfaceReviewTargets(")

quality_approval = "glm-worker/internal/workflow/quality_surface_approval.go"
text = read(quality_approval)
old = "\ttargets := w.currentQualitySurfaceReviewTargets()"
new = '''\ttargets, err := w.currentReviewDiffTargets()
\tif err != nil {
\t\treturn true, fmt.Errorf("quality-surface approval review targets: %w", err)
\t}'''
assert text.count(old) == 1
write(quality_approval, text.replace(old, new, 1))

snapshot_test = "glm-worker/internal/workflow/workflow_snapshot_test.go"
text = read(snapshot_test)
old = '''\tw.captureBoundarySnapshot = func(repoRoot string) (state.GitSnapshot, error) {
\t\tsnapshot, err := w.captureSnapshot(repoRoot)'''
new = '''\tw.collectChangedPaths = func(string, string) ([]string, error) {
\t\treturn []string{"tracked.go"}, nil
\t}
\tw.captureBoundarySnapshot = func(repoRoot string) (state.GitSnapshot, error) {
\t\tsnapshot, err := w.captureSnapshot(repoRoot)'''
assert text.count(old) == 1
write(snapshot_test, text.replace(old, new, 1))

write("glm-worker/internal/workflow/internal_review_targets_test.go", '''package workflow

import (
\t"errors"
\t"reflect"
\t"strings"
\t"testing"

\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"
)

func TestCanonicalInternalReviewTargets(t *testing.T) {
\ttargets := canonicalReviewTargets([]string{"a.go", "b.go:12-20", "inspect-current-review", "none", "PACKET", "a.go"})
\tif len(targets) != 2 || targets[0] != "a.go:@diff" || targets[1] != "b.go:12-20" {
\t\tt.Fatalf("targets = %#v", targets)
\t}
\tfor _, target := range targets {
\t\tif _, _, err := reviewtarget.Parse(target); err != nil {
\t\t\tt.Fatalf("target %q: %v", target, err)
\t\t}
\t}
}

func TestCurrentReviewDiffTargetsUsesActualTaskPaths(t *testing.T) {
\tw := newWorkflowT(t, newStateStoreT(t), &scriptedRunner{})
\tw.collectChangedPaths = func(string, string) ([]string, error) { return []string{"z.go", "a.go", "z.go"}, nil }
\ttargets, err := w.currentReviewDiffTargets()
\tif err != nil { t.Fatal(err) }
\twant := []string{"a.go:@diff", "z.go:@diff"}
\tif !reflect.DeepEqual(targets, want) { t.Fatalf("targets = %#v want %#v", targets, want) }
}

func TestCurrentReviewDiffTargetsRejectsUnavailableOrEmpty(t *testing.T) {
\tfor _, tc := range []struct {
\t\tname string
\t\tcollect func(string, string) ([]string, error)
\t\twant string
\t}{
\t\t{"lookup-failure", func(string, string) ([]string, error) { return nil, errors.New("changed paths unavailable") }, "changed paths unavailable"},
\t\t{"empty", func(string, string) ([]string, error) { return nil, nil }, "no changed paths"},
\t} {
\t\tt.Run(tc.name, func(t *testing.T) {
\t\t\tw := newWorkflowT(t, newStateStoreT(t), &scriptedRunner{})
\t\t\tw.collectChangedPaths = tc.collect
\t\t\tif _, err := w.currentReviewDiffTargets(); err == nil || !strings.Contains(err.Error(), tc.want) {
\t\t\t\tt.Fatalf("error = %v want %q", err, tc.want)
\t\t\t}
\t\t})
\t}
}
''')

write("glm-worker/internal/workflow/internal_review_producer_failclosed_test.go", '''package workflow

import (
\t"bytes"
\t"errors"
\t"testing"

\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestInternalReviewProducersDoNotEmitWithoutActualTaskTargets(t *testing.T) {
\tfailure := packet.Result{Status: packet.StatusFixRequired, Risk: packet.RiskHigh, Summary: "failure", RequirementCoverage: "covered", Invariants: "preserved", TestEvidence: "evidence", Issues: "issue", ResidualRisk: "risk", Targets: []string{"syntactic.go:@diff"}}
\tproducers := []struct {
\t\tname string
\t\trun func(*Workflow) error
\t}{
\t\t{"quality-surface", func(w *Workflow) error { return w.failClosedQualitySurface("worker-new", "changed", nil) }},
\t\t{"snapshot", func(w *Workflow) error { return w.failClosedSnapshot(state.SnapshotStageReviewStart, state.GitSnapshot{}, state.GitSnapshot{}, "mismatch", nil) }},
\t\t{"report-only-snapshot", func(w *Workflow) error { return w.failClosedReportOnlySnapshot(state.SnapshotStageReportOnlyEnd, state.GitSnapshot{}, state.GitSnapshot{}, "mismatch", nil) }},
\t\t{"non-convergence", func(w *Workflow) error { _, err := w.nonConvergedResult(failure); return err }},
\t\t{"parent-validation", func(w *Workflow) error { _, err := w.parentValidationNonConvergedResult(failure); return err }},
\t}
\tmodes := []struct {
\t\tname string
\t\tcollect func(string, string) ([]string, error)
\t}{
\t\t{"lookup-failure", func(string, string) ([]string, error) { return nil, errors.New("changed paths unavailable") }},
\t\t{"empty", func(string, string) ([]string, error) { return nil, nil }},
\t}
\tfor _, producer := range producers {
\t\tfor _, mode := range modes {
\t\t\tt.Run(producer.name+"/"+mode.name, func(t *testing.T) {
\t\t\t\tst := newStateStoreT(t)
\t\t\t\tvar out bytes.Buffer
\t\t\t\tw := newWorkflowTWithOutput(t, st, &scriptedRunner{}, &out)
\t\t\t\tw.collectChangedPaths = mode.collect
\t\t\t\tbefore := st.TaskStatus()
\t\t\t\tif err := producer.run(w); err == nil { t.Fatal("missing actual task targets must return an error") }
\t\t\t\tif out.Len() != 0 { t.Fatalf("producer emitted a packet without actual task targets: %s", out.String()) }
\t\t\t\tif st.TaskStatus() != before { t.Fatalf("producer committed state without actual targets: before=%s after=%s", before, st.TaskStatus()) }
\t\t\t})
\t\t}
\t}
}
''')
