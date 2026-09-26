from pathlib import Path
import re

ROOT = Path('.')


def read(path):
    return (ROOT / path).read_text()


def write(path, text):
    (ROOT / path).write_text(text)


def replace_once(path, old, new):
    text = read(path)
    if text.count(old) != 1:
        raise SystemExit(f'{path}: expected one match, found {text.count(old)}')
    write(path, text.replace(old, new, 1))


def regex_once(path, pattern, replacement):
    text = read(path)
    updated, count = re.subn(pattern, replacement, text, count=1, flags=re.S)
    if count != 1:
        raise SystemExit(f'{path}: expected one regex match, found {count}')
    write(path, updated)


# A: one changed-path authority, no semantic fallback API.
write('glm-worker/internal/workflow/internal_review_targets.go', '''package workflow

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

// canonicalReviewTargets preserves producer-owned semantic targets. It does not
// invent fallback targets; path-like unlocated values are only normalized to
// the canonical whole-file diff locator.
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
\ttargets := make([]string, 0, len(paths))
\tseen := make(map[string]struct{}, len(paths))
\tfor _, raw := range paths {
\t\tpath := strings.TrimSpace(raw)
\t\tif path == "" {
\t\t\tcontinue
\t\t}
\t\ttarget := fmt.Sprintf("%s:%s", path, reviewtarget.WholeFileDiffLocator)
\t\tif _, _, err := reviewtarget.Parse(target); err != nil {
\t\t\treturn nil, fmt.Errorf("current task review targets: changed path %q cannot be represented as a review target: %w", path, err)
\t\t}
\t\tif _, duplicate := seen[target]; duplicate {
\t\t\tcontinue
\t\t}
\t\tseen[target] = struct{}{}
\t\ttargets = append(targets, target)
\t}
\tif len(targets) == 0 {
\t\treturn nil, fmt.Errorf("current task review targets: current task has no changed paths")
\t}
\treturn targets, nil
}
''')

# Remove test-only production seam.
replace_once(
    'glm-worker/internal/workflow/workflow.go',
    '\tcollectChangedPaths      func(repoRoot, baselineHead string) ([]string, error)\n\tcollectReviewTargetPaths func(repoRoot, baselineHead string) ([]string, error)\n',
    '\tcollectChangedPaths      func(repoRoot, baselineHead string) ([]string, error)\n',
)

# Restore the already-complete risk-floor implementation verbatim in behavior.
replace_once(
    'glm-worker/internal/workflow/workflow.go',
    '\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"\n\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"\n',
    '\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"\n\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"\n\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"\n',
)
replace_once(
    'glm-worker/internal/workflow/workflow.go',
    '''func (w *Workflow) riskFloorReviewTargets() ([]string, error) {
\treturn w.currentReviewDiffTargets()
}
''',
    '''func (w *Workflow) riskFloorReviewTargets() ([]string, error) {
\tpaths, err := w.collectChangedPaths(w.config.RepoRoot, w.state.ReadOr("baseline-head", ""))
\tif err != nil {
\t\treturn nil, fmt.Errorf("risk floor review targets: %w", err)
\t}
\tif len(paths) == 0 {
\t\treturn nil, fmt.Errorf("risk floor review targets: current task has no changed paths")
\t}
\tpaths = append([]string(nil), paths...)
\tsort.Strings(paths)
\ttargets := make([]string, 0, len(paths))
\tfor _, path := range paths {
\t\ttargets = append(targets, fmt.Sprintf("%s:%s", path, reviewtarget.WholeFileDiffLocator))
\t}
\treturn targets, nil
}
''',
)

# Non-convergence keeps meaningful producer targets; only absent semantic targets
# fall back to the actual current task diff, never an implementation file.
regex_once(
    'glm-worker/internal/workflow/workflow.go',
    r'func \(w \*Workflow\) nonConvergedResult\(reviewResult packet\.Result\) \(packet\.Result, error\) \{.*?\n\}\n\nfunc buildNonConvergedResult',
    '''func (w *Workflow) nonConvergedResult(reviewResult packet.Result) (packet.Result, error) {
\ttargets := canonicalReviewTargets(reviewResult.Targets)
\tif len(targets) == 0 {
\t\tvar err error
\t\ttargets, err = w.currentReviewDiffTargets()
\t\tif err != nil {
\t\t\treturn packet.Result{}, fmt.Errorf("non-convergence review targets: %w", err)
\t\t}
\t}
\treturn buildNonConvergedResult(reviewResult, targets), nil
}

func buildNonConvergedResult''',
)

