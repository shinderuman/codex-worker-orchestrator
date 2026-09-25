package workflow

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestHighRiskWorkerUsesHighRiskReviewer(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("done", "HIGH")},
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.models, ",") != "opus,sonnet,sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestReviewNeedsHighRiskFloor(t *testing.T) {
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

func TestRiskFloorFailClosedPacketIsValid(t *testing.T) {
	passPkt := resultFromBody(`{"status":"PASS","risk":"LOW","summary":"reviewer pass","requirement_coverage":"covered","invariants":"preserved","test_evidence":"ev","issues":"none","residual_risk":"none","targets":["none"]}`)
	st := newStateStoreT(t)
	w := newWorkflowT(t, st, &scriptedRunner{})
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"internal/task/change.go", "cmd/tool/main.go"}, nil
	}

	enforced := w.riskFloorFailClosedResult(passPkt)
	if enforced.Status != packet.StatusNeedsSolReview || enforced.Risk != packet.RiskHigh {
		t.Fatalf("status=%s risk=%s", enforced.Status, enforced.Risk)
	}
	if err := validateTypedResult(enforced); err != nil {
		t.Fatalf("fail closed結果がvalidate不合格: %v", err)
	}
	if got := strings.Join(enforced.Targets, ","); got != "internal/task/change.go:diff,cmd/tool/main.go:diff" {
		t.Fatalf("fail closed TARGETSがcurrent task diffを指していない: %q", got)
	}
	if enforced.RequirementCoverage == "covered" {
		t.Fatalf("reviewerのPASS内容をfail closed結果へ捏造している: %#v", enforced)
	}
}

func TestResolveRiskFloorReemitAcceptsCompliantAndFailsClosed(t *testing.T) {
	st := newStateStoreT(t)
	w := newWorkflowT(t, st, &scriptedRunner{})
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"internal/task/change.go"}, nil
	}
	compliant := resultFromBody(`{"status":"NEEDS_SOL_REVIEW","risk":"HIGH","summary":"reviewer reemit","requirement_coverage":"covered","invariants":"preserved","test_evidence":"ev","issues":"i","residual_risk":"r","targets":["glm-worker/internal/workflow/workflow_test.go:needsSolReviewPacket"],"sol_question":"q"}`)
	if resolved := w.resolveRiskFloorReemit(compliant); resolved.Status != packet.StatusNeedsSolReview {
		t.Fatalf("準拠再出力はそのまま採用すべき: %#v", resolved)
	}

	passed := resultFromBody(`{"status":"PASS","risk":"LOW","summary":"pass again","requirement_coverage":"covered","invariants":"preserved","test_evidence":"ev","issues":"none","residual_risk":"none","targets":["none"]}`)
	closed := w.resolveRiskFloorReemit(passed)
	if closed.Status != packet.StatusNeedsSolReview || !strings.Contains(closed.Summary, "PASS") {
		t.Fatalf("再違反はfail closedのNEEDS_SOL_REVIEWへ昇格すべき: %#v", closed)
	}
}

func TestRiskFloorRejectsPassOnHighRiskWorker(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("high risk work", "HIGH")},
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

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
	if !strings.Contains(review, `"summary":"review"`) {
		t.Fatalf("reviewer自身の再出力NEEDS_SOL_REVIEWを採用すべき(捏造でない): %s", review)
	}
	if len(r.prompts) != 3 || !strings.Contains(r.prompts[2], "wrapper risk floor") {
		t.Fatalf("同一sessionへ再出力promptを送るべき: %#v", r.prompts)
	}
	if strings.Join(r.models, ",") != "opus,sonnet,sonnet" {
		t.Fatalf("再出力もHighRiskReviewerModelを使うべき: %#v", r.models)
	}
}

