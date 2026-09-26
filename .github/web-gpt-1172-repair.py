from pathlib import Path


def replace_once(path, old, new):
    p = Path(path)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected 1 match, found {count}: {old!r}")
    p.write_text(text.replace(old, new, 1))


workflow = "glm-worker/internal/workflow/workflow.go"
replace_once(
    workflow,
    "\tcollectChangedPaths     func(repoRoot, baselineHead string) ([]string, error)\n",
    "\tcollectChangedPaths       func(repoRoot, baselineHead string) ([]string, error)\n\tcollectReviewTargetPaths func(repoRoot, baselineHead string) ([]string, error)\n",
)

review_targets = "glm-worker/internal/workflow/internal_review_targets.go"
replace_once(
    review_targets,
    '''func (w *Workflow) currentReviewDiffTargets() ([]string, error) {
\tif w.collectChangedPaths == nil {
\t\treturn nil, fmt.Errorf("current task review targets: changed-path collector is unavailable")
\t}
\tpaths, err := w.collectChangedPaths(w.config.RepoRoot, w.state.ReadOr("baseline-head", ""))
''',
    '''func (w *Workflow) currentReviewDiffTargets() ([]string, error) {
\tcollect := w.collectChangedPaths
\tif w.collectReviewTargetPaths != nil {
\t\tcollect = w.collectReviewTargetPaths
\t}
\tif collect == nil {
\t\treturn nil, fmt.Errorf("current task review targets: changed-path collector is unavailable")
\t}
\tpaths, err := collect(w.config.RepoRoot, w.state.ReadOr("baseline-head", ""))
''',
)

workflow_test = "glm-worker/internal/workflow/workflow_test.go"
replace_once(
    workflow_test,
    '''\tw.collectChangedPaths = func(string, string) ([]string, error) {
\t\treturn []string{"tracked.go"}, nil
\t}
\tclock := newFakeClock()
''',
    '''\tw.collectChangedPaths = func(string, string) ([]string, error) {
\t\treturn nil, nil
\t}
\tw.collectReviewTargetPaths = func(string, string) ([]string, error) {
\t\treturn []string{"tracked.go"}, nil
\t}
\tclock := newFakeClock()
''',
)

snapshot_test = "glm-worker/internal/workflow/workflow_snapshot_test.go"
replace_once(
    snapshot_test,
    '''\tw.collectChangedPaths = func(string, string) ([]string, error) {
\t\treturn []string{"tracked.go"}, nil
\t}
''',
    '''\tw.collectReviewTargetPaths = func(string, string) ([]string, error) {
\t\treturn []string{"tracked.go"}, nil
\t}
''',
)

for path in [
    "glm-worker/internal/workflow/internal_review_targets_test.go",
    "glm-worker/internal/workflow/internal_review_producer_failclosed_test.go",
]:
    p = Path(path)
    text = p.read_text()
    if "w.collectChangedPaths =" not in text:
        raise SystemExit(f"{path}: no review-target collector assignments found")
    p.write_text(text.replace("w.collectChangedPaths =", "w.collectReviewTargetPaths ="))

risk_test = "glm-worker/internal/workflow/risk_floor_parent_evidence_test.go"
p = Path(risk_test)
text = p.read_text()
needle = "w.collectChangedPaths = func(string, string) ([]string, error)"
if text.count(needle) != 2:
    raise SystemExit(f"{risk_test}: expected 2 collector assignments, found {text.count(needle)}")
p.write_text(text.replace(needle, "w.collectReviewTargetPaths = func(string, string) ([]string, error)"))