# Parent-validation follows the same producer-local rule.
regex_once(
    'glm-worker/internal/workflow/parent_validation.go',
    r'func \(w \*Workflow\) parentValidationNonConvergedResult\(failure packet\.Result\) \(packet\.Result, error\) \{.*?\n\}\n\nfunc buildParentValidationNonConvergedResult',
    '''func (w *Workflow) parentValidationNonConvergedResult(failure packet.Result) (packet.Result, error) {
\ttargets := canonicalReviewTargets(failure.Targets)
\tif len(targets) == 0 {
\t\tvar err error
\t\ttargets, err = w.currentReviewDiffTargets()
\t\tif err != nil {
\t\t\treturn packet.Result{}, fmt.Errorf("parent validation non-convergence review targets: %w", err)
\t\t}
\t}
\treturn buildParentValidationNonConvergedResult(failure, targets), nil
}

func buildParentValidationNonConvergedResult''',
)

# Quality-surface keeps its semantic narrowing and only removes the hardcoded fallback.
replace_once(
    'glm-worker/internal/workflow/quality_gate.go',
    '\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"\n',
    '\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"\n\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"\n',
)
replace_once(
    'glm-worker/internal/workflow/quality_gate.go',
    '''func (w *Workflow) failClosedQualitySurface(phase, reason string, cause error) error {
\ttargets, err := w.currentReviewDiffTargets()
\tif err != nil {
\t\treturn fmt.Errorf("quality-surface review targets: %w", err)
\t}
''',
    '''func (w *Workflow) failClosedQualitySurface(phase, reason string, cause error) error {
\ttargets, err := w.currentQualitySurfaceReviewTargets()
\tif err != nil {
\t\treturn fmt.Errorf("quality-surface review targets: %w", err)
\t}
''',
)
needle = 'func qualitySurfaceFailClosedResult(phase, reason string, targets []string) packet.Result {'
quality_helper = '''func (w *Workflow) currentQualitySurfaceReviewTargets() ([]string, error) {
\tif w.collectChangedPaths == nil {
\t\treturn nil, fmt.Errorf("quality surface changed-path collector is unavailable")
\t}
\tpaths, err := w.collectChangedPaths(w.config.RepoRoot, w.state.ReadOr("baseline-head", ""))
\tif err != nil {
\t\treturn nil, fmt.Errorf("collect quality surface changed paths: %w", err)
\t}
\ttargets := make([]string, 0, len(paths))
\tseen := make(map[string]struct{}, len(paths))
\tfor _, raw := range paths {
\t\tpath := strings.TrimSpace(raw)
\t\tif path == "" || !IsQualitySurface(path) {
\t\t\tcontinue
\t\t}
\t\ttarget := fmt.Sprintf("%s:%s", path, reviewtarget.WholeFileDiffLocator)
\t\tif _, _, err := reviewtarget.Parse(target); err != nil {
\t\t\treturn nil, fmt.Errorf("quality surface changed path %q cannot be represented as a review target: %w", path, err)
\t\t}
\t\tif _, duplicate := seen[target]; duplicate {
\t\t\tcontinue
\t\t}
\t\tseen[target] = struct{}{}
\t\ttargets = append(targets, target)
\t}
\tif len(targets) == 0 {
\t\treturn nil, fmt.Errorf("quality surface has no changed paths")
\t}
\treturn targets, nil
}

'''
replace_once('glm-worker/internal/workflow/quality_gate.go', needle, quality_helper + needle)
replace_once(
    'glm-worker/internal/workflow/quality_surface_approval.go',
    '\ttargets, err := w.currentReviewDiffTargets()\n',
    '\ttargets, err := w.currentQualitySurfaceReviewTargets()\n',
)

