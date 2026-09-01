from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    file = Path(path)
    text = file.read_text()
    if old not in text:
        raise SystemExit(f"replacement target missing in {path}:\n{old}")
    file.write_text(text.replace(old, new, 1))


replace_once(
    "glm-worker/internal/workflow/activetask_test.go",
    "func TestDecisionRejectsInvalidActiveThenSameDecisionResumes(t *testing.T) {",
    "func TestDecisionRetryAfterFailClosedReviewUsesCanonicalAdmission(t *testing.T) {",
)
replace_once(
    "glm-worker/internal/workflow/activetask_test.go",
    '''\twriteActiveTaskFileContent(t, repoRoot)
\tif err := w.ExecuteDecision(decision); err != nil {
\t\tt.Fatal(err)
\t}
\tif st.TaskStatus() != state.TaskStatusWaitingSolReview {
\t\tt.Fatalf("修復後の同じdecision再実行でreviewまで到達すべき: %q", st.TaskStatus())
\t}
\tif got := st.ReadOr("last-decision", ""); got != decision {
\t\tt.Fatalf("再実行後のlast-decision = %q want %q", got, decision)
\t}
\tif len(r.prompts) != 4 {
\t\tt.Fatalf("再実行はdecision worker・reviewer・risk floor再出力の3呼出を追加すべき: %d", len(r.prompts))
\t}
\tfor i, prompt := range r.prompts[1:3] {
\t\tif !strings.Contains(prompt, "ACTIVE_TASK_FILE: "+activeTaskGuardPath) {
\t\t\tt.Fatalf("再実行prompt %dが要求源blockを欠いています:\\n%s", i, prompt)
\t\t}
\t}
\tstats, err = st.CurrentTaskStats()
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif stats.DecisionCommands != 1 {
\t\tt.Fatalf("再実行後のdecision呼出 = %d want 1(拒否は計上しない)", stats.DecisionCommands)
\t}
''',
    '''\twriteActiveTaskFileContent(t, repoRoot)
\terr = w.ExecuteDecision(decision)
\tif err == nil || !strings.Contains(err.Error(), "lifecycle inconsistency") {
\t\tt.Fatalf("fail-closed review後のdecision再実行error = %v", err)
\t}
\tif len(r.prompts) != 1 {
\t\tt.Fatalf("canonical admission拒否後にmodelが呼ばれました: %d", len(r.prompts))
\t}
\tif st.TaskStatus() != state.TaskStatusWaitingDecision || !st.Exists("pending-decision") {
\t\tt.Fatalf("canonical admission拒否後 = %q/pending=%v", st.TaskStatus(), st.Exists("pending-decision"))
\t}
\tif got := st.ReadOr("last-decision", ""); got != "" {
\t\tt.Fatalf("canonical admission拒否後にlast-decisionが消費されています: %q", got)
\t}
''',
)

replace_once(
    "glm-worker/internal/workflow/external_feasibility_gate_test.go",
    "func TestExternalFeasibilityInterruptedResumeRejectThenRepairResumes(t *testing.T) {",
    "func TestExternalFeasibilityInterruptedResumeRejectThenRepairUsesCanonicalAdmission(t *testing.T) {",
)
replace_once(
    "glm-worker/internal/workflow/external_feasibility_gate_test.go",
    '''\tresumeRunner := &scriptedRunner{steps: []runnerStep{
\t\t{structured: implementedPacket("resumed")},
\t\t{structured: passPacket()},
\t\t{structured: needsSolReviewPacket()},
\t}}
\tresumeW := newGitWorkflowT(t, st, resumeRunner, repo)
\tif err := resumeW.ExecuteResume(); err != nil {
\t\tt.Fatalf("宣言修復後の同じ--resumeが保持照合を通過しません: %v", err)
\t}
\tif st.TaskStatus() != state.TaskStatusWaitingSolReview {
\t\tt.Fatalf("修復後resumeのtask status = %q want waiting-sol-review", st.TaskStatus())
\t}
''',
    '''\tresumeRunner := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("resumed")}}}
\tresumeW := newGitWorkflowT(t, st, resumeRunner, repo)
\terr = resumeW.ExecuteResume()
\tif err == nil || !strings.Contains(err.Error(), "lifecycle inconsistency") {
\t\tt.Fatalf("fail-closed review後のresume再実行error = %v", err)
\t}
\tif len(resumeRunner.prompts) != 0 {
\t\tt.Fatalf("canonical admission拒否後にmodelが呼ばれました: %d", len(resumeRunner.prompts))
\t}
\tif st.TaskStatus() != state.TaskStatusInterrupted {
\t\tt.Fatalf("canonical admission拒否後のtask status = %q want interrupted", st.TaskStatus())
\t}
''',
)

replace_once(
    "glm-worker/internal/workflow/report_only_snapshot_test.go",
    'err == nil || !strings.Contains(err.Error(), "unsupported resume state version: 3")',
    'err == nil || !strings.Contains(err.Error(), "lifecycle inconsistency")',
)
replace_once(
    "glm-worker/internal/workflow/report_only_snapshot_test.go",
    'err == nil || !strings.Contains(err.Error(), "unsupported resume state version: 5")',
    'err == nil || !strings.Contains(err.Error(), "lifecycle inconsistency")',
)

replace_once(
    "glm-worker/internal/workflow/workflow_test.go",
    '''\t\tif err := st.Touch("pending-decision"); err != nil {
\t\t\tt.Fatal(err)
\t\t}
\t\tw := newWorkflowT(t, st, &scriptedRunner{})''',
    '''\t\tif err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
\t\t\tt.Fatal(err)
\t\t}
\t\tif err := st.Touch("pending-decision"); err != nil {
\t\t\tt.Fatal(err)
\t\t}
\t\tw := newWorkflowT(t, st, &scriptedRunner{})''',
)
replace_once(
    "glm-worker/internal/workflow/workflow_test.go",
    '''\t\tif err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{Model: "opus", StopKind: state.ResumeStopRateLimited}); err != nil {
\t\t\tt.Fatal(err)
\t\t}
\t\tw := newWorkflowT(t, st, &scriptedRunner{})''',
    '''\t\tif err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{Model: "opus", StopKind: state.ResumeStopRateLimited}); err != nil {
\t\t\tt.Fatal(err)
\t\t}
\t\tif err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
\t\t\tt.Fatal(err)
\t\t}
\t\tw := newWorkflowT(t, st, &scriptedRunner{})''',
)
