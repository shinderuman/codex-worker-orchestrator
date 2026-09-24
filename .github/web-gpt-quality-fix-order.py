from pathlib import Path


def replace_between(path: str, start_marker: str, end_marker: str, replacement: str) -> None:
    p = Path(path)
    text = p.read_text()
    start = text.index(start_marker)
    end = text.index(end_marker, start)
    p.write_text(text[:start] + replacement + text[end:])


review = Path("glm-worker/internal/workflow/review_flow.go")
text = review.read_text()
start = text.index('func (w *Workflow) prepareReviewInputSnapshot(')
end = text.index('\nfunc (w *Workflow) buildReviewCheckpoint(', start)
replacement = '''func (w *Workflow) prepareReviewInputSnapshot(
\trequest string,
\tworkerResult packet.Result,
\treviewNumber int,
\tautoFixes int,
\tworkerPhase string,
) (state.GitSnapshot, bool, error) {
\tworkerEnd, stopped, err := w.captureWorkerEndSnapshot()
\tif err != nil || stopped {
\t\treturn workerEnd, true, err
\t}
\tparentBefore, err := state.CaptureParentFileStates(w.config.RepoRoot)
\tif err != nil {
\t\treturn workerEnd, true, w.failClosedSnapshot(
\t\t\tstate.SnapshotStageWorkerEnd,
\t\t\tworkerEnd,
\t\t\tstate.GitSnapshot{},
\t\t\t"quality gate開始前parent-managed metadata取得失敗",
\t\t\terr,
\t\t)
\t}
\tqualityReport, err := w.qualityGate(w.config.RepoRoot)
\tif err != nil {
\t\treturn workerEnd, true, w.saveQualityGateStop(request, workerResult, reviewNumber, autoFixes, workerPhase, err)
\t}
\treviewInput := workerEnd
\tif qualityReport.Fixed > 0 {
\t\treviewInput, stopped, err = w.acceptQualityFixSnapshot(workerEnd, parentBefore, qualityReport)
\t\tif err != nil || stopped {
\t\t\treturn reviewInput, true, err
\t\t}
\t}
\tif !harnesslint.IsViolation(qualityReport) {
\t\treturn reviewInput, false, nil
\t}
\tresult := qualityGateFixResult(qualityReport)
\tif err := w.writeLastReview(result); err != nil {
\t\treturn reviewInput, true, err
\t}
\treturn reviewInput, true, w.handleReviewResult(request, workerResult, result, reviewNumber, autoFixes)
}
'''
text = text[:start] + replacement + text[end:]
helper_start = text.index('\nfunc (w *Workflow) handleRepositoryQualityViolation(')
helper_end = text.index('\nfunc (w *Workflow) handleReviewResult(', helper_start)
text = text[:helper_start] + text[helper_end:]
review.write_text(text)