# Tests: use the existing changed-path authority directly.
write('glm-worker/internal/workflow/internal_review_targets_test.go', '''package workflow

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
\tw.collectChangedPaths = func(string, string) ([]string, error) {
\t\treturn []string{"z.go", "commentlint", "a.go", "z.go"}, nil
\t}
\ttargets, err := w.currentReviewDiffTargets()
\tif err != nil {
\t\tt.Fatal(err)
\t}
\twant := []string{"a.go:@diff", "commentlint:@diff", "z.go:@diff"}
\tif !reflect.DeepEqual(targets, want) {
\t\tt.Fatalf("targets = %#v want %#v", targets, want)
\t}
}

func TestCurrentReviewDiffTargetsRejectsUnavailableOrEmpty(t *testing.T) {
\tfor _, tc := range []struct {
\t\tname    string
\t\tcollect func(string, string) ([]string, error)
\t\twant    string
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

write('glm-worker/internal/workflow/internal_review_producer_failclosed_test.go', '''package workflow

import (
\t"bytes"
\t"errors"
\t"reflect"
\t"testing"

\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestInternalReviewProducersDoNotEmitWithoutSemanticTargets(t *testing.T) {
\tfailure := packet.Result{Status: packet.StatusFixRequired, Risk: packet.RiskHigh, Summary: "failure", RequirementCoverage: "covered", Invariants: "preserved", TestEvidence: "evidence", Issues: "issue", ResidualRisk: "risk", Targets: []string{"none"}}
\tproducers := []struct {
\t\tname string
\t\trun  func(*Workflow) error
\t}{
\t\t{"quality-surface", func(w *Workflow) error { return w.failClosedQualitySurface("worker-new", "changed", nil) }},
\t\t{"snapshot", func(w *Workflow) error {
\t\t\treturn w.failClosedSnapshot(state.SnapshotStageReviewStart, state.GitSnapshot{}, state.GitSnapshot{}, "mismatch", nil)
\t\t}},
\t\t{"report-only-snapshot", func(w *Workflow) error {
\t\t\treturn w.failClosedReportOnlySnapshot(state.SnapshotStageReportOnlyEnd, state.GitSnapshot{}, state.GitSnapshot{}, "mismatch", nil)
\t\t}},
\t\t{"non-convergence", func(w *Workflow) error { _, err := w.nonConvergedResult(failure); return err }},
\t\t{"parent-validation", func(w *Workflow) error { _, err := w.parentValidationNonConvergedResult(failure); return err }},
\t}
\tmodes := []struct {
\t\tname    string
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
\t\t\t\tif err := producer.run(w); err == nil {
\t\t\t\t\tt.Fatal("missing semantic target must return an error")
\t\t\t\t}
\t\t\t\tif out.Len() != 0 {
\t\t\t\t\tt.Fatalf("producer emitted a packet without semantic targets: %s", out.String())
\t\t\t\t}
\t\t\t\tif st.TaskStatus() != before {
\t\t\t\t\tt.Fatalf("producer committed state without semantic targets: before=%s after=%s", before, st.TaskStatus())
\t\t\t\t}
\t\t\t})
\t\t}
\t}
}

func TestNonConvergenceProducersPreserveMeaningfulTargets(t *testing.T) {
\tfailure := packet.Result{Status: packet.StatusFixRequired, Targets: []string{"actual.go:12-20"}}
\tw := newWorkflowT(t, newStateStoreT(t), &scriptedRunner{})
\tw.collectChangedPaths = func(string, string) ([]string, error) { return nil, errors.New("must not be called") }
\n\tresult, err := w.nonConvergedResult(failure)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif !reflect.DeepEqual(result.Targets, failure.Targets) {
\t\tt.Fatalf("non-convergence targets = %#v want %#v", result.Targets, failure.Targets)
\t}
\tresult, err = w.parentValidationNonConvergedResult(failure)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif !reflect.DeepEqual(result.Targets, failure.Targets) {
\t\tt.Fatalf("parent-validation targets = %#v want %#v", result.Targets, failure.Targets)
\t}
}