func TestRiskFloorRejectsPassAfterDecision(t *testing.T) {
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
	if !strings.Contains(review, `"summary":"review"`) {
		t.Fatalf("reviewer自身の再出力を採用すべき: %s", review)
	}
	if strings.Join(r.models, ",") != "opus,sonnet,sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestRiskFloorRejectsPassAfterAutoFix(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: fixRequiredPacket()},
		{structured: implementedPacket("fixed")},
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

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
	if !strings.Contains(review, `"summary":"review"`) {
		t.Fatalf("reviewer自身の再出力を採用すべき: %s", review)
	}
	if strings.Join(r.models, ",") != "opus,haiku,opus,sonnet,sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestRiskFloorRejectsPassAfterExplicitFix(t *testing.T) {
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
	if !strings.Contains(review, `"summary":"review"`) {
		t.Fatalf("reviewer自身の再出力を採用すべき: %s", review)
	}
	if strings.Join(r.models, ",") != "opus,sonnet,sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestRiskFloorRejectsPassAfterResume(t *testing.T) {
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
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

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
	if !strings.Contains(review, `"summary":"review"`) {
		t.Fatalf("reviewer自身の再出力を採用すべき: %s", review)
	}
	if strings.Join(r.models, ",") != "sonnet,sonnet" {
		t.Fatalf("resume後のreviewと再出力models = %#v", r.models)
	}
}

func TestRiskFloorAllowsLowRiskPass(t *testing.T) {
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

func TestRiskFloorReemitFailClosedOnRepeatedPass(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("high risk work", "HIGH")},
		{structured: passPacket()},
		{structured: passPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("再違反時はfail closedでSol確認待ちへ: status=%q", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"status":"NEEDS_SOL_REVIEW"`) || !strings.Contains(review, `"risk":"HIGH"`) {
		t.Fatalf("fail closed packetでない: %s", review)
	}
	if !strings.Contains(review, "PASS") {
		t.Fatalf("再違反のfail closed summaryは非許容STATUSを明示すべき: %s", review)
	}
	if strings.Contains(review, `"requirement_coverage":"covered"`) {
		t.Fatalf("reviewerのPASS内容を捏造してはいけない: %s", review)
	}
	if len(r.prompts) != 3 {
		t.Fatalf("再出力は1回だけ行い無限反復しない: calls=%d", len(r.prompts))
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil {
		t.Fatal("fail closed後はresume checkpointを残さない")
	}
}

func TestRiskFloorReemitResumeCompliant(t *testing.T) {
	st := newStateStoreT(t)
	seedReviewStartSnapshot(t, st)
	if err := st.Write("last-request", "req"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:           state.ResumeStageReview,
		Phase:           "reviewer-1-risk-floor",
		Role:            state.ReviewerRole,
		Model:           "sonnet",
		ReadOnly:        true,
		Effort:          "high",
		Prompt:          "reemit",
		OriginalPrompt:  "reemit",
		Request:         "request",
		WorkerResult:    workerResultFromBody(workerPacketWithRisk("HIGH")),
		ReviewNumber:    1,
		StopKind:        state.ResumeStopRateLimited,
		RiskFloorReemit: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{{structured: needsSolReviewPacket()}}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("再出力resumeの準拠結果はSol確認待ちへ: status=%q", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"summary":"review"`) {
		t.Fatalf("reviewer自身の再出力NEEDS_SOL_REVIEWを採用すべき: %s", review)
	}
	if len(r.prompts) != 1 || !strings.Contains(r.prompts[0], "再開") {
		t.Fatalf("再出力工程からresume再開すべき: %#v", r.prompts)
	}
	if len(r.models) != 1 || r.models[0] != "sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestRiskFloorReemitResumeFailClosed(t *testing.T) {
	st := newStateStoreT(t)
	seedReviewStartSnapshot(t, st)
	if err := st.Write("last-request", "req"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:           state.ResumeStageReview,
		Phase:           "reviewer-1-risk-floor",
		Role:            state.ReviewerRole,
		Model:           "sonnet",
		ReadOnly:        true,
		Effort:          "high",
		Prompt:          "reemit",
		OriginalPrompt:  "reemit",
		Request:         "request",
		WorkerResult:    workerResultFromBody(workerPacketWithRisk("HIGH")),
		ReviewNumber:    1,
		StopKind:        state.ResumeStopRateLimited,
		RiskFloorReemit: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("再出力resumeの再違反もfail closedでSol確認待ちへ: status=%q", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"status":"NEEDS_SOL_REVIEW"`) || !strings.Contains(review, "PASS") {
		t.Fatalf("fail closed packetでない: %s", review)
	}
	if strings.Contains(review, `"requirement_coverage":"covered"`) {
		t.Fatalf("reviewerのPASS内容を捏造してはいけない: %s", review)
	}
	if len(r.prompts) != 1 {
		t.Fatalf("再出力resume後は追加呼出しない: calls=%d", len(r.prompts))
	}
}
