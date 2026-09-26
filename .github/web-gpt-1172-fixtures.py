from pathlib import Path
import re

ROOT = Path('.')


def patch(path, old, new):
    p = ROOT / path
    text = p.read_text()
    if text.count(old) != 1:
        raise SystemExit(f'{path}: expected one match, found {text.count(old)}')
    p.write_text(text.replace(old, new, 1))


def function_segment(path, function_name):
    p = ROOT / path
    text = p.read_text()
    start = text.find(f'func {function_name}(')
    if start < 0:
        raise SystemExit(f'{path}: missing {function_name}')
    end = text.find('\nfunc ', start + 1)
    if end < 0:
        end = len(text)
    return p, text, start, end, text[start:end]


def inject_test_collector(path, test_name, target):
    p, text, start, end, segment = function_segment(path, test_name)
    match = re.search(r'^(\s*w := newWorkflowT(?:WithOutput)?\([^\n]+\)\n)', segment, flags=re.M)
    if not match:
        raise SystemExit(f'{path}: no workflow construction in {test_name}')
    insertion = match.group(1) + f'\tw.collectChangedPaths = func(string, string) ([]string, error) {{ return []string{{"{target}"}}, nil }}\n'
    segment = segment[:match.start()] + insertion + segment[match.end():]
    p.write_text(text[:start] + segment + text[end:])


def matching_call_end(segment, open_index):
    depth = 0
    quote = None
    escape = False
    for i in range(open_index, len(segment)):
        ch = segment[i]
        if quote is not None:
            if quote != '`' and escape:
                escape = False
                continue
            if quote != '`' and ch == '\\':
                escape = True
                continue
            if ch == quote:
                quote = None
            continue
        if ch in ('"', "'", '`'):
            quote = ch
            continue
        if ch == '(':
            depth += 1
        elif ch == ')':
            depth -= 1
            if depth == 0:
                return i
    raise SystemExit('unterminated constructor call')


def inject_after_constructor(path, test_name, constructors, body):
    p, text, start, end, segment = function_segment(path, test_name)
    found = []
    for constructor in constructors:
        pos = segment.find(constructor + '(')
        if pos >= 0:
            found.append((pos, constructor))
    if not found:
        raise SystemExit(f'{path}: no expected constructor in {test_name}')
    pos, constructor = min(found)
    open_index = segment.find('(', pos)
    call_end = matching_call_end(segment, open_index)
    line_end = segment.find('\n', call_end)
    if line_end < 0:
        line_end = len(segment)
    line_start = segment.rfind('\n', 0, pos) + 1
    indent = segment[line_start:pos]
    indent = indent[:len(indent) - len(indent.lstrip())]
    insertion = '\n' + indent + body
    segment = segment[:line_end] + insertion + segment[line_end:]
    p.write_text(text[:start] + segment + text[end:])


# Review-resume fixtures are synthetic snapshot fixtures. Keep the changed-path
# override in this test helper only; production still has one authority.
patch(
    'glm-worker/internal/workflow/review_resume_parent_test.go',
    '''\tw := newWorkflowT(t, st, r)\n\tw.output = out\n''',
    '''\tw := newWorkflowT(t, st, r)\n\tw.collectChangedPaths = func(string, string) ([]string, error) { return []string{"tracked.go"}, nil }\n\tw.output = out\n''',
)

# Synthetic snapshot/diagnostic tests explicitly provide the semantic target
# that their synthetic snapshots represent.
for name in [
    'TestDiagnosticRecordsSnapshotMismatch',
    'TestDiagnosticSnapshotCaptureFailureNotCountedAsMismatch',
    'TestDiagnosticSnapshotSaveFailureNotMismatch',
]:
    inject_test_collector('glm-worker/internal/workflow/diagnostic_test.go', name, 'tracked.go')

for name in [
    'TestQualityFixRejectsUnprovenFixedReport',
    'TestQualityViolationWithoutFixRejectsExternalChangeBeforeAutoFix',
    'TestQualityPassWithoutFixDoesNotRebaseExternalChange',
]:
    inject_test_collector('glm-worker/internal/workflow/quality_fixer_snapshot_test.go', name, 'fixture.go')

