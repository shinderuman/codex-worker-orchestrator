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
    '''\treviewInput, stopped, err = w.acceptQualityGateSnapshot(workerEnd, parentBefore, qualityReport)
\tif err != nil || stopped {
\t\treturn reviewInput, true, err
\t}
''',
)

replace_once(
    "glm-worker/internal/workflow/review_flow.go",
    '''func (w *Workflow) acceptQualityFixSnapshot(workerEnd state.GitSnapshot, parentBefore state.ParentFileStates, report harnesslint.Report) (state.GitSnapshot, bool, error) {
''',
    '''func (w *Workflow) acceptQualityGateSnapshot(workerEnd state.GitSnapshot, parentBefore state.ParentFileStates, report harnesslint.Report) (state.GitSnapshot, bool, error) {
\tif report.Fixed > 0 {
\t\treturn w.acceptQualityFixSnapshot(workerEnd, parentBefore, report)
\t}
\tif harnesslint.IsViolation(report) {
\t\treturn w.guardQualityViolationNoFixSnapshot(workerEnd)
\t}
\treturn workerEnd, false, nil
}

func (w *Workflow) guardQualityViolationNoFixSnapshot(workerEnd state.GitSnapshot) (state.GitSnapshot, bool, error) {
\tcurrent, err := w.captureSnapshot(w.config.RepoRoot)
\tif err != nil {
\t\treturn current, true, w.failClosedSnapshot(
\t\t\tstate.SnapshotStageReviewStart,
\t\t\tworkerEnd,
\t\t\tstate.GitSnapshot{},
\t\t\t"machine quality violation後snapshot取得失敗",
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
marker = "func TestQualityPassWithoutFixDoesNotRebaseExternalChange(t *testing.T) {"
insert = '''func TestQualityViolationWithoutFixRejectsExternalChangeBeforeAutoFix(t *testing.T) {
\tst := newStateStoreT(t)
\tr := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("initial")}}}
\tw := newWorkflowT(t, st, r)
\tpath := filepath.Join(w.config.RepoRoot, "fixture.go")
\tif err := os.WriteFile(path, []byte("package fixture\\n"), 0o644); err != nil {
\t\tt.Fatal(err)
\t}
\tw.captureSnapshot = state.CaptureGitSnapshot
\tw.captureBoundarySnapshot = state.CaptureRepositoryBoundarySnapshot
\tw.qualityGate = func(string) (harnesslint.Report, error) {
\t\tif err := os.WriteFile(path, []byte("package fixture\\n\\nvar changed = true\\n"), 0o644); err != nil {
\t\t\treturn harnesslint.Report{}, err
\t\t}
\t\treturn harnesslint.Report{
\t\t\tStatus: "fail",
\t\t\tFixed:  0,
\t\t\tViolations: []harnesslint.Violation{{
\t\t\t\tRule: "fixture", Path: "fixture.go", Line: 1, Column: 1, Message: "still invalid",
\t\t\t}},
\t\t}, nil
\t}

\tif err := w.ExecuteNewTask("request"); err != nil {
\t\tt.Fatal(err)
\t}
\tif len(r.phases) != 1 {
\t\tt.Fatalf("external変更確認前にauto-fixへ進んでいます: %v", r.phases)
\t}
\tif st.TaskStatus() != state.TaskStatusWaitingSolReview {
\t\tt.Fatalf("fixer由来でないexternal変更はfail closedすべきです: %s", st.TaskStatus())
\t}
}

'''
if text.count(marker) != 1:
    raise SystemExit("pass no-fix test marker missing")
p.write_text(text.replace(marker, insert + marker, 1))