replace_between(
    "glm-worker/internal/workflow/review_flow.go",
    'func (w *Workflow) acceptQualityFixSnapshot(',
    '\nfunc (w *Workflow) handleReviewResult(',
    '''func (w *Workflow) acceptQualityFixSnapshot(workerEnd state.GitSnapshot, parentBefore state.ParentFileStates, report harnesslint.Report) (state.GitSnapshot, bool, error) {
\treviewInput, err := w.captureSnapshot(w.config.RepoRoot)
\tif err != nil {
\t\treturn reviewInput, true, w.failClosedSnapshot(
\t\t\tstate.SnapshotStageReviewStart,
\t\t\tworkerEnd,
\t\t\tstate.GitSnapshot{},
\t\t\t"machine quality fixer後snapshot取得失敗",
\t\t\terr,
\t\t)
\t}
\tif reason := qualityFixSnapshotMismatchReason(workerEnd, reviewInput, report); reason != "" {
\t\treturn reviewInput, true, w.failClosedSnapshot(
\t\t\tstate.SnapshotStageReviewStart,
\t\t\tworkerEnd,
\t\t\treviewInput,
\t\t\treason,
\t\t\tnil,
\t\t)
\t}
\tparentAfter, err := state.CaptureParentFileStates(w.config.RepoRoot)
\tif err != nil {
\t\treturn reviewInput, true, w.failClosedSnapshot(
\t\t\tstate.SnapshotStageReviewStart,
\t\t\tworkerEnd,
\t\t\treviewInput,
\t\t\t"machine quality fixer後parent-managed metadata取得失敗",
\t\t\terr,
\t\t)
\t}
\tif !state.SameParentFileStates(parentBefore, parentAfter) {
\t\treturn reviewInput, true, w.failClosedSnapshot(
\t\t\tstate.SnapshotStageReviewStart,
\t\t\tworkerEnd,
\t\t\treviewInput,
\t\t\tfmt.Sprintf("machine quality fixer実行中にparent-managed metadataが変化しました(fixed=%d)", report.Fixed),
\t\t\tnil,
\t\t)
\t}
\tif err := w.state.SaveWorkerEndSnapshot(reviewInput); err != nil {
\t\treturn reviewInput, true, w.failClosedSnapshot(
\t\t\tstate.SnapshotStageReviewStart,
\t\t\tworkerEnd,
\t\t\treviewInput,
\t\t\t"machine quality fixer後worker-end snapshot保存失敗",
\t\t\terr,
\t\t)
\t}
\treturn reviewInput, false, nil
}

func qualityFixSnapshotMismatchReason(workerEnd, reviewInput state.GitSnapshot, report harnesslint.Report) string {
\tevidence := report.FixEvidence
\tswitch {
\tcase evidence == nil || evidence.Method != harnesslint.FixProvenanceIsolatedPostimageV1 || evidence.Input == nil:
\t\treturn fmt.Sprintf("machine quality fixer provenanceがありません(fixed=%d)", report.Fixed)
\tcase evidence.Input.Head != workerEnd.Head || evidence.Input.IndexDigest != workerEnd.IndexDigest || evidence.Input.WorktreeDigest != workerEnd.WorktreeDigest:
\t\treturn fmt.Sprintf("machine quality fixer provenanceがworker-end snapshotと一致しません(fixed=%d)", report.Fixed)
\tcase reviewInput.Head != workerEnd.Head || reviewInput.IndexDigest != workerEnd.IndexDigest:
\t\treturn fmt.Sprintf("machine quality fixer実行中にHEAD/indexが変化しました(fixed=%d)", report.Fixed)
\tdefault:
\t\treturn ""
\t}
}
''',
)

replace_between(
    "glm-worker/internal/harnesslint/fix_isolation.go",
    'func runWithIsolatedFixes(',
    '\nfunc initializeFixWorkspace(',
    '''func runWithIsolatedFixes(root string, execute isolatedFixRun) (Report, error) {
\tinput, err := state.CaptureGitSnapshot(root)
\tif err != nil {
\t\treturn Report{}, err
\t}
\tpaths, err := repositoryPaths(root)
\tif err != nil {
\t\treturn Report{}, err
\t}
\tbefore, err := captureFixManifest(root, paths)
\tif err != nil {
\t\treturn Report{}, err
\t}
\tworkspace, err := os.MkdirTemp("", "harnesslint-fix-*")
\tif err != nil {
\t\treturn Report{}, err
\t}
\tdefer func() { _ = os.RemoveAll(workspace) }()
\treport, after, changed, err := runIsolatedFixWorkspace(workspace, before, execute)
\tif err != nil {
\t\treturn Report{}, err
\t}
\tif len(changed) == 0 {
\t\treturn report, nil
\t}
\tif err := applyIsolatedFixPostimages(root, before, after, changed); err != nil {
\t\treturn Report{}, err
\t}
\treport.FixEvidence = &FixEvidence{
\t\tMethod: FixProvenanceIsolatedPostimageV1,
\t\tInput: &FixInputSnapshot{
\t\t\tHead:           input.Head,
\t\t\tIndexDigest:    input.IndexDigest,
\t\t\tWorktreeDigest: input.WorktreeDigest,
\t\t},
\t}
\treturn report, nil
}

func runIsolatedFixWorkspace(workspace string, before fixManifest, execute isolatedFixRun) (Report, fixManifest, []string, error) {
\tif err := materializeFixManifest(workspace, before); err != nil {
\t\treturn Report{}, nil, nil, err
\t}
\tif err := initializeFixWorkspace(workspace); err != nil {
\t\treturn Report{}, nil, nil, err
\t}
\treport, err := execute(workspace)
\tif err != nil {
\t\treturn Report{}, nil, nil, err
\t}
\tafterPaths, err := repositoryPaths(workspace)
\tif err != nil {
\t\treturn Report{}, nil, nil, err
\t}
\tafter, err := captureFixManifest(workspace, afterPaths)
\tif err != nil {
\t\treturn Report{}, nil, nil, err
\t}
\tchanged, err := fixManifestChangedPaths(before, after)
\tif err != nil {
\t\treturn Report{}, nil, nil, err
\t}
\tif len(changed) != report.Fixed {
\t\treturn Report{}, nil, nil, fmt.Errorf("quality fixer change count mismatch: report=%d postimages=%d", report.Fixed, len(changed))
\t}
\treturn report, after, changed, nil
}

func applyIsolatedFixPostimages(root string, before, after fixManifest, changed []string) error {
\tif err := verifyFixManifest(root, before); err != nil {
\t\treturn fmt.Errorf("quality fixer input changed while isolated fixer ran: %w", err)
\t}
\tfor _, path := range changed {
\t\tif err := applyFixPostimage(root, path, before[path], after[path]); err != nil {
\t\t\treturn err
\t\t}
\t}
\tif err := verifyFixManifest(root, after); err != nil {
\t\treturn fmt.Errorf("quality fixer postimage verification failed: %w", err)
\t}
\treturn nil
}
''',
)

