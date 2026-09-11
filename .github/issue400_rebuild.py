from pathlib import Path
import re
import urllib.request

OLD_HEAD = "b148ac163c244af03306b667fb0659ddee4cbf11"
BASE = f"https://raw.githubusercontent.com/shinderuman/codex-worker-orchestrator/{OLD_HEAD}"

saved_files = [
    "glm-worker/internal/state/lifecycle_transition.go",
    "glm-worker/internal/state/parent_accept.go",
    "glm-worker/internal/state/parent_action.go",
    "glm-worker/internal/state/parent_no_go.go",
    "glm-worker/internal/state/parent_review.go",
    "glm-worker/internal/state/parent_review_authority_test.go",
    "glm-worker/internal/state/parent_review_state.go",
    "glm-worker/internal/state/quality_surface_recovery.go",
    "glm-worker/internal/state/stats.go",
    "glm-worker/internal/state/store.go",
    "glm-worker/internal/workflow/workflow.go",
]


def replace_once(path: str, old: str, new: str) -> None:
    file = Path(path)
    text = file.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected one replacement target, got {count}")
    file.write_text(text.replace(old, new, 1))


def replace_count(path: str, old: str, new: str, expected: int) -> None:
    file = Path(path)
    text = file.read_text()
    count = text.count(old)
    if count != expected:
        raise SystemExit(f"{path}: expected {expected} replacement targets, got {count}")
    file.write_text(text.replace(old, new))


def seed_after_task_write(path: str, task_expr: str, seed_expr: str, expected: int) -> None:
    file = Path(path)
    text = file.read_text()
    pattern = re.compile(
        rf'(?m)^(?P<i>[\t ]*)if err := st\.Write\("task\.id", {re.escape(task_expr)}\); err != nil \{{\n'
        rf'(?P=i)\tt\.Fatal\(err\)\n'
        rf'(?P=i)\}}\n'
    )
    matches = list(pattern.finditer(text))
    if len(matches) != expected:
        raise SystemExit(f"{path}: expected {expected} task fixture matches, got {len(matches)}")

    def repl(match: re.Match[str]) -> str:
        indent = match.group("i")
        return match.group(0) + f"{indent}{seed_expr}\n"

    file.write_text(pattern.sub(repl, text))


for path in saved_files:
    with urllib.request.urlopen(f"{BASE}/{path}") as response:
        Path(path).write_bytes(response.read())

replace_once(
    "glm-worker/internal/state/store.go",
    '''func (s *StateStore) InvalidateAllSessions() error {
\treturn s.Remove("worker.id", "worker.ready", "reviewer.id", "reviewer.ready", "isolation.policy", isolationOriginStateFile)
}

func (s *StateStore) InvalidateSession(role SessionRole) error {
\treturn s.Remove(string(role)+".id", string(role)+".ready")
}

''',
    "",
)

replace_once(
    "glm-worker/internal/state/store.go",
    '''\tif resume {
\t\tstats, err := s.loadTaskStats()
\t\tif err != nil || stats.TaskID != taskID {
\t\t\ts.InitializeTaskStats(taskID)
\t\t}
\t\tif _, err := s.loadParentReviewState(); err != nil {
\t\t\treturn "", fmt.Errorf("session rotation retry cannot verify parent review state: %w", err)
\t\t}
\t\tif err := s.SetTaskStatus(TaskStatusActive); err != nil {
\t\t\treturn "", err
\t\t}
\t\treturn taskID, nil
\t}
''',
    '''\tif resume {
\t\treturn s.resumeTaskWithID(taskID)
\t}
''',
)
replace_once(
    "glm-worker/internal/state/store.go",
    "func newTaskTransitionStateFileNames() []string {\n",
    '''func (s *StateStore) resumeTaskWithID(taskID string) (string, error) {
\tstats, err := s.loadTaskStats()
\tif err != nil || stats.TaskID != taskID {
\t\ts.InitializeTaskStats(taskID)
\t}
\tif _, err := s.loadParentReviewState(); err != nil {
\t\treturn "", fmt.Errorf("session rotation retry cannot verify parent review state: %w", err)
\t}
\tif err := s.SetTaskStatus(TaskStatusActive); err != nil {
\t\treturn "", err
\t}
\treturn taskID, nil
}

func newTaskTransitionStateFileNames() []string {
''',
)

