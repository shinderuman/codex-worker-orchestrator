package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExecuteExplicitFixRejectsCompletedTask(t *testing.T) {
	st := newStateStoreT(t)
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}

	w := newWorkflowT(t, st, &scriptedRunner{})
	err := w.ExecuteExplicitFix("fix", "", "")
	if err == nil || !strings.Contains(err.Error(), "only available after NEEDS_SOL_REVIEW") {
		t.Fatalf("completed taskの--fixを拒否する必要があります: %v", err)
	}
}

func TestResumePromptUsesOriginalPrompt(t *testing.T) {
	checkpoint := state.ResumeCheckpoint{
		Prompt:         "already wrapped resume prompt",
		OriginalPrompt: "ORIGINAL TASK",
	}

	prompt := resumePrompt(checkpoint)
	if !strings.Contains(prompt, "ORIGINAL TASK") {
		t.Fatalf("original prompt missing: %s", prompt)
	}
	if strings.Contains(prompt, "already wrapped resume prompt") {
		t.Fatalf("resume prompt nested previous resume prompt: %s", prompt)
	}
}

func TestExecuteNewTaskReachesPass(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	if strings.Join(r.models, ",") != "opus,haiku" {
		t.Fatalf("models = %#v", r.models)
	}
	if !strings.Contains(r.prompts[0], artifactPromptMarker) {
		t.Fatalf("worker promptにartifact保存先がありません: %q", r.prompts[0])
	}
	if strings.Contains(r.prompts[1], artifactPromptMarker) {
		t.Fatalf("read-only reviewerへartifact書込指示を渡しています: %q", r.prompts[1])
	}
}

func TestExecuteNewTaskNeedsSolDecision(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: needsSolDecisionPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	if !st.Exists("pending-decision") {
		t.Fatal("pending-decisionが設定されていません")
	}
}

func TestExecuteNewTaskNeedsSolReview(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q", st.TaskStatus())
	}
}

func TestExecuteDecisionContinuesPendingTask(t *testing.T) {
	st := newStateStoreT(t)
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("decision applied")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteDecision("A案で進める"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview || st.Exists("pending-decision") {
		t.Fatalf("decision後のstate: status=%q pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
	if decision := st.ReadOr("last-decision", ""); decision != "A案で進める" {
		t.Fatalf("last-decision = %q", decision)
	}
	if len(r.prompts) == 0 || !strings.Contains(r.prompts[0], "A案で進める") {
		t.Fatalf("decision prompt = %#v", r.prompts)
	}
	if strings.Join(r.models, ",") != "opus,sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestExecuteExplicitFixContinuesSolReviewTask(t *testing.T) {
	st := newStateStoreT(t)
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-review", "review"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("explicit fix")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteExplicitFix("境界値を修正する", "", ""); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	if len(r.prompts) == 0 || !strings.Contains(r.prompts[0], "境界値を修正する") {
		t.Fatalf("fix prompt = %#v", r.prompts)
	}
	if stats := currentStats(t, st); stats.FixCommands != 1 {
		t.Fatalf("fix stats = %#v", stats)
	}
	if strings.Join(r.models, ",") != "opus,sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestAutoFixNonConvergence(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: fixRequiredPacket()},
		{structured: implementedPacket("fix")},
		{structured: fixRequiredPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.collectChangedPaths = func(string, string) ([]string, error) { return []string{"fixture.go"}, nil }
	w.config.MaxAutoFixRounds = 1

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	if strings.Join(r.models, ",") != "opus,haiku,opus,sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestAutoFixCanRequestSolDecision(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: fixRequiredPacket()},
		{structured: needsSolDecisionPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("auto-fix decision state: status=%q pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
}

func TestAutoFixRejectsReviewerStatus(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: fixRequiredPacket()},
		{structured: passPacket()},
	}}
	w := newWorkflowT(t, st, r)

	err := w.ExecuteNewTask("request")
	var workerErr *WorkerError
	if err == nil || !errors.As(err, &workerErr) || !strings.Contains(err.Error(), "worker結果のstatus") {
		t.Fatalf("auto-fix role status error = %v", err)
	}
	if len(r.prompts) != 3 {
		t.Fatalf("修正再依頼はしない: calls=%d", len(r.prompts))
	}
}

func TestWorkerRejectsReviewerStatus(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	w := newWorkflowT(t, st, r)

	err := w.ExecuteNewTask("request")
	if err == nil || !strings.Contains(err.Error(), "worker結果のstatusとして許容されません") {
		t.Fatalf("worker role status error = %v", err)
	}
	if len(r.prompts) != 1 {
		t.Fatalf("schema保証範囲のため修正再依頼はしない: calls=%d", len(r.prompts))
	}
}