types = Path("glm-worker/internal/harnesslint/types.go")
text = types.read_text()
text = text.replace('const FixProvenanceIsolatedPostimageV1 = "isolated-postimage-v1"\n\n', '', 1)
marker = '''type Report struct {
\tStatus      string       `json:"status"`
\tFixed       int          `json:"fixed"`
\tViolations  []Violation  `json:"violations"`
\tFixEvidence *FixEvidence `json:"fix_evidence,omitempty"`
}
'''
if marker not in text:
    raise SystemExit("types report marker missing")
text = text.replace(marker, marker + '\nconst FixProvenanceIsolatedPostimageV1 = "isolated-postimage-v1"\n', 1)
types.write_text(text)

replace_between(
    "glm-worker/internal/workflow/quality_fixer_snapshot_test.go",
    'func TestQualityFixSnapshotRejectsUnprovenFixedReport(',
    '\nfunc TestQualityPassWithoutFixDoesNotRebaseExternalChange(',
    '''func TestQualityFixRejectsUnprovenFixedReport(t *testing.T) {
\tfor _, status := range []string{"pass", "fail"} {
\t\tt.Run(status, func(t *testing.T) {
\t\t\tst := newStateStoreT(t)
\t\t\tr := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("initial")}}}
\t\t\tw := newWorkflowT(t, st, r)
\t\t\tpath := filepath.Join(w.config.RepoRoot, "fixture.go")
\t\t\tif err := os.WriteFile(path, []byte("package fixture\\n"), 0o644); err != nil {
\t\t\t\tt.Fatal(err)
\t\t\t}
\t\t\tw.captureSnapshot = state.CaptureGitSnapshot
\t\t\tw.captureBoundarySnapshot = state.CaptureRepositoryBoundarySnapshot
\t\t\tw.qualityGate = func(string) (harnesslint.Report, error) {
\t\t\t\tif err := os.WriteFile(path, []byte("package fixture\\n\\nvar changed = true\\n"), 0o644); err != nil {
\t\t\t\t\treturn harnesslint.Report{}, err
\t\t\t\t}
\t\t\t\treport := harnesslint.Report{Status: status, Fixed: 1, Violations: []harnesslint.Violation{}}
\t\t\t\tif status == "fail" {
\t\t\t\t\treport.Violations = []harnesslint.Violation{{Rule: "fixture", Path: "fixture.go", Line: 1, Column: 1, Message: "still invalid"}}
\t\t\t\t}
\t\t\t\treturn report, nil
\t\t\t}
\t\t\tif err := w.ExecuteNewTask("request"); err != nil {
\t\t\t\tt.Fatal(err)
\t\t\t}
\t\t\tif len(r.phases) != 1 {
\t\t\t\tt.Fatalf("provenance確認前に次phaseへ進んでいます: %v", r.phases)
\t\t\t}
\t\t\tif st.TaskStatus() != state.TaskStatusWaitingSolReview {
\t\t\t\tt.Fatalf("provenanceのないmachine fixはfail closedすべきです: %s", st.TaskStatus())
\t\t\t}
\t\t})
\t}
}
''',
)
