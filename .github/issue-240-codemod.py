from pathlib import Path


def replace_exact(path: str, old: str, new: str, count: int = 1) -> None:
    file_path = Path(path)
    content = file_path.read_text()
    actual = content.count(old)
    if actual != count:
        raise SystemExit(f"{path}: expected {count} occurrences, found {actual}: {old[:80]!r}")
    file_path.write_text(content.replace(old, new))


workflow = "glm-worker/internal/workflow/workflow.go"
replace_exact(
    workflow,
    "\tcaptureSnapshot       func(repoRoot string) (state.GitSnapshot, error)\n\tcollectChangedPaths   func(repoRoot, baselineHead string) ([]string, error)\n",
    "\tcaptureSnapshot         func(repoRoot string) (state.GitSnapshot, error)\n\tcaptureBoundarySnapshot func(repoRoot string) (state.GitSnapshot, error)\n\tcollectChangedPaths     func(repoRoot, baselineHead string) ([]string, error)\n",
)
replace_exact(
    workflow,
    "\t\tcaptureSnapshot:       state.CaptureGitSnapshot,\n\t\tcollectChangedPaths:   collectChangedPaths,\n",
    "\t\tcaptureSnapshot:         state.CaptureGitSnapshot,\n\t\tcaptureBoundarySnapshot: state.CaptureRepositoryBoundarySnapshot,\n\t\tcollectChangedPaths:     collectChangedPaths,\n",
)
replace_exact(
    workflow,
    "\tif loadErr != nil || resumeCheckpointStatus(saved) == state.TaskStatusActive {\n\t\tprevious.StopParentFiles = captureStopParentFiles(w.config.RepoRoot)\n\t\t_ = w.state.SaveResumeCheckpoint(previous)\n\t}\n",
    "\tif loadErr != nil || resumeCheckpointStatus(saved) == state.TaskStatusActive {\n\t\t_ = w.attachStopRepositoryBoundary(&previous)\n\t\t_ = w.state.SaveResumeCheckpoint(previous)\n\t}\n",
)
replace_exact(
    workflow,
    "\tcheckpoint.StopParentFiles = captureStopParentFiles(w.config.RepoRoot)\n",
    "\t_ = w.attachStopRepositoryBoundary(&checkpoint)\n",
    count=3,
)
replace_exact(
    workflow,
    "\tif snapshot, snapErr := state.CaptureGitSnapshot(w.config.RepoRoot); snapErr == nil {\n\t\tif files, filesErr := state.CaptureStopDirtyFiles(w.config.RepoRoot); filesErr == nil {\n\t\t\tcheckpoint.StopGitSnapshot = &snapshot\n\t\t\tcheckpoint.StopDirtyFiles = files\n\t\t}\n\t}\n",
    "\tif files, filesErr := state.CaptureStopDirtyFiles(w.config.RepoRoot); filesErr == nil {\n\t\tcheckpoint.StopDirtyFiles = files\n\t}\n",
)
replace_exact(
    workflow,
    "\treviewStart, err := w.captureSnapshot(w.config.RepoRoot)\n\tif err != nil {\n\t\treturn true, w.failClosedSnapshot(state.SnapshotStageReviewStart, workerEnd, state.GitSnapshot{}, \"review-start snapshot取得失敗\", err)\n\t}\n\n\tif parentStates, parentErr := readParentFileStates(w.config.RepoRoot); parentErr == nil {\n\t\treviewStart.ParentFiles = &parentStates\n\t}\n",
    "\treviewStart, err := w.captureRepositoryBoundary()\n\tif err != nil {\n\t\treturn true, w.failClosedSnapshot(state.SnapshotStageReviewStart, workerEnd, state.GitSnapshot{}, \"review-start snapshot取得失敗\", err)\n\t}\n",
)
replace_exact(
    workflow,
    "\tcurrent, err := w.captureSnapshot(w.config.RepoRoot)\n\tif err != nil {\n\t\treturn true, w.failClosedSnapshot(state.SnapshotStageReviewResume, saved, state.GitSnapshot{}, \"resume時snapshot取得失敗\", err)\n\t}\n",
    "\tcurrent, err := w.captureRepositoryBoundary()\n\tif err != nil {\n\t\treturn true, w.failClosedSnapshot(state.SnapshotStageReviewResume, saved, state.GitSnapshot{}, \"resume時snapshot取得失敗\", err)\n\t}\n",
)
replace_exact(
    workflow,
    "\tif comparison.ParentUpdateAccepted {\n\t\tif parentStates, parentErr := readParentFileStates(w.config.RepoRoot); parentErr == nil {\n\t\t\tcurrent.ParentFiles = &parentStates\n\t\t}\n\t\tif err := w.state.SaveReviewStartSnapshot(current); err != nil {\n",
    "\tif comparison.ParentUpdateAccepted {\n\t\tif err := w.state.SaveReviewStartSnapshot(current); err != nil {\n",
)
replace_exact(
    workflow,
    "\tif !reviewResumeParentBaselineMatches(saved, current) || saved.ParentFiles == nil || checkpoint.StopParentFiles == nil {\n\t\treturn false\n\t}\n\tnow, err := readParentFileStates(w.config.RepoRoot)\n\tif err != nil {\n\t\treturn false\n\t}\n",
    "\tif !reviewResumeParentBaselineMatches(saved, current) || saved.ParentFiles == nil || checkpoint.StopParentFiles == nil || current.ParentFiles == nil {\n\t\treturn false\n\t}\n\tnow := *current.ParentFiles\n",
)

