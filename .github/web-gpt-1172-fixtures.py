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
    line = segment[line_start:line_end]
    match = re.search(r'\b([A-Za-z_][A-Za-z0-9_]*)\s*:?=', line)
    if not match:
        raise SystemExit(f'{path}: cannot determine workflow variable in {test_name}: {line!r}')
    workflow_var = match.group(1)
    indent = line[:len(line) - len(line.lstrip())]
    rendered = body.replace('{workflow}', workflow_var)
    insertion = '\n' + indent + rendered
    segment = segment[:line_end] + insertion + segment[line_end:]
    p.write_text(text[:start] + segment + text[end:])


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

inject_test_collector('glm-worker/internal/workflow/parent_validation_test.go', 'exhaustParentValidationFixBudget', 'tracked.go')

actual_diff_body = '{workflow}.collectChangedPaths = func(repoRoot, _ string) ([]string, error) { return collectTaskChangedPaths(repoRoot, {workflow}.state) }'
fixed_target_body = '{workflow}.collectChangedPaths = func(string, string) ([]string, error) { return []string{"tracked.txt"}, nil }'

for name in [
    'TestReviewEndWorktreeMutationRejectsPass',
    'TestReviewEndUntrackedMutationRejectsPass',
    'TestReviewEndIndexMutationRejectsPass',
    'TestReviewEndHeadMutationRejectsPass',
    'TestReviewEndMutationRejectsFixRequired',
    'TestReviewEndMutationRejectsNeedsSolReview',
]:
    inject_after_constructor('glm-worker/internal/workflow/review_end_snapshot_test.go', name, ['newMutationWorkflow'], actual_diff_body)

inject_after_constructor('glm-worker/internal/workflow/review_end_snapshot_test.go', 'TestReviewEndMutationOnRiskFloorReemitRejects', ['newMutationWorkflowShell'], actual_diff_body)
inject_after_constructor('glm-worker/internal/workflow/review_end_snapshot_test.go', 'TestReviewEndMutationAfterRateLimitResumeRejectsPass', ['newMutationWorkflowShell'], fixed_target_body)

for name in [
    'TestReportOnlyWorktreeMutationFailsClosedBeforeReview',
    'TestReportOnlyIndexMutationFailsClosedBeforeReview',
    'TestReportOnlyHeadMutationFailsClosedBeforeReview',
]:
    inject_after_constructor('glm-worker/internal/workflow/report_only_snapshot_test.go', name, ['newReportOnlyWorkflow'], actual_diff_body)

for name in [
    'TestReportOnlyStartSnapshotCaptureFailureStopsBeforeWorkerRun',
    'TestReportOnlyStartSnapshotSaveFailureStopsBeforeWorkerRun',
    'TestReportOnlyComparisonSaveFailureFailsClosed',
    'TestReportOnlyEndSnapshotCaptureFailureFailsClosedNotMismatch',
]:
    inject_after_constructor('glm-worker/internal/workflow/report_only_snapshot_test.go', name, ['newReportOnlyWorkflow', 'newMutationWorkflowShell'], fixed_target_body)

inject_after_constructor('glm-worker/internal/workflow/report_only_snapshot_test.go', 'TestReportOnlyTransientRecoveryStillEnforcesInvariant', ['newReportOnlyWorkflow'], actual_diff_body)

for name in [
    'TestReportOnlyRateLimitResumeVerifiesAgainstSameStartSnapshot',
    'TestReportOnlyProviderUnavailableResumeVerifiesAgainstStartSnapshot',
    'TestReportOnlyResumeWithoutStartSnapshotFailsClosedBeforeCalls',
]:
    inject_after_constructor('glm-worker/internal/workflow/report_only_snapshot_test.go', name, ['newMutationWorkflowShell'], fixed_target_body)

for name in [
    'TestPlanFileReviewerMutationUsesExistingSnapshotInvariant',
    'TestHistoryFileReviewerMutationUsesExistingSnapshotInvariant',
]:
    inject_after_constructor('glm-worker/internal/workflow/plan_file_guard_test.go', name, ['newPlanFileWorkflow'], actual_diff_body)

# Review-resume table helper only gets a target for cases whose expected outcome
# is fail-closed. Accepted resume cases remain untouched so risk-floor behavior is
# not changed by test setup.
patch(
    'glm-worker/internal/workflow/review_resume_parent_test.go',
    'func runReviewResumeDeltaCase(t *testing.T, tt reviewResumeDeltaCase) reviewResumeDeltaRun {',
    'func runReviewResumeDeltaCase(t *testing.T, tt reviewResumeDeltaCase, failClosed bool) reviewResumeDeltaRun {',
)
patch(
    'glm-worker/internal/workflow/review_resume_parent_test.go',
    '\tw := newReviewResumeWorkflow(t, st, r, out)\n\trepoRoot := w.config.RepoRoot\n',
    '\tw := newReviewResumeWorkflow(t, st, r, out)\n\tif failClosed {\n\t\tw.collectChangedPaths = func(string, string) ([]string, error) { return []string{"tracked.txt"}, nil }\n\t}\n\trepoRoot := w.config.RepoRoot\n',
)
patch(
    'glm-worker/internal/workflow/review_resume_parent_test.go',
    'assertReviewResumeAccepted(t, runReviewResumeDeltaCase(t, tt))',
    'assertReviewResumeAccepted(t, runReviewResumeDeltaCase(t, tt, false))',
)
patch(
    'glm-worker/internal/workflow/review_resume_parent_test.go',
    'run := runReviewResumeDeltaCase(t, tt)\n\t\t\tassertReviewResumeStopped',
    'run := runReviewResumeDeltaCase(t, tt, true)\n\t\t\tassertReviewResumeStopped',
)

# Other synthetic resume fail-closed tests set the existing collector on only
# the workflow instance that is expected to synthesize NEEDS_SOL_REVIEW.
inject_after_constructor('glm-worker/internal/workflow/repository_boundary_activation_test.go', 'TestReviewResumeInactiveHarnessRejectsCoincidentalParentPathChange', ['newReviewResumeWorkflow'], fixed_target_body)
inject_after_constructor('glm-worker/internal/workflow/review_resume_parent_test.go', 'TestReviewResumeLegacyStateFailsClosed', ['newReviewResumeWorkflow'], fixed_target_body)
inject_after_constructor('glm-worker/internal/workflow/review_resume_parent_test.go', 'TestReviewResumeParentUpdateThenReviewerMutationFailsClosed', ['newReviewResumeWorkflow'], fixed_target_body)

patch(
    'glm-worker/internal/workflow/review_resume_parent_test.go',
    '\tw2 := newReviewResumeWorkflow(t, st, r2, &out2)\n',
    '\tw2 := newReviewResumeWorkflow(t, st, r2, &out2)\n\tw2.collectChangedPaths = func(string, string) ([]string, error) { return []string{"tracked.txt"}, nil }\n',
)
patch(
    'glm-worker/internal/workflow/review_resume_parent_canonical_test.go',
    '\tw3 := newReviewResumeWorkflow(t, st, r3, &out3)\n',
    '\tw3 := newReviewResumeWorkflow(t, st, r3, &out3)\n\tw3.collectChangedPaths = func(string, string) ([]string, error) { return []string{"tracked.txt"}, nil }\n',
)