func TestQualitySurfaceReviewTargetsRemainNarrowed(t *testing.T) {
\tw := newWorkflowT(t, newStateStoreT(t), &scriptedRunner{})
\tw.collectChangedPaths = func(string, string) ([]string, error) {
\t\treturn []string{"ordinary.go", "commentlint"}, nil
\t}
\ttargets, err := w.currentQualitySurfaceReviewTargets()
\tif err != nil {
\t\tt.Fatal(err)
\t}
\twant := []string{"commentlint:@diff"}
\tif !reflect.DeepEqual(targets, want) {
\t\tt.Fatalf("quality-surface targets = %#v want %#v", targets, want)
\t}
}
''')

# Restore producer-semantic expectation for parent validation.
replace_once(
    'glm-worker/internal/workflow/parent_validation_test.go',
    'func TestParentValidationBudgetExhaustionUsesCurrentTaskTargets(t *testing.T) {',
    'func TestParentValidationBudgetExhaustionKeepsFailureTargets(t *testing.T) {',
)
replace_once(
    'glm-worker/internal/workflow/parent_validation_test.go',
    '''\tif !strings.Contains(emitted, `"targets":["tracked.go:@diff"]`) {
\t\tt.Fatalf("terminal packet must use the actual current-task review target: %s", emitted)
\t}
''',
    '''\tif !strings.Contains(emitted, `"targets":["a.go:@diff"]`) {
\t\tt.Fatalf("terminal packet must keep the harnesslint failure targets: %s", emitted)
\t}
''',
)

for path in [
    'glm-worker/internal/workflow/risk_floor_parent_evidence_test.go',
    'glm-worker/internal/workflow/risk_floor_test.go',
]:
    text = read(path)
    write(path, text.replace('collectReviewTargetPaths', 'collectChangedPaths'))

# Remove the test-only seam from generic fixtures; targeted tests override collectChangedPaths directly.
replace_once(
    'glm-worker/internal/workflow/workflow_test.go',
    '''\tw.collectReviewTargetPaths = func(string, string) ([]string, error) {
\t\treturn []string{"tracked.go"}, nil
\t}
''',
    '',
)
replace_once(
    'glm-worker/internal/workflow/workflow_snapshot_test.go',
    'w.collectReviewTargetPaths = func(string, string) ([]string, error) {',
    'w.collectChangedPaths = func(string, string) ([]string, error) {',
)
replace_once(
    'glm-worker/internal/workflow/quality_surface_parent_approval_test.go',
    '\tw.collectReviewTargetPaths = func(string, string) ([]string, error) { return []string{"commentlint"}, nil }\n',
    '',
)

# B: simple bounded signal retention, not a generic ranking framework.
regex_once(
    'glm-worker/internal/shadoweval/input.go',
    r'func selectEventEvidence\(records \[\]state\.TaskEventRecord\) \[\]state\.TaskEventRecord \{.*?\n\}\n\nfunc projectEventEvidence',
    '''func selectEventEvidence(records []state.TaskEventRecord) []state.TaskEventRecord {
\tif len(records) <= eventEvidenceMaxItems {
\t\treturn append([]state.TaskEventRecord(nil), records...)
\t}
\tselectedIndexes := make([]int, 0, eventEvidenceMaxItems)
\tselected := make([]bool, len(records))
\tfor index := len(records) - 1; index >= 0 && len(selectedIndexes) < eventEvidenceMaxItems; index-- {
\t\tif !eventEvidenceHasSignal(records[index]) {
\t\t\tcontinue
\t\t}
\t\tselected[index] = true
\t\tselectedIndexes = append(selectedIndexes, index)
\t}
\tfor index := len(records) - 1; index >= 0 && len(selectedIndexes) < eventEvidenceMaxItems; index-- {
\t\tif selected[index] {
\t\t\tcontinue
\t\t}
\t\tselected[index] = true
\t\tselectedIndexes = append(selectedIndexes, index)
\t}
\tsort.Ints(selectedIndexes)
\tresult := make([]state.TaskEventRecord, 0, len(selectedIndexes))
\tfor _, index := range selectedIndexes {
\t\tresult = append(result, records[index])
\t}
\treturn result
}

func eventEvidenceHasSignal(record state.TaskEventRecord) bool {
\tif record.IsError || record.Validation != nil || len(record.SearchPaths) > 0 {
\t\treturn true
\t}
\tfor _, block := range record.Blocks {
\t\tif block.IsError || block.OperationCategory != "" || len(block.Validation) > 0 {
\t\t\treturn true
\t\t}
\t}
\treturn false
}

