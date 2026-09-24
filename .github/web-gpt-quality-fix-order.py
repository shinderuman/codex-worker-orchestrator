from pathlib import Path

p = Path("glm-worker/internal/workflow/review_flow.go")
text = p.read_text()
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
p.write_text(text)