replace_once(
    "glm-worker/internal/state/parent_review.go",
    '''func (s *StateStore) RecordParentOutcome(kind, origin, cause string) (bool, error) {
\tresolved, ok, err := s.resolveParentReviewState(kind, origin, cause)
\tif !ok || err != nil {
\t\treturn ok, err
\t}
\ts.UpdateTaskStats(func(stats *TaskStats) {
\t\tstats.ParentReviewOpen = nil
\t\tstats.recordParentOutcome(kind, origin, resolved)
\t})
\ttaskID, err := s.TaskID()
\tif err != nil {
\t\treturn false, err
\t}
\ts.appendParentOutcomeEvent(taskID, parentPhaseOfKind(kind), kind, origin, cause, resolved)
\treturn true, nil
}
''',
    '''func (s *StateStore) RecordParentOutcome(kind, origin, cause string) (bool, error) {
\ttaskID, err := s.TaskID()
\tif err != nil {
\t\treturn false, err
\t}
\tresolved, ok, err := s.resolveParentReviewState(kind, origin, cause)
\tif !ok || err != nil {
\t\treturn ok, err
\t}
\ts.UpdateTaskStats(func(stats *TaskStats) {
\t\tstats.ParentReviewOpen = nil
\t\tstats.recordParentOutcome(kind, origin, resolved)
\t})
\ts.appendParentOutcomeEvent(taskID, parentPhaseOfKind(kind), kind, origin, cause, resolved)
\treturn true, nil
}
''',
)

replace_once(
    "glm-worker/internal/state/parent_review_state.go",
    '''\tif kind == ParentOutcomeFix {
\t\tif err := validateParentFixDeclaration(origin, cause); err != nil {
\t\t\treturn ParentReviewOpenState{}, false, err
\t\t}
\t}
\tstate, err := s.loadParentReviewState()
''',
    '''\tif kind == ParentOutcomeFix {
\t\tif err := validateParentFixDeclaration(origin, cause); err != nil {
\t\t\treturn ParentReviewOpenState{}, false, err
\t\t}
\t}
\ttaskID, taskErr := s.Read("task.id")
\tif errors.Is(taskErr, os.ErrNotExist) || taskID == "" {
\t\treturn ParentReviewOpenState{}, false, nil
\t}
\tif taskErr != nil {
\t\treturn ParentReviewOpenState{}, false, taskErr
\t}
\tstate, err := s.loadParentReviewState()
''',
)

changed = 0
for path in Path("glm-worker").rglob("*_test.go"):
    lines = path.read_text().splitlines(keepends=True)
    out: list[str] = []
    file_changed = False
    for line in lines:
        stripped = line.strip()
        if (
            ".RecordSolResult(" in stripped
            and stripped.endswith(")")
            and not stripped.startswith("if err :=")
            and not stripped.startswith("_ =")
        ):
            indent = line[: len(line) - len(line.lstrip())]
            out.extend(
                [
                    f"{indent}if err := {stripped}; err != nil {{\n",
                    f"{indent}\tt.Fatal(err)\n",
                    f"{indent}}}\n",
                ]
            )
            changed += 1
            file_changed = True
        else:
            out.append(line)
    if file_changed:
        path.write_text("".join(out))
if changed != 35:
    raise SystemExit(f"expected 35 unchecked RecordSolResult calls, got {changed}")

replace_once(
    "glm-worker/internal/state/parent_review_test.go",
    "func recordPacket(st *StateStore, status packet.Status, risk packet.Risk, producer ParentReviewProducer) {",
    "func recordPacket(t *testing.T, st *StateStore, status packet.Status, risk packet.Risk, producer ParentReviewProducer) {",
)
path = Path("glm-worker/internal/state/parent_review_test.go")
path.write_text(path.read_text().replace("recordPacket(st, ", "recordPacket(t, st, "))