for name in [
    'TestQualitySurfaceChangeStopsBeforeReviewer',
    'TestMissingQualitySurfaceBaselineFailsClosedWithoutReconstruction',
]:
    inject_test_collector('glm-worker/internal/workflow/quality_gate_test.go', name, 'commentlint')

for name in [
    'TestAutoFixRoundsDoNotRecordParentOutcome',
    'TestExplicitFixRecordsOutcomeOnceDespiteReexecution',
]:
    inject_test_collector('glm-worker/internal/workflow/parent_review_test.go', name, 'tracked.go')

inject_test_collector('glm-worker/internal/workflow/task_lifecycle_test.go', 'TestAutoFixNonConvergence', 'tracked.go')
inject_test_collector('glm-worker/internal/workflow/parent_validation_test.go', 'exhaustParentValidationFixBudget', 'tracked.go')

# Tests that synthesize snapshot failures on mutation workflows set the existing
# collector on that test instance only. Do not modify newMutationWorkflowShell.
actual_diff_body = 'w.collectChangedPaths = func(repoRoot, _ string) ([]string, error) { return collectTaskChangedPaths(repoRoot, w.state) }'
fixed_target_body = 'w.collectChangedPaths = func(string, string) ([]string, error) { return []string{"tracked.txt"}, nil }'

for name in [
    'TestReviewEndWorktreeMutationRejectsPass',
    'TestReviewEndUntrackedMutationRejectsPass',
    'TestReviewEndIndexMutationRejectsPass',
    'TestReviewEndHeadMutationRejectsPass',
    'TestReviewEndMutationRejectsFixRequired',
    'TestReviewEndMutationRejectsNeedsSolReview',
    'TestReviewEndMutationAfterRateLimitResumeRejectsPass',
    'TestReviewEndMutationOnRiskFloorReemitRejects',
]:
    inject_after_constructor(
        'glm-worker/internal/workflow/review_end_snapshot_test.go',
        name,
        ['newMutationWorkflow', 'newMutationWorkflowShell'],
        actual_diff_body,
    )

for name in [
    'TestReportOnlyWorktreeMutationFailsClosedBeforeReview',
    'TestReportOnlyIndexMutationFailsClosedBeforeReview',
    'TestReportOnlyHeadMutationFailsClosedBeforeReview',
]:
    inject_after_constructor(
        'glm-worker/internal/workflow/report_only_snapshot_test.go',
        name,
        ['newReportOnlyWorkflow'],
        actual_diff_body,
    )

for name in [
    'TestReportOnlyStartSnapshotCaptureFailureStopsBeforeWorkerRun',
    'TestReportOnlyStartSnapshotSaveFailureStopsBeforeWorkerRun',
    'TestReportOnlyComparisonSaveFailureFailsClosed',
    'TestReportOnlyEndSnapshotCaptureFailureFailsClosedNotMismatch',
    'TestReportOnlyRateLimitResumeVerifiesAgainstSameStartSnapshot',
    'TestReportOnlyTransientRecoveryStillEnforcesInvariant',
    'TestReportOnlyProviderUnavailableResumeVerifiesAgainstStartSnapshot',
    'TestReportOnlyResumeWithoutStartSnapshotFailsClosedBeforeCalls',
]:
    inject_after_constructor(
        'glm-worker/internal/workflow/report_only_snapshot_test.go',
        name,
        ['newReportOnlyWorkflow', 'newMutationWorkflowShell'],
        fixed_target_body,
    )

for name in [
    'TestPlanFileReviewerMutationUsesExistingSnapshotInvariant',
    'TestHistoryFileReviewerMutationUsesExistingSnapshotInvariant',
]:
    inject_after_constructor(
        'glm-worker/internal/workflow/plan_file_guard_test.go',
        name,
        ['newMutationWorkflow', 'newMutationWorkflowShell'],
        actual_diff_body,
    )