replace_exact(
    "glm-worker/internal/workflow/workflow_test.go",
    "\tw.captureSnapshot = func(string) (state.GitSnapshot, error) {\n\t\treturn fixedSnapshot, nil\n\t}\n\tw.collectChangedPaths = func(string, string) ([]string, error) {\n",
    "\tw.captureSnapshot = func(string) (state.GitSnapshot, error) {\n\t\treturn fixedSnapshot, nil\n\t}\n\tw.captureBoundarySnapshot = func(repoRoot string) (state.GitSnapshot, error) {\n\t\tsnapshot, err := w.captureSnapshot(repoRoot)\n\t\tif err != nil {\n\t\t\treturn snapshot, err\n\t\t}\n\t\tparents := state.ParentFileStates{}\n\t\tsnapshot.ParentFiles = &parents\n\t\treturn snapshot, nil\n\t}\n\tw.collectChangedPaths = func(string, string) ([]string, error) {\n",
)

replace_exact(
    "glm-worker/internal/workflow/workflow_snapshot_test.go",
    "func newSnapshotWorkflow(st *state.StateStore, r *scriptedRunner, out io.Writer) *Workflow {\n\treturn NewWorkflow(config.AppConfig{\n\t\tWorkerModel:           \"opus\",\n\t\tReviewerModel:         \"haiku\",\n\t\tHighRiskReviewerModel: \"sonnet\",\n\t\tRoutineEffort:         \"high\",\n\t\tMaxAutoFixRounds:      2,\n\t}, st, r, out)\n}\n",
    "func newSnapshotWorkflow(st *state.StateStore, r *scriptedRunner, out io.Writer) *Workflow {\n\tw := NewWorkflow(config.AppConfig{\n\t\tWorkerModel:           \"opus\",\n\t\tReviewerModel:         \"haiku\",\n\t\tHighRiskReviewerModel: \"sonnet\",\n\t\tRoutineEffort:         \"high\",\n\t\tMaxAutoFixRounds:      2,\n\t}, st, r, out)\n\tw.captureBoundarySnapshot = func(repoRoot string) (state.GitSnapshot, error) {\n\t\tsnapshot, err := w.captureSnapshot(repoRoot)\n\t\tif err != nil {\n\t\t\treturn snapshot, err\n\t\t}\n\t\tparents := state.ParentFileStates{}\n\t\tsnapshot.ParentFiles = &parents\n\t\treturn snapshot, nil\n\t}\n\treturn w\n}\n",
)

replace_exact(
    "glm-worker/internal/workflow/review_resume_parent_test.go",
    "func newReviewResumeWorkflow(t *testing.T, st *state.StateStore, r *scriptedRunner, out io.Writer) *Workflow {\n\tt.Helper()\n\tw := newWorkflowT(t, st, r)\n\tw.output = out\n\treturn w\n}\n",
    "func newReviewResumeWorkflow(t *testing.T, st *state.StateStore, r *scriptedRunner, out io.Writer) *Workflow {\n\tt.Helper()\n\tw := newWorkflowT(t, st, r)\n\tw.output = out\n\tw.captureBoundarySnapshot = func(repoRoot string) (state.GitSnapshot, error) {\n\t\tsnapshot, err := w.captureSnapshot(repoRoot)\n\t\tif err != nil {\n\t\t\treturn snapshot, err\n\t\t}\n\t\tparents, err := state.CaptureParentFileStates(repoRoot)\n\t\tif err != nil {\n\t\t\treturn snapshot, err\n\t\t}\n\t\tsnapshot.ParentFiles = &parents\n\t\treturn snapshot, nil\n\t}\n\treturn w\n}\n",
)

replace_exact(
    "glm-worker/internal/workflow/planfile.go",
    "func captureStopParentFiles(repoRoot string) *state.ParentFileStates {\n\tstates, err := state.CaptureParentFileStates(repoRoot)\n\tif err != nil {\n\t\treturn nil\n\t}\n\treturn &states\n}\n\n",
    "",
)
