from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    if text.count(old) != 1:
        raise SystemExit(f"{path}: replacement count is {text.count(old)}")
    p.write_text(text.replace(old, new, 1))


replace_once(
    "glm-worker/internal/workflow/review_flow.go",
    '''\tif qualityReport.Fixed > 0 {
\t\treviewInput, stopped, err = w.acceptQualityFixSnapshot(workerEnd, parentBefore, qualityReport)
\t\tif err != nil || stopped {
\t\t\treturn reviewInput, true, err
\t\t}
\t}
''',
    '''\tif qualityReport.Fixed > 0 {
\t\treviewInput, stopped, err = w.acceptQualityFixSnapshot(workerEnd, parentBefore, qualityReport)
\t} else {
\t\treviewInput, stopped, err = w.acceptQualityNoFixSnapshot(workerEnd)
\t}
\tif err != nil || stopped {
\t\treturn reviewInput, true, err
\t}
''',
)

replace_once(
    "glm-worker/internal/workflow/review_flow.go",
    '''func (w *Workflow) acceptQualityFixSnapshot(workerEnd state.GitSnapshot, parentBefore state.ParentFileStates, report harnesslint.Report) (state.GitSnapshot, bool, error) {
''',
    '''func (w *Workflow) acceptQualityNoFixSnapshot(workerEnd state.GitSnapshot) (state.GitSnapshot, bool, error) {
\tcurrent, err := w.captureSnapshot(w.config.RepoRoot)
\tif err != nil {
\t\treturn current, true, w.failClosedSnapshot(
\t\t\tstate.SnapshotStageReviewStart,
\t\t\tworkerEnd,
\t\t\tstate.GitSnapshot{},
\t\t\t"machine quality gate後snapshot取得失敗",
\t\t\terr,
\t\t)
\t}
\tif state.EqualGitSnapshot(workerEnd, current) {
\t\treturn current, false, nil
\t}
\treturn current, true, w.failClosedSnapshot(
\t\tstate.SnapshotStageReviewStart,
\t\tworkerEnd,
\t\tcurrent,
\t\t"machine quality gate実行中にfixer由来でないrepository変更を検出しました",
\t\tnil,
\t)
}

func (w *Workflow) acceptQualityFixSnapshot(workerEnd state.GitSnapshot, parentBefore state.ParentFileStates, report harnesslint.Report) (state.GitSnapshot, bool, error) {
''',
)

p = Path("glm-worker/internal/workflow/quality_fixer_snapshot_test.go")
text = p.read_text()
start = text.index("func TestQualityPassWithoutFixDoesNotRebaseExternalChange(")
end = text.index("\nfunc TestParentFileStatesRequireExactMatch(", start)
replacement = '''func TestQualityNoFixRejectsExternalChangeBeforeNextPhase(t *testing.T) {
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
\t\t\t\treport := harnesslint.Report{Status: status, Fixed: 0, Violations: []harnesslint.Violation{}}
\t\t\t\tif status == "fail" {
\t\t\t\t\treport.Violations = []harnesslint.Violation{{Rule: "fixture", Path: "fixture.go", Line: 1, Column: 1, Message: "still invalid"}}
\t\t\t\t}
\t\t\t\treturn report, nil
\t\t\t}
\t\t\tif err := w.ExecuteNewTask("request"); err != nil {
\t\t\t\tt.Fatal(err)
\t\t\t}
\t\t\tif len(r.phases) != 1 {
\t\t\t\tt.Fatalf("external変更確認前に次phaseへ進んでいます: %v", r.phases)
\t\t\t}
\t\t\tif st.TaskStatus() != state.TaskStatusWaitingSolReview {
\t\t\t\tt.Fatalf("fixer由来でないexternal変更はfail closedすべきです: %s", st.TaskStatus())
\t\t\t}
\t\t})
\t}
}
'''
p.write_text(text[:start] + replacement + text[end:])
