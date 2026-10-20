package workflow

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestHighRiskWorkerUsesHighRiskReviewer(t *testing.T) {
	t.Parallel()
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("done", "HIGH")},
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)
	writeGitTestFile(t, w.config.RepoRoot, "glm-worker/internal/workflow/risk_floor_test.go", "package workflow\n")
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"glm-worker/internal/workflow/risk_floor_test.go"}, nil
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.models, ",") != "opus,sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestReviewNeedsHighRiskFloor(t *testing.T) {
	t.Parallel()
	lowWorker := resultFromBody(`{"status":"IMPLEMENTED","risk":"LOW"}`)
	highWorker := resultFromBody(`{"status":"IMPLEMENTED","risk":"HIGH"}`)

	tests := []struct {
		name           string
		workerResult   packet.Result
		autoFixes      int
		hasDecision    bool
		hasPriorReview bool
		want           bool
	}{
		{"low worker fresh", lowWorker, 0, false, false, false},
		{"high worker", highWorker, 0, false, false, true},
		{"after autofix", lowWorker, 1, false, false, true},
		{"after decision", lowWorker, 0, true, false, true},
		{"after prior review", lowWorker, 0, false, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reviewNeedsHighRiskFloor(tt.workerResult, tt.autoFixes, tt.hasDecision, tt.hasPriorReview); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestRiskFloorEscalationPacketIsValid(t *testing.T) {
	t.Parallel()
	passPkt := resultFromBody(`{"status":"PASS","risk":"LOW","summary":"reviewer pass","requirement_coverage":"covered","invariants":"preserved","test_evidence":"ev","issues":"none","residual_risk":"none","targets":["none"]}`)

	w := newWorkflowT(t, newStateStoreT(t), &scriptedRunner{})
	w.collectChangedPaths = func(string, string) ([]string, error) { return []string{"internal/task/change.go"}, nil }
	enforced, err := w.riskFloorEscalationResult(passPkt)
	if err != nil {
		t.Fatal(err)
	}
	if enforced.Status != packet.StatusNeedsSolReview || enforced.Risk != packet.RiskHigh {
		t.Fatalf("status=%s risk=%s", enforced.Status, enforced.Risk)
	}
	if err := validateTypedResult(enforced); err != nil {
		t.Fatalf("fail closed結果がvalidate不合格: %v", err)
	}
	if enforced.RequirementCoverage == "covered" {
		t.Fatalf("reviewerのPASS内容を昇格結果へ捏造している: %#v", enforced)
	}
	if !strings.Contains(enforced.Summary, "PASS") || !strings.Contains(enforced.TestEvidence, "ev") {
		t.Fatalf("raw reviewer PASSの内容を保持すべき: %#v", enforced)
	}
	if len(enforced.Targets) != 1 || enforced.Targets[0] != "internal/task/change.go:@diff" {
		t.Fatalf("semantic review targets = %#v", enforced.Targets)
	}
}

func TestRiskFloorRejectsPassOnHighRiskWorker(t *testing.T) {
	t.Parallel()
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("high risk work", "HIGH")},
		{structured: passPacket()},
	}}
	w := newWorkflowT(t, st, r)
	writeGitTestFile(t, w.config.RepoRoot, "glm-worker/internal/workflow/risk_floor_test.go", "package workflow\n")
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"glm-worker/internal/workflow/risk_floor_test.go"}, nil
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("HIGH risk workerへのreviewer PASSを拒否すべき: status=%q", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"status":"NEEDS_SOL_REVIEW"`) || !strings.Contains(review, `"risk":"HIGH"`) {
		t.Fatalf("risk floor強制packetでない: %s", review)
	}
	if strings.Contains(review, "STATUS: PASS") {
		t.Fatalf("PASSが通っている: %s", review)
	}
	if !strings.Contains(review, "機械的にSol確認へ昇格") {
		t.Fatalf("昇格は機械適用でありreviewer自身の再出力として記録しない: %s", review)
	}
	if !strings.Contains(review, "reviewer raw status=PASS") {
		t.Fatalf("raw reviewer PASSを原文として保持すべき: %s", review)
	}
	if len(r.prompts) != 2 {
		t.Fatalf("再出力model turnを行わない: %#v", r.prompts)
	}
	if strings.Join(r.models, ",") != "opus,sonnet" {
		t.Fatalf("reviewまでの2呼び出しで完結すべき: %#v", r.models)
	}
}

func TestRiskFloorRejectsPassAfterDecision(t *testing.T) {
	t.Parallel()
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
		{structured: implementedPacketWithRisk("decision applied", "LOW")},
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)
	writeGitTestFile(t, w.config.RepoRoot, "glm-worker/internal/workflow/risk_floor_test.go", "package workflow\n")
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"glm-worker/internal/workflow/risk_floor_test.go"}, nil
	}

	if err := w.ExecuteDecision("A案で進める"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("decision後のreviewer PASSを拒否すべき: status=%q", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"status":"NEEDS_SOL_REVIEW"`) || !strings.Contains(review, `"risk":"HIGH"`) {
		t.Fatalf("risk floor強制packetでない: %s", review)
	}
	if !strings.Contains(review, "機械的にSol確認へ昇格") {
		t.Fatalf("昇格packetでない: %s", review)
	}
	if strings.Join(r.models, ",") != "opus,sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestRiskFloorRejectsPassAfterAutoFix(t *testing.T) {
	t.Parallel()
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: fixRequiredPacket()},
		{structured: implementedPacket("fixed")},
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)
	writeGitTestFile(t, w.config.RepoRoot, "glm-worker/internal/workflow/risk_floor_test.go", "package workflow\n")
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"glm-worker/internal/workflow/risk_floor_test.go"}, nil
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("auto-fix後のreviewer PASSを拒否すべき: status=%q", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"status":"NEEDS_SOL_REVIEW"`) || !strings.Contains(review, `"risk":"HIGH"`) {
		t.Fatalf("risk floor強制packetでない: %s", review)
	}
	if !strings.Contains(review, "機械的にSol確認へ昇格") {
		t.Fatalf("昇格packetでない: %s", review)
	}
	if strings.Join(r.models, ",") != "opus,haiku,opus,sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestRiskFloorRejectsPassAfterExplicitFix(t *testing.T) {
	t.Parallel()
	st := newStateStoreT(t)
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-review", "previous review"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("explicit fix")},
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)
	writeGitTestFile(t, w.config.RepoRoot, "glm-worker/internal/workflow/risk_floor_test.go", "package workflow\n")
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"glm-worker/internal/workflow/risk_floor_test.go"}, nil
	}

	if err := w.ExecuteExplicitFix("境界値を修正する", "", ""); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("explicit fix後のreviewer PASSを拒否すべき: status=%q", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"status":"NEEDS_SOL_REVIEW"`) || !strings.Contains(review, `"risk":"HIGH"`) {
		t.Fatalf("risk floor強制packetでない: %s", review)
	}
	if !strings.Contains(review, "機械的にSol確認へ昇格") {
		t.Fatalf("昇格packetでない: %s", review)
	}
	if strings.Join(r.models, ",") != "opus,sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestRiskFloorRejectsPassAfterResume(t *testing.T) {
	t.Parallel()
	st := newStateStoreT(t)
	seedReviewStartSnapshot(t, st)
	if err := st.Write("last-request", "req"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:          state.ResumeStageReview,
		Phase:          "reviewer-1",
		Role:           state.ReviewerRole,
		Model:          "sonnet",
		ReadOnly:       true,
		Effort:         "high",
		Prompt:         "review",
		OriginalPrompt: "review",
		Request:        "request",
		WorkerResult:   workerResultFromBody(workerPacketWithRisk("HIGH")),
		ReviewNumber:   1,
		StopKind:       state.ResumeStopRateLimited,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{
		{structured: passPacket()},
	}}
	w := newWorkflowT(t, st, r)
	writeGitTestFile(t, w.config.RepoRoot, "glm-worker/internal/workflow/risk_floor_test.go", "package workflow\n")
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"glm-worker/internal/workflow/risk_floor_test.go"}, nil
	}

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("resume後のreviewer PASSを拒否すべき: status=%q", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"status":"NEEDS_SOL_REVIEW"`) || !strings.Contains(review, `"risk":"HIGH"`) {
		t.Fatalf("risk floor強制packetでない: %s", review)
	}
	if !strings.Contains(review, "機械的にSol確認へ昇格") {
		t.Fatalf("昇格packetでない: %s", review)
	}
	if strings.Join(r.models, ",") != "sonnet" {
		t.Fatalf("resume後のreview呼び出しのみ: %#v", r.models)
	}
}

func TestRiskFloorAllowsLowRiskPass(t *testing.T) {
	t.Parallel()
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
		t.Fatalf("LOW risk通常PASSは完遂すべき: status=%q", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"status":"PASS"`) || !strings.Contains(review, `"risk":"LOW"`) {
		t.Fatalf("PASS/LOWが保持されるべき: %s", review)
	}
	if strings.Join(r.models, ",") != "opus,haiku" {
		t.Fatalf("通常ReviewerModelを使うべき: %#v", r.models)
	}
}

func TestRiskFloorEscalationDoesNotRepeatModelCalls(t *testing.T) {
	t.Parallel()
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("high risk work", "HIGH")},
		{structured: passPacket()},
	}}
	w := newWorkflowT(t, st, r)
	writeGitTestFile(t, w.config.RepoRoot, "glm-worker/internal/workflow/risk_floor_test.go", "package workflow\n")
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"glm-worker/internal/workflow/risk_floor_test.go"}, nil
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("昇格時はSol確認待ちへ: status=%q", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"status":"NEEDS_SOL_REVIEW"`) || !strings.Contains(review, `"risk":"HIGH"`) {
		t.Fatalf("昇格packetでない: %s", review)
	}
	if !strings.Contains(review, "PASS") {
		t.Fatalf("昇格summaryはraw reviewer STATUSを明示すべき: %s", review)
	}
	if strings.Contains(review, `"requirement_coverage":"covered"`) {
		t.Fatalf("reviewerのPASS内容を捏造してはいけない: %s", review)
	}
	if len(r.prompts) != 2 {
		t.Fatalf("昇格に追加model呼出しを要しない: calls=%d", len(r.prompts))
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil {
		t.Fatal("昇格後はresume checkpointを残さない")
	}
}
