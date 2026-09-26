from pathlib import Path
import re

ROOT = Path('.')


def patch(path, old, new):
    p = ROOT / path
    text = p.read_text()
    if text.count(old) != 1:
        raise SystemExit(f'{path}: expected one match, found {text.count(old)}')
    p.write_text(text.replace(old, new, 1))


def inject_test_collector(path, test_name, target):
    p = ROOT / path
    text = p.read_text()
    start = text.find(f'func {test_name}(')
    if start < 0:
        raise SystemExit(f'{path}: missing {test_name}')
    end = text.find('\nfunc ', start + 1)
    if end < 0:
        end = len(text)
    segment = text[start:end]
    match = re.search(r'^(\s*w := newWorkflowT(?:WithOutput)?\([^\n]+\)\n)', segment, flags=re.M)
    if not match:
        raise SystemExit(f'{path}: no workflow construction in {test_name}')
    insertion = match.group(1) + f'\tw.collectChangedPaths = func(string, string) ([]string, error) {{ return []string{{"{target}"}}, nil }}\n'
    segment = segment[:match.start()] + insertion + segment[match.end():]
    p.write_text(text[:start] + segment + text[end:])


# Mutation-oriented suites use the real test repository diff rather than a synthetic global default.
patch(
    'glm-worker/internal/workflow/review_end_snapshot_test.go',
    '''func newMutationWorkflowShell(t *testing.T, st *state.StateStore) *Workflow {
\tt.Helper()
\tpinRepositoryHarnessActiveT(t, st)
\treturn newWorkflowT(t, st, &scriptedRunner{})
}
''',
    '''func newMutationWorkflowShell(t *testing.T, st *state.StateStore) *Workflow {
\tt.Helper()
\tpinRepositoryHarnessActiveT(t, st)
\tw := newWorkflowT(t, st, &scriptedRunner{})
\tw.collectChangedPaths = func(repoRoot, _ string) ([]string, error) {
\t\treturn collectTaskChangedPaths(repoRoot, st)
\t}
\treturn w
}
''',
)

# Review-resume fixtures use synthetic snapshots; give only those fixtures an explicit semantic task target.
patch(
    'glm-worker/internal/workflow/review_resume_parent_test.go',
    '''\tw := newWorkflowT(t, st, r)
\tw.output = out
''',
    '''\tw := newWorkflowT(t, st, r)
\tw.collectChangedPaths = func(string, string) ([]string, error) { return []string{"tracked.go"}, nil }
\tw.output = out
''',
)

# Synthetic snapshot/diagnostic tests explicitly provide the task target they are not otherwise exercising.
for name in [
    'TestDiagnosticRecordsSnapshotMismatch',
    'TestDiagnosticSnapshotCaptureFailureNotCountedAsMismatch',
    'TestDiagnosticSnapshotSaveFailureNotMismatch',
]:
    inject_test_collector('glm-worker/internal/workflow/diagnostic_test.go', name, 'tracked.go')

# These tests intentionally mutate fixture.go after their synthetic task baseline.
for name in [
    'TestQualityFixRejectsUnprovenFixedReport',
    'TestQualityViolationWithoutFixRejectsExternalChangeBeforeAutoFix',
    'TestQualityPassWithoutFixDoesNotRebaseExternalChange',
]:
    inject_test_collector('glm-worker/internal/workflow/quality_fixer_snapshot_test.go', name, 'fixture.go')

# Quality-surface tests keep the original semantic narrowing.
for name in [
    'TestQualitySurfaceChangeStopsBeforeReviewer',
    'TestMissingQualitySurfaceBaselineFailsClosedWithoutReconstruction',
]:
    inject_test_collector('glm-worker/internal/workflow/quality_gate_test.go', name, 'commentlint')

# Non-convergence fixtures that do not model a repository diff provide it explicitly per test.
for name in [
    'TestAutoFixRoundsDoNotRecordParentOutcome',
    'TestExplicitFixRecordsOutcomeOnceDespiteReexecution',
]:
    inject_test_collector('glm-worker/internal/workflow/parent_review_test.go', name, 'tracked.go')

inject_test_collector('glm-worker/internal/workflow/task_lifecycle_test.go', 'TestAutoFixNonConvergence', 'tracked.go')

# Parent-validation budget tests share one local fixture; keep the override local to that fixture.
inject_test_collector('glm-worker/internal/workflow/parent_validation_test.go', 'exhaustParentValidationFixBudget', 'tracked.go')
