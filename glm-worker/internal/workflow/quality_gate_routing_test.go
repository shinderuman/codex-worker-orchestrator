package workflow

import (
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestQualityGateBlocksReviewerAndRoutesWorkerFix(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("initial")},
		{structured: implementedPacket("fixed")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()
	calls := 0
	w.qualityGate = func(string) (harnesslint.Report, error) {
		calls++
		if calls == 1 {
			return harnesslint.Report{Status: "fail", Violations: []harnesslint.Violation{{Rule: "funlen", Path: "a.go", Line: 3, Column: 1, Message: "too long"}}}, nil
		}
		return harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
	}
	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.phases, []string{"worker-new", "worker-auto-fix-1", "reviewer-2-high-floor"}) {
		t.Fatalf("phases = %v", r.phases)
	}
	if calls != 2 {
		t.Fatalf("harnesslint calls = %d", calls)
	}
	if !strings.Contains(r.prompts[1], "harnesslint") {
		t.Fatalf("fix prompt = %s", r.prompts[1])
	}
	status := st.TaskStatus()
	if status != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %s", status)
	}
}
