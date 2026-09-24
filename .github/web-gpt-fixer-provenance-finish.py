from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    if text.count(old) != 1:
        raise SystemExit(f"{path}: expected one replacement for {old[:80]!r}, got {text.count(old)}")
    p.write_text(text.replace(old, new, 1))


replace_once(
    "glm-worker/internal/harnesslint/fix_isolation.go",
    '"sort"\n)',
    '"sort"\n\n\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"\n)',
)
replace_once(
    "glm-worker/internal/harnesslint/fix_isolation.go",
    'func runWithIsolatedFixes(root string, execute isolatedFixRun) (Report, error) {\n\tpaths, err := repositoryPaths(root)',
    'func runWithIsolatedFixes(root string, execute isolatedFixRun) (Report, error) {\n\tinput, err := state.CaptureGitSnapshot(root)\n\tif err != nil {\n\t\treturn Report{}, err\n\t}\n\tpaths, err := repositoryPaths(root)',
)
replace_once(
    "glm-worker/internal/harnesslint/fix_isolation.go",
    'report.FixEvidence = &FixEvidence{Method: FixProvenanceIsolatedPostimageV1}\n',
    'report.FixEvidence = &FixEvidence{\n\t\tMethod: FixProvenanceIsolatedPostimageV1,\n\t\tInput: &FixInputSnapshot{\n\t\t\tHead:           input.Head,\n\t\t\tIndexDigest:    input.IndexDigest,\n\t\t\tWorktreeDigest: input.WorktreeDigest,\n\t\t},\n\t}\n',
)

q = Path("glm-worker/internal/workflow/quality_gate.go")
text = q.read_text()
text = text.replace('\n\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"', '')
start = text.index('func runRepositoryQualityGate(root string) (harnesslint.Report, error) {')
end = text.index('\nfunc captureQualitySurfaceDigest', start)
new_func = '''func runRepositoryQualityGate(root string) (harnesslint.Report, error) {
\tif root == "" {
\t\treturn harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
\t}
\tqualityToolsApply, err := repositoryharness.QualityToolsApply(root)
\tif err != nil {
\t\treturn harnesslint.Report{}, err
\t}
\tif !qualityToolsApply {
\t\treturn harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
\t}
\treport, err := harnesslint.Run(root, true)
\tif err != nil {
\t\treturn harnesslint.Report{}, err
\t}
\tif report.Fixed > 0 && (report.FixEvidence == nil || report.FixEvidence.Method != harnesslint.FixProvenanceIsolatedPostimageV1 || report.FixEvidence.Input == nil) {
\t\treturn harnesslint.Report{}, fmt.Errorf("machine quality fixer provenance is missing")
\t}
\treturn report, nil
}
'''
q.write_text(text[:start] + new_func + text[end:])

replace_once(
    "glm-worker/internal/workflow/review_flow.go",
    'return w.acceptQualityFixSnapshot(workerEnd, parentBefore, qualityReport.Fixed)',
    'return w.acceptQualityFixSnapshot(workerEnd, parentBefore, qualityReport)',
)
review = Path("glm-worker/internal/workflow/review_flow.go")
text = review.read_text()
start = text.index('func (w *Workflow) acceptQualityFixSnapshot(')
end = text.index('\nfunc (w *Workflow) handleRepositoryQualityViolation(', start)
new_accept = '''func (w *Workflow) acceptQualityFixSnapshot(workerEnd state.GitSnapshot, parentBefore state.ParentFileStates, report harnesslint.Report) (state.GitSnapshot, bool, error) {
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
\tevidence := report.FixEvidence
\tif evidence == nil || evidence.Method != harnesslint.FixProvenanceIsolatedPostimageV1 || evidence.Input == nil {
\t\treturn reviewInput, true, w.failClosedSnapshot(
\t\t\tstate.SnapshotStageReviewStart,
\t\t\tworkerEnd,
\t\t\treviewInput,
\t\t\tfmt.Sprintf("machine quality fixer provenanceがありません(fixed=%d)", report.Fixed),
\t\t\tnil,
\t\t)
\t}
\tif evidence.Input.Head != workerEnd.Head || evidence.Input.IndexDigest != workerEnd.IndexDigest || evidence.Input.WorktreeDigest != workerEnd.WorktreeDigest {
\t\treturn reviewInput, true, w.failClosedSnapshot(
\t\t\tstate.SnapshotStageReviewStart,
\t\t\tworkerEnd,
\t\t\treviewInput,
\t\t\tfmt.Sprintf("machine quality fixer provenanceがworker-end snapshotと一致しません(fixed=%d)", report.Fixed),
\t\t\tnil,
\t\t)
\t}
\tif reviewInput.Head != workerEnd.Head || reviewInput.IndexDigest != workerEnd.IndexDigest {
\t\treturn reviewInput, true, w.failClosedSnapshot(
\t\t\tstate.SnapshotStageReviewStart,
\t\t\tworkerEnd,
\t\t\treviewInput,
\t\t\tfmt.Sprintf("machine quality fixer実行中にHEAD/indexが変化しました(fixed=%d)", report.Fixed),
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
'''
review.write_text(text[:start] + new_accept + text[end:])
