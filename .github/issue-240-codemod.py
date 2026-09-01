from pathlib import Path


def replace_exact(path: str, old: str, new: str, count: int = 1) -> None:
    file_path = Path(path)
    content = file_path.read_text()
    actual = content.count(old)
    if actual != count:
        raise SystemExit(f"{path}: expected {count} occurrences, found {actual}: {old[:80]!r}")
    file_path.write_text(content.replace(old, new))


replace_exact(
    "glm-worker/internal/workflow/planfile.go",
    "func readParentFileState(repoRoot string, name string) (state.ParentFileState, error) {\n\treturn state.CaptureParentFileState(repoRoot, name)\n}\n\nfunc readParentFileStates(repoRoot string) (state.ParentFileStates, error) {\n\treturn state.CaptureParentFileStates(repoRoot)\n}\n\n",
    "",
)

replace_exact(
    "glm-worker/internal/workflow/reviewer_repo_search.go",
    "isParentManagedReviewPath(path)",
    "state.IsParentManagedPath(path)",
    count=2,
)
replace_exact(
    "glm-worker/internal/workflow/reviewer_repo_search.go",
    "\nfunc isParentManagedReviewPath(path string) bool {\n\treturn state.IsParentManagedPath(path)\n}\n",
    "",
)

replace_exact(
    "glm-worker/internal/workflow/review_resume_parent_test.go",
    "\tstates, err := readParentFileStates(repoRoot)\n",
    "\tstates, err := state.CaptureParentFileStates(repoRoot)\n",
)

replace_exact(
    "glm-worker/internal/workflow/workflow.go",
    "!w.acceptReviewResumeParentDelta(saved, current, checkpoint)",
    "!acceptReviewResumeParentDelta(saved, current, checkpoint)",
)
replace_exact(
    "glm-worker/internal/workflow/workflow.go",
    "func (w *Workflow) acceptReviewResumeParentDelta(saved, current state.GitSnapshot, checkpoint state.ResumeCheckpoint) bool {",
    "func acceptReviewResumeParentDelta(saved, current state.GitSnapshot, checkpoint state.ResumeCheckpoint) bool {",
)
