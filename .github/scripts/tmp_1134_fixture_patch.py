from pathlib import Path


def replace_one(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected exactly one match, got {count}")
    p.write_text(text.replace(old, new, 1))


replace_one(
    "glm-worker/internal/workflow/risk_floor_test.go",
    '''func TestRiskFloorReemitFailClosedOnRepeatedPass(t *testing.T) {
\tst := newStateStoreT(t)
\tr := &scriptedRunner{steps: []runnerStep{
\t\t{structured: implementedPacketWithRisk("high risk work", "HIGH")},
\t\t{structured: passPacket()},
\t\t{structured: passPacket()},
\t}}
\tw := newWorkflowT(t, st, r)
''',
    '''func TestRiskFloorReemitFailClosedOnRepeatedPass(t *testing.T) {
\tst := newStateStoreT(t)
\tr := &scriptedRunner{steps: []runnerStep{
\t\t{structured: implementedPacketWithRisk("high risk work", "HIGH")},
\t\t{structured: passPacket()},
\t\t{structured: passPacket()},
\t}}
\tw := newWorkflowT(t, st, r)
\tw.collectChangedPaths = func(string, string) ([]string, error) { return []string{"internal/task/change.go"}, nil }
''',
)

replace_one(
    "glm-worker/internal/workflow/risk_floor_test.go",
    '''func TestRiskFloorReemitResumeFailClosed(t *testing.T) {
\tst := newStateStoreT(t)
''',
    '''func TestRiskFloorReemitResumeFailClosed(t *testing.T) {
\tst := newStateStoreT(t)
''',
)
replace_one(
    "glm-worker/internal/workflow/risk_floor_test.go",
    '''\tr := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
\tw := newWorkflowT(t, st, r)

\tif err := w.ExecuteResume(); err != nil {
''',
    '''\tr := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
\tw := newWorkflowT(t, st, r)
\tw.collectChangedPaths = func(string, string) ([]string, error) { return []string{"internal/task/change.go"}, nil }

\tif err := w.ExecuteResume(); err != nil {
''',
)

replace_one(
    "glm-worker/internal/workflow/diagnostic_test.go",
    '''func TestDiagnosticRiskFloorReemitCallHasNoFloorDiagnostics(t *testing.T) {
\tst := newStateStoreT(t)
\tr := &scriptedRunner{steps: []runnerStep{
\t\t{structured: implementedPacketWithRisk("risky", "HIGH")},
\t\t{structured: passPacket()},
\t\t{structured: passPacket()},
\t}}
\tw := newWorkflowT(t, st, r)
''',
    '''func TestDiagnosticRiskFloorReemitCallHasNoFloorDiagnostics(t *testing.T) {
\tst := newStateStoreT(t)
\tr := &scriptedRunner{steps: []runnerStep{
\t\t{structured: implementedPacketWithRisk("risky", "HIGH")},
\t\t{structured: passPacket()},
\t\t{structured: passPacket()},
\t}}
\tw := newWorkflowT(t, st, r)
\tw.collectChangedPaths = func(string, string) ([]string, error) { return []string{"internal/task/change.go"}, nil }
''',
)