func projectEventEvidence''',
)

regex_once(
    'glm-worker/internal/shadoweval/fixed_range_review_test.go',
    r'func TestBuildInputRetainsLateHighSignalEvidenceWithinBound\(t \*testing\.T\) \{.*?\n\}\n\nfunc TestReductionEstimateUsesCanonicalSourceEvidenceBytes',
    '''func TestBuildInputRetainsLateHighSignalEvidenceWithinBound(t *testing.T) {
\tlogs := []state.ModelCallLog{{
\t\tVersion:   state.ModelCallLogVersion,
\t\tCallType:  state.CallTypeTask,
\t\tCallID:    "call-a",
\t\tTaskID:    "task-1",
\t\tRole:      state.ReviewerRole,
\t\tPhase:     "reviewer-1",
\t\tOutcome:   "success",
\t\tStartedAt: time.Date(2026, 9, 24, 1, 0, 0, 0, time.UTC),
\t}}
\trecords := make([]state.TaskEventRecord, 0, 43)
\tfor seq := 1; seq <= 40; seq++ {
\t\trecords = append(records, state.TaskEventRecord{
\t\t\tVersion: 1, TaskID: "task-1", CallID: "call-a", Role: "reviewer", Phase: "reviewer-1",
\t\t\tSeq: seq, Kind: "assistant", Subtype: "message",
\t\t})
\t}
\trecords = append(records,
\t\tstate.TaskEventRecord{
\t\t\tVersion: 1, TaskID: "task-1", CallID: "call-a", Role: "reviewer", Phase: "reviewer-1",
\t\t\tSeq: 41, Kind: "assistant", Subtype: "tool-result",
\t\t\tValidation: &state.TaskValidationEvent{
\t\t\t\tAttribution: "reviewer", Source: "tool", Form: "go-test", Suite: "./...", Scope: "repository",
\t\t\t\tResult: state.ValidationResultFail, Evidence: "late validation failure",
\t\t\t},
\t\t},
\t\tstate.TaskEventRecord{
\t\t\tVersion: 1, TaskID: "task-1", CallID: "call-a", Role: "reviewer", Phase: "reviewer-1",
\t\t\tSeq: 42, Kind: "assistant", Subtype: "tool-result", IsError: true,
\t\t},
\t\tstate.TaskEventRecord{
\t\t\tVersion: 1, TaskID: "task-1", CallID: "call-a", Role: "reviewer", Phase: "reviewer-1",
\t\t\tSeq: 43, Kind: "assistant", Subtype: "tool-result", SearchPaths: []string{"late.go"},
\t\t\tBlocks: []state.TaskBlockSummary{{Type: "tool_result", Name: "go test", OperationCategory: state.OperationCategoryTest}},
\t\t},
\t)

\tfirst, err := BuildInput("task-1", logs, records)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tsecond, err := BuildInput("task-1", logs, records)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif first.ItemsSHA256 != second.ItemsSHA256 {
\t\tt.Fatalf("bounded selection is not deterministic: %s != %s", first.ItemsSHA256, second.ItemsSHA256)
\t}
\tif len(first.Items) != 1 || len(first.Items[0].Events) > eventEvidenceMaxItems {
\t\tt.Fatalf("bounded event count = %#v", first.Items)
\t}
\tvar validationSeen, errorSeen, searchOperationSeen bool
\tpreviousSeq := 0
\tfor _, event := range first.Items[0].Events {
\t\tif event.Seq <= previousSeq {
\t\t\tt.Fatalf("selected events lost chronological order: previous=%d current=%d", previousSeq, event.Seq)
\t\t}
\t\tpreviousSeq = event.Seq
\t\tif event.Seq == 41 && event.Validation != nil && event.Validation.Result == state.ValidationResultFail {
\t\t\tvalidationSeen = true
\t\t}
\t\tif event.Seq == 42 && event.IsError {
\t\t\terrorSeen = true
\t\t}
\t\tif event.Seq == 43 && len(event.SearchPaths) == 1 && len(event.Blocks) == 1 && event.Blocks[0].OperationCategory == state.OperationCategoryTest {
\t\t\tsearchOperationSeen = true
\t\t}
\t}
\tif !validationSeen || !errorSeen || !searchOperationSeen {
\t\tt.Fatalf("late signal evidence was dropped: validation=%v error=%v search_operation=%v events=%#v", validationSeen, errorSeen, searchOperationSeen, first.Items[0].Events)
\t}
}

func TestReductionEstimateUsesCanonicalSourceEvidenceBytes''',
)
