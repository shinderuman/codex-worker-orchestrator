package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRunNoGoCompletesObservationWithoutWorker(t *testing.T) {
	cfg, st := newParentActionIdentityTestState(t)
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/observation.md", []byte("# observation\n\n## External feasibility\n\nstatus: observation\nassumption: representative producer behavior\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolDecision, Risk: packet.RiskHigh}, state.ParentReviewProducer{Role: string(state.WorkerRole), Model: "opus"})

	var stdout bytes.Buffer
	if err := execute(cfg, []string{"no-go"}, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output noGoOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Status != "no-go" || !output.Completed {
		t.Fatalf("output = %#v", output)
	}
	if st.TaskStatus() != state.TaskStatusComplete || st.Exists("pending-decision") {
		t.Fatalf("terminal state = status:%s pending:%v", st.TaskStatus(), st.Exists("pending-decision"))
	}
}

func TestRunNoGoRejectsHeldRepositoryLock(t *testing.T) {
	cfg, st := newParentActionIdentityTestState(t)
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/observation.md", []byte("# observation\n\n## External feasibility\n\nstatus: observation\nassumption: representative producer behavior\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolDecision, Risk: packet.RiskHigh}, state.ParentReviewProducer{Role: string(state.WorkerRole), Model: "opus"})

	lock, err := repolock.Acquire(st.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()

	if err := runNoGo(cfg, &bytes.Buffer{}); !errors.Is(err, repolock.ErrHeld) {
		t.Fatalf("runNoGo error = %v, want ErrHeld", err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("contended no-go mutated state: status:%s pending:%v", st.TaskStatus(), st.Exists("pending-decision"))
	}
}

func TestRunNoGoRejectsGenericDecision(t *testing.T) {
	cfg, st := newParentActionIdentityTestState(t)
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/normal.md", []byte("# normal\n\n## External feasibility\n\nstatus: not-applicable\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolDecision, Risk: packet.RiskHigh}, state.ParentReviewProducer{})

	if err := execute(cfg, []string{"no-go"}, &bytes.Buffer{}, nil); err == nil {
		t.Fatal("generic decision accepted terminal no-go")
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("generic decision was mutated: status:%s pending:%v", st.TaskStatus(), st.Exists("pending-decision"))
	}
}