Path("glm-worker/internal/app/parent_review_state_fixture_test.go").write_text(
    '''package app

import (
\t"fmt"
\t"testing"

\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func seedParentReviewStateForTest(t *testing.T, st *state.StateStore, taskID string) {
\tt.Helper()
\tvalue := fmt.Sprintf(`{"version":1,"task_id":%q}`, taskID)
\tif err := st.Write("parent-review-state.json", value); err != nil {
\t\tt.Fatal(err)
\t}
}
'''
)

seed_after_task_write(
    "glm-worker/internal/app/parent_handoff_test.go",
    "routingEvidenceTaskID",
    "seedParentReviewStateForTest(t, st, routingEvidenceTaskID)",
    1,
)
replace_count(
    "glm-worker/internal/app/parent_handoff_test.go",
    '''\tst.UpdateTaskStats(func(stats *state.TaskStats) {
\t\tstats.ParentReviewOpen = &state.ParentReviewOpenState{PacketStatus: "PASS", Risk: "LOW"}
\t})
''',
    '''\tif err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{}); err != nil {
\t\tt.Fatal(err)
\t}
''',
    2,
)

seed_after_task_write(
    "glm-worker/internal/app/quality_preflight_test.go",
    "taskID",
    "seedParentReviewStateForTest(t, st, taskID)",
    1,
)
seed_after_task_write(
    "glm-worker/internal/app/watch_test.go",
    '"12345678-aaaa-bbbb-cccc-dddddddddddd"',
    'seedParentReviewStateForTest(t, st, "12345678-aaaa-bbbb-cccc-dddddddddddd")',
    1,
)
seed_after_task_write(
    "glm-worker/internal/app/watch_orphan_test.go",
    "watchOrphanTaskID",
    "seedParentReviewStateForTest(t, st, watchOrphanTaskID)",
    1,
)
seed_after_task_write(
    "glm-worker/internal/app/watch_orphan_unix_test.go",
    "watchOrphanTaskID",
    "seedParentReviewStateForTest(t, st, watchOrphanTaskID)",
    1,
)

replace_once(
    "glm-worker/internal/state/parent_action_parked_origin_test.go",
    '''func setParkedReviewLabel(t *testing.T, st *StateStore, label string) {
\tt.Helper()
\tst.UpdateTaskStats(func(stats *TaskStats) {
\t\tstats.ParentReviewOpen = &ParentReviewOpenState{PacketStatus: label}
\t})
}
''',
    '''func setParkedReviewLabel(t *testing.T, st *StateStore, label string) {
\tt.Helper()
\tcurrent, err := st.loadParentReviewState()
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif label == roundCommentNone {
\t\tcurrent.Open = nil
\t} else {
\t\tcurrent.Open = &ParentReviewOpenState{PacketStatus: label}
\t}
\tif err := st.writeParentReviewState(current); err != nil {
\t\tt.Fatal(err)
\t}
}
''',
)
replace_once(
    "glm-worker/internal/state/session_rotation_test.go",
    '''\t\tst.UpdateTaskStats(func(stats *TaskStats) {
\t\t\tstats.openParentReview("PASS", "LOW", ParentReviewProducer{})
\t\t})
''',
    '''\t\tif err := st.openParentReviewState("PASS", "LOW", ParentReviewProducer{}); err != nil {
\t\t\tt.Fatal(err)
\t\t}
''',
)
replace_once(
    "glm-worker/internal/state/quality_surface_recovery_test.go",
    '''\tseedApprovedQualitySurfaceActivation(t, st)
\tfailWritesFor(t, st, currentStatsFile)

\terr := st.ActivateQualitySurfaceApproval()
''',
    '''\tseedApprovedQualitySurfaceActivation(t, st)
\tfailWritesFor(t, st, parentReviewStateFile)

\terr := st.ActivateQualitySurfaceApproval()
''',
)
replace_once(
    "glm-worker/internal/workflow/quality_surface_parent_approval_test.go",
    '''\tst, err := state.NewStateStore(cfg)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif err := state.CaptureGitBaseline(cfg, st); err != nil {
''',
    '''\tst, err := state.NewStateStore(cfg)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif _, err := st.StartNewTask(); err != nil {
\t\tt.Fatal(err)
\t}
\tif err := state.CaptureGitBaseline(cfg, st); err != nil {
''',
)
