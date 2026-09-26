from pathlib import Path


def replace_once(path, old, new):
    p = Path(path)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected 1 match, found {count}: {old!r}")
    p.write_text(text.replace(old, new, 1))


# Risk-floor tests that intentionally inject semantic task paths must inject
# the shared review-target collector now that it owns risk-floor TARGETS too.
p = Path("glm-worker/internal/workflow/risk_floor_test.go")
text = p.read_text()
needle = "w.collectChangedPaths = func(string, string) ([]string, error)"
if text.count(needle) < 1:
    raise SystemExit("risk_floor_test.go: no changed-path fixtures found")
p.write_text(text.replace(needle, "w.collectReviewTargetPaths = func(string, string) ([]string, error)"))

# #1172 intentionally replaces inherited failure TARGETS with actual current-task
# diff TARGETS for terminal parent-validation non-convergence.
replace_once(
    "glm-worker/internal/workflow/parent_validation_test.go",
    "func TestParentValidationBudgetExhaustionKeepsFailureTargets(t *testing.T) {",
    "func TestParentValidationBudgetExhaustionUsesCurrentTaskTargets(t *testing.T) {",
)
replace_once(
    "glm-worker/internal/workflow/parent_validation_test.go",
    '''\tif !strings.Contains(emitted, `"targets":["a.go:@diff"]`) {
\t\tt.Fatalf("terminal packet must keep the harnesslint failure targets: %s", emitted)
\t}''',
    '''\tif !strings.Contains(emitted, `"targets":["tracked.go:@diff"]`) {
\t\tt.Fatalf("terminal packet must use the actual current-task review target: %s", emitted)
\t}''',
)

# This test isolates quality-surface accepted-scope lifecycle. Keep its synthetic
# current-task target explicit so the new review-target guard does not change the
# lifecycle axis under test.
replace_once(
    "glm-worker/internal/workflow/quality_surface_parent_approval_test.go",
    '''\tw := NewWorkflow(cfg, st, nil, io.Discard)
\tw.temp = t.TempDir()
''',
    '''\tw := NewWorkflow(cfg, st, nil, io.Discard)
\tw.collectReviewTargetPaths = func(string, string) ([]string, error) { return []string{"commentlint"}, nil }
\tw.temp = t.TempDir()
''',
)
