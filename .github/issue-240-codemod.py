from pathlib import Path


def replace_exact(path: str, old: str, new: str, count: int = 1) -> None:
    file_path = Path(path)
    content = file_path.read_text()
    actual = content.count(old)
    if actual != count:
        raise SystemExit(f"{path}: expected {count} occurrences, found {actual}: {old[:80]!r}")
    file_path.write_text(content.replace(old, new))


replace_exact(
    "glm-worker/internal/workflow/workflow_test.go",
    "\tw.captureBoundarySnapshot = func(repoRoot string) (state.GitSnapshot, error) {\n\t\tsnapshot, err := w.captureSnapshot(repoRoot)\n\t\tif err != nil {\n\t\t\treturn snapshot, err\n\t\t}\n\t\tparents := state.ParentFileStates{}\n\t\tsnapshot.ParentFiles = &parents\n\t\treturn snapshot, nil\n\t}\n",
    "\tw.captureBoundarySnapshot = func(repoRoot string) (state.GitSnapshot, error) {\n\t\tsnapshot, err := w.captureSnapshot(repoRoot)\n\t\tif err != nil {\n\t\t\treturn snapshot, err\n\t\t}\n\t\tparents, err := state.CaptureParentFileStates(repoRoot)\n\t\tif err != nil {\n\t\t\treturn snapshot, err\n\t\t}\n\t\tsnapshot.ParentFiles = &parents\n\t\treturn snapshot, nil\n\t}\n",
)

replace_exact(
    "glm-worker/internal/workflow/execution_milestones_test.go",
    "\tw := NewWorkflow(cfg, st, runner, io.Discard)\n\tw.captureSnapshot = func(string) (state.GitSnapshot, error) { return fixedSnapshot, nil }\n\tw.collectChangedPaths = func(string, string) ([]string, error) { return nil, nil }\n",
    "\tw := NewWorkflow(cfg, st, runner, io.Discard)\n\tw.captureSnapshot = func(string) (state.GitSnapshot, error) { return fixedSnapshot, nil }\n\tw.captureBoundarySnapshot = func(repoRoot string) (state.GitSnapshot, error) {\n\t\tsnapshot, err := w.captureSnapshot(repoRoot)\n\t\tif err != nil {\n\t\t\treturn snapshot, err\n\t\t}\n\t\tparents, err := state.CaptureParentFileStates(repoRoot)\n\t\tif err != nil {\n\t\t\treturn snapshot, err\n\t\t}\n\t\tsnapshot.ParentFiles = &parents\n\t\treturn snapshot, nil\n\t}\n\tw.collectChangedPaths = func(string, string) ([]string, error) { return nil, nil }\n",
)
