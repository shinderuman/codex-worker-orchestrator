from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    file = Path(path)
    text = file.read_text()
    if old not in text:
        raise SystemExit(f"replacement target missing in {path}:\n{old}")
    file.write_text(text.replace(old, new, 1))


replace_once(
    "glm-worker/internal/workflow/workflow.go",
    '''func (w *Workflow) validateNewTaskStart() error {
\tif w.state.Exists("pending-decision") {
\t\treturn &WorkerError{Message: "previous task is waiting for Sol decision; use --decision or --reset"}
\t}
\tif open := w.state.OpenParentReviewLabel(); open != "none" {
\t\treturn &WorkerError{Message: fmt.Sprintf("previous task has unresolved parent review (%s); resolve it explicitly with --accept (or --fix when rework is required) before starting a new task", open)}
\t}
\tcheckpoint, err := w.state.LoadResumeCheckpoint()
\tif err != nil {
\t\treturn nil
\t}
\tswitch checkpoint.StopKind {
\tcase state.ResumeStopRateLimited:
\t\treturn &WorkerError{Message: "previous task is rate-limited; use --resume or --reset"}
\tcase state.ResumeStopProviderUnavailable:
\t\treturn &WorkerError{Message: "previous task is provider-unavailable; use --resume or --reset"}
\tcase state.ResumeStopInterrupted:
\t\treturn &WorkerError{Message: "previous task is interrupted; use --resume or --reset"}
\tcase state.ResumeStopGuardRecoverable:
\t\treturn &WorkerError{Message: "previous task stopped on a recoverable guard failure; repair the guard then use --resume or --reset"}
\tdefault:
\t\treturn nil
\t}
}''',
    '''func (w *Workflow) validateNewTaskStart() error {
\treturn w.admitNewTask()
}''',
)

replace_once(
    "glm-worker/internal/workflow/workflow.go",
    '''\t\tif w.state.TaskStatus() != state.TaskStatusWaitingDecision || !w.state.Exists("pending-decision") {
\t\t\treturn &WorkerError{Message: "no pending Sol decision for this repository"}
\t\t}''',
    '''\t\tif err := w.admitParentAction(state.ParentActionDecision); err != nil {
\t\t\treturn err
\t\t}''',
)

replace_once(
    "glm-worker/internal/workflow/workflow.go",
    '''\t\tif w.state.Exists("pending-decision") {
\t\t\treturn &WorkerError{Message: "task is waiting for Sol decision; resolve it before --fix"}
\t\t}
\t\tif w.state.TaskStatus() != state.TaskStatusWaitingSolReview {
\t\t\treturn &WorkerError{Message: "--fix is only available after NEEDS_SOL_REVIEW; start a new task after PASS"}
\t\t}''',
    '''\t\tif err := w.admitParentAction(state.ParentActionFix); err != nil {
\t\t\treturn err
\t\t}''',
)

replace_once(
    "glm-worker/internal/workflow/workflow.go",
    '''func (w *Workflow) executeResume() error {
\tcheckpoint, decl, pocResume, err := w.loadResumeCheckpoint()''',
    '''func (w *Workflow) executeResume() error {
\tif err := w.admitParentAction(state.ParentActionResume); err != nil {
\t\treturn err
\t}
\tcheckpoint, decl, pocResume, err := w.loadResumeCheckpoint()''',
)

replace_once(
    "glm-worker/internal/workflow/workflow.go",
    '''\tif !checkpoint.IsStopped() {
\t\treturn state.ResumeCheckpoint{}, externalFeasibility{}, false, &WorkerError{Message: "saved task is not stopped by Z.ai 5h limit, provider unavailability, user interruption or a recoverable guard failure"}
\t}
''',
    "",
)

replace_once(
    "glm-worker/internal/workflow/execution_milestone_flow.go",
    '''\tif w.state.TaskStatus() != state.TaskStatusWaitingDecision || !w.state.Exists("pending-decision") {
\t\treturn executionMilestoneDecisionContext{}, &WorkerError{Message: "no pending Sol decision for this repository"}
\t}''',
    '''\tif err := w.admitParentAction(state.ParentActionDecision); err != nil {
\t\treturn executionMilestoneDecisionContext{}, err
\t}''',
)

replace_once(
    "glm-worker/internal/workflow/execution_milestone_flow.go",
    '''func (w *Workflow) executeExecutionMilestoneResume() error {
\tcheckpoint, decl, pocResume, err := w.loadResumeCheckpoint()''',
    '''func (w *Workflow) executeExecutionMilestoneResume() error {
\tif err := w.admitParentAction(state.ParentActionResume); err != nil {
\t\treturn err
\t}
\tcheckpoint, decl, pocResume, err := w.loadResumeCheckpoint()''',
)
